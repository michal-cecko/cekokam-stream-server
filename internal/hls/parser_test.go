package hls

import (
	"strings"
	"testing"
)

func TestParse_ExtractsSegmentsAndDuration(t *testing.T) {
	input := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:10",
		"#EXTINF:10.0,",
		"1234.ts",
		"#EXTINF:10.0,",
		"1235.ts",
		"#EXT-X-ENDLIST",
		"",
	}, "\n"))

	pl := Parse(input)

	if pl.ExtinfSecs != 10 {
		t.Errorf("ExtinfSecs = %d, want 10", pl.ExtinfSecs)
	}
	if len(pl.Segments) != 2 || pl.Segments[0] != "1234.ts" || pl.Segments[1] != "1235.ts" {
		t.Errorf("Segments = %v, want [1234.ts 1235.ts]", pl.Segments)
	}
}

func TestRewrite_MatchesPHPOutput(t *testing.T) {
	input := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:10",
		"#EXTINF:10.0,",
		"1234.ts",
		"#EXTINF:10.0,",
		"1235.ts",
		"#EXT-X-ENDLIST",
	}, "\n"))

	pl := Parse(input)
	renamed := map[string]string{
		"1234": "streams/test-channel/ts/1234/" + MD5Hex("1234.ts") + ".ts",
		"1235": "streams/test-channel/ts/1235/" + MD5Hex("1235.ts") + ".ts",
	}

	got := string(Rewrite(pl, "test-channel", "Test Channel", "https://stream.example.com", renamed))

	expected := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:10",
		`#EXTINF:10 tvg-logo="https://stream.example.com/logos/test-channel.png", Test Channel`,
		"ts/1234/" + MD5Hex("1234.ts") + ".ts",
		`#EXTINF:10 tvg-logo="https://stream.example.com/logos/test-channel.png", Test Channel`,
		"ts/1235/" + MD5Hex("1235.ts") + ".ts",
		"#EXT-X-ENDLIST",
	}, "\n")

	if got != expected {
		t.Errorf("Rewrite mismatch.\nWANT:\n%s\n\nGOT:\n%s", expected, got)
	}
}

func TestRewrite_KeepsExtinfWhenSegmentNotRenamed(t *testing.T) {
	input := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXTINF:10.0,",
		"1234.ts",
	}, "\n"))

	pl := Parse(input)

	got := string(Rewrite(pl, "test-channel", "Test", "https://x.example.com", map[string]string{}))

	expected := strings.Join([]string{
		"#EXTM3U",
		`#EXTINF:10 tvg-logo="https://x.example.com/logos/test-channel.png", Test`,
		"1234.ts",
	}, "\n")

	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestPHPAtoi(t *testing.T) {
	cases := map[string]int{
		"10.0,":   10,
		"1234":    1234,
		"  42x":   42,
		"-5":      -5,
		"abc":     0,
		"":        0,
		"+7":      7,
		"0001234": 1234,
	}
	for in, want := range cases {
		if got := phpAtoi(in); got != want {
			t.Errorf("phpAtoi(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPathinfoFilename(t *testing.T) {
	cases := map[string]string{
		"1234.ts":         "1234",
		"/path/seg.ts":    "seg",
		"":                "",
		"foo":             "foo",
		"#EXTM3U":         "#EXTM3U",
		"#EXTINF:10.0,":   "#EXTINF:10",
		"foo.bar.baz":     "foo.bar",
	}
	for in, want := range cases {
		if got := pathinfoFilename(in); got != want {
			t.Errorf("pathinfoFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTSRelative(t *testing.T) {
	got := tsRelative("streams/foo/ts/1234/abc.ts")
	if got != "1234/abc.ts" {
		t.Errorf("tsRelative = %q", got)
	}
}
