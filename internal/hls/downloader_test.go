package hls

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cekokam/cekokam-stream-server/internal/dashboard"
	"github.com/cekokam/cekokam-stream-server/internal/health"
)

// fakeUpstream mimics wan.fajn.tv: the source is a master playlist whose variant
// and segments only work with their own query string.
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/s/key/87/stream.m3u8" && q.Get("token_stream") == "":
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\n"+
				"http://"+r.Host+"/s/key/87/stream.m3u8?device=dev&token_stream=tok\n")
		case r.URL.Path == "/s/key/87/stream.m3u8" && q.Get("token_stream") == "tok":
			io.WriteString(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:12\n#EXT-X-MEDIA-SEQUENCE:100\n"+
				"#EXTINF:6.000000,\nseg-ab12-100.ts?cdn_key=87%3Aab12%3Aseg-ab12-100.ts&token_stream=tok\n"+
				"#EXTINF:6.000000,\nseg-ab12-101.ts?cdn_key=87%3Aab12%3Aseg-ab12-101.ts&token_stream=tok\n")
		case strings.HasPrefix(r.URL.Path, "/s/key/87/seg-") && q.Get("cdn_key") != "":
			io.WriteString(w, "TS:"+r.URL.Path)
		default:
			http.Error(w, "bad request", http.StatusBadRequest)
		}
	}))
}

func TestTick_FollowsMasterPlaylistAndKeepsSegmentQuery(t *testing.T) {
	upstream := fakeUpstream(t)
	defer upstream.Close()

	storage := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDownloader(storage, "https://stream.example.com", time.Second, 5*time.Second, health.New(), logger)
	ch := dashboard.Channel{Slug: "premier-sport-1-hd", Name: "Premier Sport 1 HD", Source: upstream.URL + "/s/key/87/stream.m3u8?device=dev"}

	d.tick(context.Background(), ch, logger)

	seg, err := os.ReadFile(filepath.Join(storage, "streams", ch.Slug, "ts", "seg-ab12-100", MD5Hex("seg-ab12-100.ts?cdn_key=87%3Aab12%3Aseg-ab12-100.ts&token_stream=tok")+".ts"))
	if err != nil {
		t.Fatalf("segment not downloaded: %v", err)
	}
	if string(seg) != "TS:/s/key/87/seg-ab12-100.ts" {
		t.Errorf("segment body = %q", seg)
	}

	manifest, err := os.ReadFile(filepath.Join(storage, "streams", ch.Slug, "stream.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), upstream.URL) || strings.Contains(string(manifest), "token_stream") {
		t.Errorf("manifest leaks the upstream URL:\n%s", manifest)
	}
	if !strings.Contains(string(manifest), "\nts/seg-ab12-101/") {
		t.Errorf("manifest does not point at local segments:\n%s", manifest)
	}
}

func TestResolveURL(t *testing.T) {
	cases := []struct{ base, ref, want string }{
		{"http://10.100.10.252/253-x/143/stream.m3u8", "1790796063.ts", "http://10.100.10.252/253-x/143/1790796063.ts"},
		{"http://h/a/stream.m3u8?device=d", "1234.ts", "http://h/a/1234.ts"},
		{"http://h/a/stream.m3u8?device=d", "seg-1.ts?cdn_key=87%3Aab&t=1", "http://h/a/seg-1.ts?cdn_key=87%3Aab&t=1"},
		{"http://h/a/stream.m3u8", "https://cdn/b/stream.m3u8?t=1", "https://cdn/b/stream.m3u8?t=1"},
		{"http://h/a/stream.m3u8", "/b/1.ts", "http://h/b/1.ts"},
	}
	for _, c := range cases {
		got, err := resolveURL(c.base, c.ref)
		if err != nil || got != c.want {
			t.Errorf("resolveURL(%q, %q) = %q, %v; want %q", c.base, c.ref, got, err, c.want)
		}
	}
}
