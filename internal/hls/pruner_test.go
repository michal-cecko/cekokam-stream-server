package hls

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrune_KeepsNewestFoldersByTime(t *testing.T) {
	storage := t.TempDir()
	tsDir := filepath.Join(storage, "streams", "ch", "ts")
	now := time.Now()
	folders := map[string]time.Duration{
		"1790796063":   -time.Hour,
		"seg-ab12-100": -2 * time.Minute,
		"seg-ab12-101": -time.Minute,
		"seg-ab12-102": 0,
	}
	for name, age := range folders {
		dir := filepath.Join(tsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, now.Add(age), now.Add(age)); err != nil {
			t.Fatal(err)
		}
	}

	p := NewPruner(storage, 2, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.prune("ch", p.Log)

	for name, wantKept := range map[string]bool{"1790796063": false, "seg-ab12-100": false, "seg-ab12-101": true, "seg-ab12-102": true} {
		_, err := os.Stat(filepath.Join(tsDir, name))
		if kept := err == nil; kept != wantKept {
			t.Errorf("%s kept = %v, want %v", name, kept, wantKept)
		}
	}
}
