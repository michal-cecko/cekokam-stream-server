package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/cekokam/cekokam-stream-server/internal/health"
)

func New(storageDir string, h *health.State, healthWindow time.Duration) http.Handler {
	mux := http.NewServeMux()

	files := http.FileServer(http.Dir(storageDir))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if h.Healthy(healthWindow) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unhealthy"))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		clean := filepath.ToSlash(filepath.Clean(r.URL.Path))
		if !strings.HasPrefix(clean, "/streams/") && !strings.HasPrefix(clean, "/logos/") {
			http.NotFound(w, r)
			return
		}

		switch strings.ToLower(filepath.Ext(clean)) {
		case ".m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache")
		case ".ts":
			w.Header().Set("Content-Type", "video/mp2t")
			w.Header().Set("Cache-Control", "public, max-age=10")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=300")
		}

		files.ServeHTTP(w, r)
	})

	return mux
}
