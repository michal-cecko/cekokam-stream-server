package hls

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
)

type Playlist struct {
	Lines      []string
	Segments   []string
	ExtinfSecs int
}

func Parse(content []byte) Playlist {
	rawLines := strings.Split(string(content), "\n")
	pl := Playlist{Lines: rawLines}
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case !strings.HasPrefix(line, "#"):
			pl.Segments = append(pl.Segments, line)
		case strings.HasPrefix(line, "#EXTINF:"):
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				pl.ExtinfSecs = phpAtoi(parts[1])
			}
		}
	}
	return pl
}

func Rewrite(pl Playlist, slug, name, publicURL string, renamed map[string]string) []byte {
	logoURL := fmt.Sprintf("%s/logos/%s.png", strings.TrimRight(publicURL, "/"), slug)
	extinfLine := fmt.Sprintf("#EXTINF:%d tvg-logo=\"%s\", %s", pl.ExtinfSecs, logoURL, name)

	out := make([]string, 0, len(pl.Lines))
	for _, line := range pl.Lines {
		line = strings.TrimSpace(line)
		key := pathinfoFilename(line)
		if key != "" {
			if tsFile, ok := renamed[key]; ok && tsFile != "" {
				out = append(out, "ts/"+tsRelative(tsFile))
				continue
			}
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			out = append(out, extinfLine)
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

func MD5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func tsRelative(p string) string {
	idx := strings.Index(p, "/ts/")
	if idx == -1 {
		return p
	}
	return p[idx+len("/ts/"):]
}

func pathinfoFilename(s string) string {
	if s == "" {
		return ""
	}
	base := path.Base(s)
	if base == "/" || base == "." {
		return ""
	}
	ext := path.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func phpAtoi(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	sign := 1
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		if s[i] == '-' {
			sign = -1
		}
		i++
	}
	n := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	return n * sign
}
