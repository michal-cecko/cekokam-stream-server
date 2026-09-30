package hls

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cekokam/cekokam-stream-server/internal/dashboard"
)

type Pruner struct {
	StorageDir    string
	PreserveCount int
	Interval      time.Duration
	Log           *slog.Logger
}

func NewPruner(storageDir string, preserveCount int, interval time.Duration, logger *slog.Logger) *Pruner {
	return &Pruner{
		StorageDir:    storageDir,
		PreserveCount: preserveCount,
		Interval:      interval,
		Log:           logger,
	}
}

func (p *Pruner) Run(ctx context.Context, ch dashboard.Channel) {
	logger := p.Log.With(slog.String("slug", ch.Slug), slog.String("component", "pruner"))
	t := time.NewTicker(p.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.prune(ch.Slug, logger)
		}
	}
}

func (p *Pruner) prune(slug string, logger *slog.Logger) {
	tsDir := filepath.Join(p.StorageDir, "streams", slug, "ts")
	entries, err := os.ReadDir(tsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("readdir ts failed", slog.Any("err", err))
		}
		return
	}

	type seqDir struct {
		name    string
		modTime time.Time
	}
	dirs := make([]seqDir, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, seqDir{name: e.Name(), modTime: info.ModTime()})
	}

	if len(dirs) <= p.PreserveCount {
		return
	}

	// Newest first by folder time: segment names are not always numeric (e.g. `seg-33a45d97-9996757`).
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	deleted := 0
	for _, dir := range dirs[p.PreserveCount:] {
		full := filepath.Join(tsDir, dir.name)
		if err := os.RemoveAll(full); err != nil {
			logger.Warn("prune remove failed", slog.String("dir", dir.name), slog.Any("err", err))
			continue
		}
		deleted++
	}

	if deleted > 0 {
		logger.Info("pruned sequence folders", slog.Int("deleted", deleted), slog.Int("kept", p.PreserveCount))
	}
}
