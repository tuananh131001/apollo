package navidrome_test

import (
	"strings"
	"testing"

	"github.com/tuananh131001/apollo/internal/navidrome"
)

func TestLastFMSongIDIsDeterministic(t *testing.T) {
	a := navidrome.LastFMSongID("Lena", "Say")
	b := navidrome.LastFMSongID("Lena", "Say")

	if a != b {
		t.Errorf("ids = %q and %q, want the same id", a, b)
	}
	if !strings.HasPrefix(a, "lastfm-") {
		t.Errorf("id = %q, want lastfm- prefix", a)
	}
}

func TestLastFMSongIDIsUnique(t *testing.T) {
	pairs := [][2]string{
		{"Lena", "Say"},
		{"Lena", "Satellite"},
		{"Justin Bieber", "Say"},
		// Splitting the same text differently must not collide.
		{"a b", "c"},
		{"a", "b c"},
		{"", "ab"},
		{"ab", ""},
	}
	seen := map[string][2]string{}
	for _, p := range pairs {
		id := navidrome.LastFMSongID(p[0], p[1])
		if prev, ok := seen[id]; ok {
			t.Errorf("LastFMSongID(%q, %q) = LastFMSongID(%q, %q) = %q", p[0], p[1], prev[0], prev[1], id)
		}
		seen[id] = p
	}
}

func TestLastFMSongIDRoundTrips(t *testing.T) {
	for _, p := range [][2]string{
		{"Lena", "Say"},
		{"Sơn Tùng M-TP", "Chúng Ta Của Hiện Tại"},
		{"AC/DC", "T.N.T. & \"Friends\" @ 100%"},
		{"", "Untitled"},
	} {
		id := navidrome.LastFMSongID(p[0], p[1])

		artist, title, ok := navidrome.ParseLastFMSongID(id)

		if !ok || artist != p[0] || title != p[1] {
			t.Errorf("ParseLastFMSongID(%q) = (%q, %q, %v), want (%q, %q, true)", id, artist, title, ok, p[0], p[1])
		}
	}
}

func TestParseLastFMSongIDRejectsMalformedIDs(t *testing.T) {
	for _, id := range []string{
		"lastfm-",
		"lastfm-!!!",
		"lastfm-" + "bm90IGpzb24", // base64url of "not json"
		"lastfm-WyJMZW5hIl0",      // base64url of `["Lena"]`, one value
		"2b5f7c8e-0e1a-4c3b",      // a Navidrome id
		"",
	} {
		if artist, title, ok := navidrome.ParseLastFMSongID(id); ok {
			t.Errorf("ParseLastFMSongID(%q) = (%q, %q, true), want not ok", id, artist, title)
		}
	}
}
