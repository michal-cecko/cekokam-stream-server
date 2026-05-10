package hls

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/cekokam/cekokam-stream-server/internal/dashboard"
	"github.com/cekokam/cekokam-stream-server/internal/health"
)

type Downloader struct {
	StorageDir     string
	PublicURL      string
	PollInterval   time.Duration
	SegmentTimeout time.Duration
	Health         *health.State
	HTTPClient     *http.Client
	Log            *slog.Logger
}

func NewDownloader(storageDir, publicURL string, pollInterval, segmentTimeout time.Duration, h *health.State, logger *slog.Logger) *Downloader {
	return &Downloader{
		StorageDir:     storageDir,
		PublicURL:      publicURL,
		PollInterval:   pollInterval,
		SegmentTimeout: segmentTimeout,
		Health:         h,
		HTTPClient:     &http.Client{Timeout: 0},
		Log:            logger,
	}
}

func (d *Downloader) Run(ctx context.Context, ch dashboard.Channel) {
	logger := d.Log.With(slog.String("slug", ch.Slug))
	logger.Info("downloader started")
	defer logger.Info("downloader stopped")

	d.tick(ctx, ch, logger)

	t := time.NewTicker(d.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.tick(ctx, ch, logger)
		}
	}
}

func (d *Downloader) tick(ctx context.Context, ch dashboard.Channel, logger *slog.Logger) {
	start := time.Now()
	stats := struct {
		Downloaded int
		Skipped    int
		Failed     int
	}{}

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, err := d.fetchPlaylist(fetchCtx, ch.Source)
	if err != nil {
		logger.Warn("fetch playlist failed", slog.Any("err", err))
		return
	}

	pl := Parse(body)
	if len(pl.Segments) == 0 {
		logger.Warn("empty playlist (no segments)")
		return
	}

	streamDir := filepath.Join(d.StorageDir, "streams", ch.Slug)
	renamed := make(map[string]string, len(pl.Segments))

	for _, seg := range pl.Segments {
		seqName := pathinfoFilename(seg)
		if seqName == "" {
			continue
		}
		segDir := filepath.Join(streamDir, "ts", seqName)

		if existing, ok := firstFileIn(segDir); ok {
			renamed[seqName] = filepath.ToSlash(filepath.Join("streams", ch.Slug, "ts", seqName, existing))
			stats.Skipped++
			continue
		}

		if err := os.MkdirAll(segDir, 0o755); err != nil {
			logger.Warn("mkdir segment dir failed", slog.String("seq", seqName), slog.Any("err", err))
			stats.Failed++
			continue
		}

		segURL, err := resolveSegmentURL(ch.Source, seg)
		if err != nil {
			logger.Warn("resolve segment url failed", slog.String("seg", seg), slog.Any("err", err))
			stats.Failed++
			continue
		}

		hashedName := MD5Hex(seg) + ".ts"
		dstPath := filepath.Join(segDir, hashedName)

		if err := d.downloadSegment(ctx, segURL, dstPath); err != nil {
			logger.Warn("segment download failed", slog.String("seq", seqName), slog.String("url", segURL), slog.Any("err", err))
			stats.Failed++
			continue
		}

		renamed[seqName] = filepath.ToSlash(filepath.Join("streams", ch.Slug, "ts", seqName, hashedName))
		stats.Downloaded++
		logger.Info("segment downloaded", slog.String("seq", seqName), slog.String("name", hashedName))
	}

	rewritten := Rewrite(pl, ch.Slug, ch.Name, d.PublicURL, renamed)

	if err := atomicWrite(filepath.Join(streamDir, "stream.m3u8"), rewritten); err != nil {
		logger.Error("publish m3u8 failed", slog.Any("err", err))
		return
	}

	d.Health.RecordChannelTick()
	logger.Info("tick complete",
		slog.Int("downloaded", stats.Downloaded),
		slog.Int("skipped", stats.Skipped),
		slog.Int("failed", stats.Failed),
		slog.Duration("runtime", time.Since(start)))
}

func (d *Downloader) fetchPlaylist(ctx context.Context, source string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (d *Downloader) downloadSegment(parentCtx context.Context, segURL, dstPath string) error {
	ctx, cancel := context.WithTimeout(parentCtx, d.SegmentTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
	if err != nil {
		return err
	}
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("empty body")
	}
	if bytes.Contains(body, []byte("Internal Server Error")) {
		return fmt.Errorf("body contains 'Internal Server Error' marker")
	}

	if err := os.WriteFile(dstPath, body, 0o644); err != nil {
		return err
	}
	return nil
}

func resolveSegmentURL(playlistURL, segment string) (string, error) {
	pu, err := url.Parse(playlistURL)
	if err != nil {
		return "", err
	}
	pu.Path = path.Dir(pu.Path) + "/" + segment
	pu.RawQuery = ""
	pu.Fragment = ""
	return pu.String(), nil
}

func firstFileIn(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return e.Name(), true
		}
	}
	return "", false
}

func atomicWrite(dstPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	tmpPath := dstPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, dstPath)
}
