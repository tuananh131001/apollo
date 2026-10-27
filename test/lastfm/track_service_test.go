package lastfm_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/tuananh131001/apollo/internal/lastfm"
)

const apiKey = "test-api-key"

const sayByLena = `{"results":{"opensearch:totalResults":"1","trackmatches":{"track":[
	{"name":"Say","artist":"Lena","url":"https://www.last.fm/music/Lena/_/Say","listeners":"1234","mbid":"abc"}
]}}}`

func TestSearchTrackSendsQuery(t *testing.T) {
	var got url.Values
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(sayByLena))
	})

	tracks := svc.SearchTrack(t.Context(), "say", "lena", 0)

	want := url.Values{
		"method":  {"track.search"},
		"track":   {"say"},
		"artist":  {"lena"},
		"api_key": {apiKey},
		"format":  {"json"},
	}
	if got.Encode() != want.Encode() {
		t.Errorf("query = %s, want %s", got.Encode(), want.Encode())
	}
	wantTracks := []lastfm.Track{{Name: "Say", Artist: "Lena", URL: "https://www.last.fm/music/Lena/_/Say", MBID: "abc"}}
	if !slices.Equal(tracks, wantTracks) {
		t.Errorf("tracks = %+v, want %+v", tracks, wantTracks)
	}
}

func TestSearchTrackDecodesRealResponse(t *testing.T) {
	// The first three matches of a real track.search for "say" by "lena".
	body, err := os.ReadFile("testdata/track_search_say_lena.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	svc := newService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})

	tracks := svc.SearchTrack(t.Context(), "say", "lena", 0)

	want := []lastfm.Track{
		{Name: "SAY", Artist: "Lena", URL: "https://www.last.fm/music/Lena/_/SAY"},
		{Name: "SAY (feat. T.R.I)", Artist: "Lena", URL: "https://www.last.fm/music/Lena/_/SAY+(feat.+T.R.I)", MBID: "212c9d9b-1e97-454c-871f-140c0d20bb30"},
		{Name: "Day to Say", Artist: "Lena", URL: "https://www.last.fm/music/Lena/_/Day+to+Say"},
	}
	if !slices.Equal(tracks, want) {
		t.Errorf("tracks = %+v, want %+v", tracks, want)
	}
}

func TestSearchTrackOmitsEmptyArtist(t *testing.T) {
	var got url.Values
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(sayByLena))
	})

	svc.SearchTrack(t.Context(), "say", "", 0)

	if got.Has("artist") {
		t.Errorf("artist = %q, want no artist parameter", got.Get("artist"))
	}
}

func TestSearchTrackSendsLimit(t *testing.T) {
	var got url.Values
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(sayByLena))
	})

	svc.SearchTrack(t.Context(), "say", "", 5)

	if got.Get("limit") != "5" {
		t.Errorf("limit = %q, want 5", got.Get("limit"))
	}
}

func TestSearchTrackNoMatches(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":{"opensearch:totalResults":"0","trackmatches":{"track":[]}}}`))
	})

	if tracks := svc.SearchTrack(t.Context(), "zzzz", "", 0); len(tracks) != 0 {
		t.Errorf("tracks = %+v, want none", tracks)
	}
}

func TestSearchTrackLogsFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLog string
	}{
		{
			name: "last.fm error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":10,"message":"Invalid API key - You must be granted a valid key by last.fm"}`))
			},
			wantLog: "last.fm error 10: Invalid API key",
		},
		{
			name: "http error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{}`))
			},
			wantLog: "unexpected HTTP status 500",
		},
		{
			name: "invalid body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`<html>`))
			},
			wantLog: "decode response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			svc := newService(t, tt.handler)

			if tracks := svc.SearchTrack(t.Context(), "say", "", 0); len(tracks) != 0 {
				t.Errorf("tracks = %+v, want none", tracks)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("logs = %q, want them to contain %q", logs, tt.wantLog)
			}
		})
	}
}

func TestSearchTrackUnreachableKeepsAPIKeyOutOfLogs(t *testing.T) {
	logs := captureLogs(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	base, _ := url.Parse(srv.URL)
	svc := lastfm.NewTrackService(apiKey, base, nil)

	if tracks := svc.SearchTrack(t.Context(), "say", "", 0); len(tracks) != 0 {
		t.Errorf("tracks = %+v, want none", tracks)
	}
	if !strings.Contains(logs.String(), "lastfm track search failed") {
		t.Errorf("logs = %q, want a search failure", logs)
	}
	if strings.Contains(logs.String(), apiKey) {
		t.Errorf("logs = %q, must not contain the API key", logs)
	}
}

// newService returns a TrackService pointed at a test server running handler.
func newService(t *testing.T, handler http.HandlerFunc) *lastfm.TrackService {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL + "/2.0/")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return lastfm.NewTrackService(apiKey, base, nil)
}

// captureLogs redirects the default slog logger into a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}
