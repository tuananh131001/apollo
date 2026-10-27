package navidrome_test

import (
	"encoding/xml"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

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

// recordingYtDlp is a stand-in yt-dlp that only records that it was run.
type recordingYtDlp struct {
	Path   string
	marker string
}

func newRecordingYtDlp(t *testing.T) recordingYtDlp {
	t.Helper()
	dir := t.TempDir()
	y := recordingYtDlp{Path: filepath.Join(dir, "yt-dlp"), marker: filepath.Join(dir, "ran")}
	script := "#!/bin/sh\ntouch '" + y.marker + "'\n"
	if err := os.WriteFile(y.Path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return y
}

func (y recordingYtDlp) Ran() bool {
	_, err := os.Stat(y.marker)
	return err == nil
}

// newStreamingProxy returns a RestProxy in front of the Navidrome at upstream
// that plays Last.fm songs with the yt-dlp at ytdlpPath.
func newStreamingProxy(upstream *url.URL, ytdlpPath string) *navidrome.RestProxy {
	stream := navidrome.NewInterceptStreamService(youtube.NewAudioService(ytdlpPath), upstream, nil)
	return navidrome.NewRestProxy(upstream, nil, stream)
}

// streamQuery returns an authenticated stream query string for id.
func streamQuery(user, password, id string) string {
	return subsonicQuery(user, password) + "&" + url.Values{"id": {id}}.Encode()
}
