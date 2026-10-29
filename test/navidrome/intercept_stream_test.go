package navidrome_test

import (
	"encoding/xml"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/internal/youtube"
	"github.com/tuananh131001/apollo/test/utils"
)

func TestInterceptStreamRejectsCredentialsBeforeRunningYtDlp(t *testing.T) {
	nd := newFakeNavidrome(t)
	ytdlp := newRecordingYtDlp(t)
	proxy := newStreamingProxy(nd.URL, ytdlp.Path)

	rec := utils.Serve(proxy, "/rest/stream.view?"+streamQuery(ndUser, "wrong", navidrome.LastFMSongID("Lena", "Say")))

	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	want := subsonicError{Code: 40, Message: "Wrong username or password"}
	if resp.Status != "failed" || resp.Error != want {
		t.Errorf("response = %+v, want failed with %+v", resp, want)
	}
	if ytdlp.Ran() {
		t.Error("yt-dlp ran for a request with wrong credentials")
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

func TestInterceptStreamRejectsMalformedID(t *testing.T) {
	nd := newFakeNavidrome(t)
	ytdlp := newRecordingYtDlp(t)
	proxy := newStreamingProxy(nd.URL, ytdlp.Path)

	for _, id := range []string{"lastfm-", "lastfm-!!!"} {
		t.Run(id, func(t *testing.T) {
			rec := utils.Serve(proxy, "/rest/stream.view?"+streamQuery(ndUser, ndPassword, id))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
			}
			resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
			if resp.Status != "failed" || resp.Error.Code != 70 {
				t.Errorf("response = %+v, want failed with code 70", resp)
			}
		})
	}
	if ytdlp.Ran() {
		t.Error("yt-dlp ran for a malformed id")
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

func TestInterceptStreamErrorFormat(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newStreamingProxy(nd.URL, newRecordingYtDlp(t).Path)
	query := url.Values{
		"u":  {ndUser},
		"p":  {ndPassword},
		"v":  {subsonicVersion},
		"c":  {"apollo-test"},
		"id": {"lastfm-!!!"},
	}

	t.Run("json", func(t *testing.T) {
		query := maps.Clone(query)
		query.Set("f", "json")
		rec := utils.Serve(proxy, "/rest/stream.view?"+query.Encode())

		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
		if resp.Status != "failed" || resp.Error.Code != 70 {
			t.Errorf("response = %+v, want failed with code 70", resp)
		}
	})

	t.Run("xml", func(t *testing.T) {
		rec := utils.Serve(proxy, "/rest/stream?"+query.Encode())

		if got := rec.Header().Get("Content-Type"); got != "application/xml" {
			t.Errorf("Content-Type = %q, want application/xml", got)
		}
		var resp xmlResponse
		if err := xml.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode xml: %v; body = %s", err, rec.Body)
		}
		if resp.Status != "failed" || resp.Error.Code != 70 {
			t.Errorf("response = %+v, want failed with code 70", resp)
		}
	})
}

func TestInterceptStreamSendsAudio(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newStreamingProxy(nd.URL, newFakeYtDlp(t, ytDlpAudio).Path)

	rec := utils.Serve(proxy, "/rest/stream.view?"+streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say")))

	if rec.Code != http.StatusOK || rec.Body.String() != "fake-m4a-audio" {
		t.Fatalf("response = %d %q, want 200 with the audio", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mp4" {
		t.Errorf("Content-Type = %q, want audio/mp4", got)
	}
	// The audio is piped as yt-dlp sends it, so clients must not seek by range.
	if got := rec.Header().Get("Accept-Ranges"); got != "none" {
		t.Errorf("Accept-Ranges = %q, want none", got)
	}
}

func TestInterceptStreamAnswersHeadWithoutRunningYtDlp(t *testing.T) {
	nd := newFakeNavidrome(t)
	ytdlp := newFakeYtDlp(t, ytDlpAudio)
	proxy := newStreamingProxy(nd.URL, ytdlp.Path)
	target := "/rest/stream.view?" + streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say"))

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, target, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mp4" {
		t.Errorf("Content-Type = %q, want audio/mp4", got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "none" {
		t.Errorf("Accept-Ranges = %q, want none", got)
	}
	if ytdlp.Ran() {
		t.Error("yt-dlp ran for a HEAD request")
	}
	if pings := nd.Pings(); len(pings) != 1 {
		t.Errorf("navidrome pings = %d, want 1 (HEAD is still authenticated)", len(pings))
	}
}

func TestInterceptStreamReportsNoMatchAsNotFound(t *testing.T) {
	nd := newFakeNavidrome(t)
	logs := captureLogs(t)
	proxy := newStreamingProxy(nd.URL, newFakeYtDlp(t, ytDlpNoMatch).Path)

	rec := utils.Serve(proxy, "/rest/stream.view?"+streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say")))

	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	if resp.Status != "failed" || resp.Error.Code != 70 {
		t.Errorf("response = %+v, want failed with code 70", resp)
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || strings.Contains(out, "level=ERROR") {
		t.Errorf("logs = %q, want a warning only", out)
	}
}

func TestInterceptStreamReportsYtDlpFailureAsError(t *testing.T) {
	nd := newFakeNavidrome(t)
	logs := captureLogs(t)
	proxy := newStreamingProxy(nd.URL, newFakeYtDlp(t, ytDlpFails).Path)

	rec := utils.Serve(proxy, "/rest/stream.view?"+streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say")))

	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	if resp.Status != "failed" || resp.Error.Code != 0 {
		t.Errorf("response = %+v, want failed with code 0", resp)
	}
	out := logs.String()
	if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "403") {
		t.Errorf("logs = %q, want an error with yt-dlp's stderr", out)
	}
}

// fakeYtDlp is a stand-in yt-dlp that records that it was run, then runs a
// shell script body.
type fakeYtDlp struct {
	Path   string
	marker string
}

// Bodies for newFakeYtDlp.
const (
	// ytDlpAudio prints a little audio and exits, like a short song.
	ytDlpAudio = "printf 'fake-m4a-audio'"
	// ytDlpNoMatch prints nothing and succeeds, as yt-dlp does when YouTube
	// Music finds no song.
	ytDlpNoMatch = ""
	// ytDlpFails fails before sending audio, as when YouTube blocks a download.
	ytDlpFails = "echo 'ERROR: HTTP Error 403: Forbidden' >&2; exit 1"
)

func newFakeYtDlp(t *testing.T, body string) fakeYtDlp {
	t.Helper()
	dir := t.TempDir()
	y := fakeYtDlp{Path: filepath.Join(dir, "yt-dlp"), marker: filepath.Join(dir, "ran")}
	script := "#!/bin/sh\ntouch '" + y.marker + "'\n" + body + "\n"
	if err := os.WriteFile(y.Path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return y
}

func newRecordingYtDlp(t *testing.T) fakeYtDlp {
	t.Helper()
	return newFakeYtDlp(t, ytDlpNoMatch)
}

func (y fakeYtDlp) Ran() bool {
	_, err := os.Stat(y.marker)
	return err == nil
}

// newStreamingProxy returns a RestProxy in front of the Navidrome at upstream
// that plays Last.fm songs with the yt-dlp at ytdlpPath.
func newStreamingProxy(upstream *url.URL, ytdlpPath string) *navidrome.RestProxy {
	stream := navidrome.NewInterceptStreamService(youtube.NewAudioService(ytdlpPath, 4), time.Minute, upstream, nil)
	return navidrome.NewRestProxy(upstream, nil, stream)
}

// streamQuery returns an authenticated stream query string for id.
func streamQuery(user, password, id string) string {
	return subsonicQuery(user, password) + "&" + url.Values{"id": {id}}.Encode()
}
