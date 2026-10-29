package navidrome_test

// These tests play Last.fm songs end to end: a real Navidrome checks
// credentials and a real yt-dlp fetches audio from YouTube Music. Last.fm stays
// faked. They are skipped unless a Navidrome and yt-dlp are available:
//
//	APOLLO_NAVIDROME_URL=http://localhost:4533 \
//	NAVIDROME_USER=admin NAVIDROME_PASSWORD=secret \
//	APOLLO_YTDLP_PATH=/path/to/yt-dlp go test ./test/navidrome/

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuananh131001/apollo/internal/lastfm"
	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/internal/server"
	"github.com/tuananh131001/apollo/internal/youtube"
)

// minAudioSize is the least a full song should weigh at ~128 kbps.
const minAudioSize = 1 << 20

// ytMusicSearch is in the command line of every yt-dlp apollo runs.
const ytMusicSearch = "music.youtube.com/search"

func TestLiveSearchThenPlay(t *testing.T) {
	live := startLiveApollo(t)

	songs := live.search(t, "/s SAY @ Lena")
	if len(songs) == 0 || !strings.HasPrefix(songs[0].ID, "lastfm-") {
		t.Fatalf("songs = %+v, want a lastfm- id", songs)
	}
	resp := live.get(t, context.Background(), "/rest/stream.view", songs[0].ID)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read audio: %v", err)
	}

	assertAudio(t, resp, body[:min(len(body), 64)])
	if len(body) < minAudioSize {
		t.Errorf("audio = %d bytes, want at least %d", len(body), minAudioSize)
	}
}

func TestLiveStreamEndpointsReturnSameAudio(t *testing.T) {
	live := startLiveApollo(t)
	id := navidrome.LastFMSongID("Lena", "Say")

	var first []byte
	for _, endpoint := range []string{"/rest/stream.view", "/rest/stream", "/rest/download.view"} {
		t.Run(endpoint, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resp := live.get(t, ctx, endpoint, id)
			defer func() { _ = resp.Body.Close() }()
			head := make([]byte, 256<<10)
			if _, err := io.ReadFull(resp.Body, head); err != nil {
				t.Fatalf("read audio: %v", err)
			}

			assertAudio(t, resp, head)
			if first == nil {
				first = head
			} else if !bytes.Equal(head, first) {
				t.Errorf("%s audio differs from /rest/stream.view", endpoint)
			}
		})
	}
}

func TestLiveSongWithoutMatchDoesNotBreakServer(t *testing.T) {
	live := startLiveApollo(t)
	logs := captureLogs(t)
	// YouTube Music finds nothing for a query this long.
	title := strings.Repeat("x", 600)

	resp := live.get(t, context.Background(), "/rest/stream.view", navidrome.LastFMSongID("", title))
	defer func() { _ = resp.Body.Close() }()

	var env jsonEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode json: %v; Content-Type = %q", err, resp.Header.Get("Content-Type"))
	}
	if env.Response.Status != "failed" || env.Response.Error.Code != 70 {
		t.Errorf("response = %+v, want failed with code 70", env.Response)
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "request_id=") {
		t.Errorf("logs = %q, want a warning with the request id", out)
	}
	waitForNoYtDlp(t, title)
	// The server still answers.
	if songs := live.search(t, "/s SAY @ Lena"); len(songs) == 0 {
		t.Error("search after a failed stream returned no songs")
	}
}

func TestLiveClientDisconnectStopsYtDlp(t *testing.T) {
	live := startLiveApollo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp := live.get(t, ctx, "/rest/stream.view", navidrome.LastFMSongID("Lena", "Say"))
	head := make([]byte, 64)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		t.Fatalf("read audio: %v", err)
	}
	if len(ytDlpProcesses("Lena")) == 0 {
		t.Fatal("no yt-dlp process while streaming")
	}
	cancel()
	_ = resp.Body.Close()

	waitForNoYtDlp(t, "Lena")
}

func TestLiveLibrarySongStreamsUnchanged(t *testing.T) {
	live := startLiveApollo(t)
	q := live.authQuery()
	q.Set("size", "1")
	random, err := http.Get(live.navidrome.JoinPath("rest", "getRandomSongs.view").String() + "?" + q.Encode())
	if err != nil {
		t.Fatalf("getRandomSongs: %v", err)
	}
	defer func() { _ = random.Body.Close() }()
	var env struct {
		Response struct {
			RandomSongs struct {
				Song []struct {
					ID string `json:"id"`
				} `json:"song"`
			} `json:"randomSongs"`
		} `json:"subsonic-response"`
	}
	if err := json.NewDecoder(random.Body).Decode(&env); err != nil {
		t.Fatalf("decode getRandomSongs: %v", err)
	}
	if len(env.Response.RandomSongs.Song) == 0 {
		t.Skip("navidrome library has no songs")
	}
	id := env.Response.RandomSongs.Song[0].ID

	viaApollo := live.get(t, context.Background(), "/rest/stream.view", id)
	defer func() { _ = viaApollo.Body.Close() }()
	q = live.authQuery()
	q.Set("id", id)
	direct, err := http.Get(live.navidrome.JoinPath("rest", "stream.view").String() + "?" + q.Encode())
	if err != nil {
		t.Fatalf("stream from navidrome: %v", err)
	}
	defer func() { _ = direct.Body.Close() }()

	got, _ := io.ReadAll(viaApollo.Body)
	want, _ := io.ReadAll(direct.Body)
	if viaApollo.StatusCode != http.StatusOK || len(want) == 0 || !bytes.Equal(got, want) {
		t.Errorf("via apollo: status %d, %d bytes; want navidrome's %d bytes unchanged",
			viaApollo.StatusCode, len(got), len(want))
	}
	if g, w := viaApollo.Header.Get("Content-Type"), direct.Header.Get("Content-Type"); g != w {
		t.Errorf("Content-Type = %q, want %q", g, w)
	}
}

