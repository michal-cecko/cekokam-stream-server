package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/cekokam/cekokam-stream-server/internal/config"
	"github.com/cekokam/cekokam-stream-server/internal/dashboard"
	"github.com/cekokam/cekokam-stream-server/internal/health"
	"github.com/cekokam/cekokam-stream-server/internal/hls"
	"github.com/cekokam/cekokam-stream-server/internal/server"
)

const healthWindow = 2 * time.Minute

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", slog.Any("err", err))
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Join(cfg.StorageDir, "streams"), 0o755); err != nil {
		logger.Error("create streams dir failed", slog.Any("err", err))
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(cfg.StorageDir, "logos"), 0o755); err != nil {
		logger.Error("create logos dir failed", slog.Any("err", err))
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h := health.New()
	dash := dashboard.NewClient(cfg.DashboardURL, cfg.DashboardToken)
	dl := hls.NewDownloader(cfg.StorageDir, cfg.PublicURL, cfg.PollInterval, cfg.SegmentTimeout, h, logger)
	pr := hls.NewPruner(cfg.StorageDir, cfg.PreserveCount, cfg.PruneInterval, logger)

	supervisor := newChannelSupervisor(rootCtx, cfg, dash, dl, pr, h, logger)

	go supervisor.run()

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.New(cfg.StorageDir, h, healthWindow),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http server listening", slog.String("addr", cfg.ListenAddr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", slog.Any("err", err))
			stop()
		}
	}()

	<-rootCtx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", slog.Any("err", err))
	}

	supervisor.wait()
	logger.Info("shutdown complete")
}

type channelSupervisor struct {
	ctx     context.Context
	cfg     config.Config
	dash    *dashboard.Client
	dl      *hls.Downloader
	pr      *hls.Pruner
	health  *health.State
	logger  *slog.Logger
	mu      sync.Mutex
	active  map[string]channelHandle
	wg      sync.WaitGroup
	stopped chan struct{}
}

type channelHandle struct {
	cancel context.CancelFunc
}

func newChannelSupervisor(ctx context.Context, cfg config.Config, dash *dashboard.Client, dl *hls.Downloader, pr *hls.Pruner, h *health.State, logger *slog.Logger) *channelSupervisor {
	return &channelSupervisor{
		ctx:     ctx,
		cfg:     cfg,
		dash:    dash,
		dl:      dl,
		pr:      pr,
		health:  h,
		logger:  logger,
		active:  make(map[string]channelHandle),
		stopped: make(chan struct{}),
	}
}

func (s *channelSupervisor) run() {
	defer close(s.stopped)
	s.sync()

	t := time.NewTicker(s.cfg.SyncInterval)
	defer t.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			for _, h := range s.active {
				h.cancel()
			}
			s.mu.Unlock()
			return
		case <-t.C:
			s.sync()
		}
	}
}

func (s *channelSupervisor) wait() {
	<-s.stopped
	s.wg.Wait()
}

func (s *channelSupervisor) sync() {
	syncCtx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	channels, err := s.dash.ListChannels(syncCtx)
	if err != nil {
		s.logger.Warn("dashboard sync failed", slog.Any("err", err))
		return
	}
	s.health.RecordSync()

	wanted := make(map[string]dashboard.Channel, len(channels))
	for _, ch := range channels {
		if ch.IsActive {
			wanted[ch.Slug] = ch
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for slug, h := range s.active {
		if _, ok := wanted[slug]; !ok {
			h.cancel()
			delete(s.active, slug)
			s.logger.Info("channel removed", slog.String("slug", slug))
		}
	}

	for slug, ch := range wanted {
		if _, ok := s.active[slug]; ok {
			s.refreshLogo(syncCtx, ch)
			continue
		}
		s.refreshLogo(syncCtx, ch)
		chCtx, cancel := context.WithCancel(s.ctx)
		s.active[slug] = channelHandle{cancel: cancel}
		s.wg.Add(2)
		go func(c dashboard.Channel) {
			defer s.wg.Done()
			s.dl.Run(chCtx, c)
		}(ch)
		go func(c dashboard.Channel) {
			defer s.wg.Done()
			s.pr.Run(chCtx, c)
		}(ch)
		s.logger.Info("channel added", slog.String("slug", slug))
	}
}

func (s *channelSupervisor) refreshLogo(ctx context.Context, ch dashboard.Channel) {
	body, err := s.dash.FetchLogo(ctx, ch.Slug)
	if err != nil {
		if !errors.Is(err, dashboard.ErrNotFound) {
			s.logger.Warn("logo fetch failed", slog.String("slug", ch.Slug), slog.Any("err", err))
		}
		return
	}
	dst := filepath.Join(s.cfg.StorageDir, "logos", ch.Slug+".png")
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		s.logger.Warn("logo write failed", slog.String("slug", ch.Slug), slog.Any("err", err))
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		s.logger.Warn("logo rename failed", slog.String("slug", ch.Slug), slog.Any("err", err))
	}
}