// liveApollo is apollo's full HTTP handler in front of a real Navidrome and
// yt-dlp, with Last.fm answering every search with "Say" by Lena.
type liveApollo struct {
	url       string
	navidrome *url.URL
	user      string
	password  string
}

func startLiveApollo(t *testing.T) liveApollo {
	t.Helper()
	nd, err := url.Parse(os.Getenv("APOLLO_NAVIDROME_URL"))
	user, password := os.Getenv("NAVIDROME_USER"), os.Getenv("NAVIDROME_PASSWORD")
	if err != nil || nd.Host == "" || user == "" {
		t.Skip("set APOLLO_NAVIDROME_URL, NAVIDROME_USER and NAVIDROME_PASSWORD to run live tests")
	}
	ytdlp := os.Getenv("APOLLO_YTDLP_PATH")
	if ytdlp == "" {
		ytdlp = "yt-dlp"
	}
	if _, err := exec.LookPath(ytdlp); err != nil {
		t.Skipf("yt-dlp not found (%v); set APOLLO_YTDLP_PATH", err)
	}
	if conn, err := net.DialTimeout("tcp", "music.youtube.com:443", 5*time.Second); err != nil {
		t.Skipf("YouTube Music unreachable: %v", err)
	} else {
		_ = conn.Close()
	}

	lastfmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	}))
	t.Cleanup(lastfmSrv.Close)
	base, _ := url.Parse(lastfmSrv.URL + "/2.0/")
	tracks := lastfm.NewTrackService("test-api-key", base, nil)

	proxy := navidrome.NewRestProxy(nd, nil,
		navidrome.NewInterceptSearchService(tracks, nd, nil),
		navidrome.NewInterceptStreamService(youtube.NewAudioService(ytdlp, 4), time.Minute, nd, nil),
	)
	srv := httptest.NewServer(server.NewHandler(proxy))
	t.Cleanup(srv.Close)
	return liveApollo{url: srv.URL, navidrome: nd, user: user, password: password}
}

func (a liveApollo) authQuery() url.Values {
	return url.Values{
		"u": {a.user},
		"p": {a.password},
		"v": {subsonicVersion},
		"c": {"apollo-test"},
		"f": {"json"},
	}
}

// get requests endpoint for song id through apollo.
func (a liveApollo) get(t *testing.T, ctx context.Context, endpoint, id string) *http.Response {
	t.Helper()
	q := a.authQuery()
	q.Set("id", id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", endpoint, err)
	}
	return resp
}

// search sends a search3 query through apollo and returns its songs.
func (a liveApollo) search(t *testing.T, query string) []lastfmSong {
	t.Helper()
	q := a.authQuery()
	q.Set("query", query)
	resp, err := http.Get(a.url + "/rest/search3.view?" + q.Encode())
	if err != nil {
		t.Fatalf("search3: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var env lastfmEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode search3: %v", err)
	}
	if env.Response.Status != "ok" || env.Response.SearchResult3 == nil {
		t.Fatalf("search3 response = %+v", env.Response)
	}
	return env.Response.SearchResult3.Song
}

// assertAudio checks resp is m4a audio starting with head.
func assertAudio(t *testing.T, resp *http.Response, head []byte) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "audio/mp4" {
		t.Errorf("Content-Type = %q, want audio/mp4", got)
	}
	if len(head) < 8 || string(head[4:8]) != "ftyp" {
		t.Errorf("audio starts %q, want an MP4 ftyp box", head[:min(len(head), 8)])
	}
}

// ytDlpProcesses returns the pids of yt-dlp processes searching YouTube Music
// for a query containing term.
func ytDlpProcesses(term string) []string {
	var pids []string
	paths, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, p := range paths {
		cmdline, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if s := string(cmdline); strings.Contains(s, ytMusicSearch) && strings.Contains(s, term) {
			pids = append(pids, filepath.Base(filepath.Dir(p)))
		}
	}
	return pids
}

// waitForNoYtDlp fails the test if a yt-dlp for term is still running after
// two seconds.
func waitForNoYtDlp(t *testing.T, term string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pids := ytDlpProcesses(term)
		if len(pids) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("yt-dlp still running: pids %v", pids)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// captureLogs sends slog's default logger to a buffer for the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
