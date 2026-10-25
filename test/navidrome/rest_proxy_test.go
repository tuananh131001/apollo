package navidrome_test

import (
	"encoding/xml"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/test/internal/vcr"
	"github.com/tuananh131001/apollo/test/utils"
)

// Cassettes are recorded and replayed with the vcr package; see its docs for
// how to re-record them against a real Navidrome.

// Expected values for errors apollo generates when Navidrome is unreachable.
const (
	subsonicVersion    = "1.16.1"
	unavailableMessage = "navidrome is unavailable"
)

// subsonicError, jsonEnvelope, jsonResponse and xmlResponse decode Subsonic
// responses in tests.
type subsonicError struct {
	Code    int    `json:"code" xml:"code,attr"`
	Message string `json:"message" xml:"message,attr"`
}

type jsonEnvelope struct {
	Response jsonResponse `json:"subsonic-response"`
}

type jsonResponse struct {
	Status        string         `json:"status"`
	Version       string         `json:"version"`
	Error         subsonicError  `json:"error"`
	SearchResult3 *searchResult3 `json:"searchResult3"`
}

type searchResult3 struct {
	Song []song `json:"song"`
}

type song struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

type xmlResponse struct {
	XMLName xml.Name      `xml:"subsonic-response"`
	Status  string        `xml:"status,attr"`
	Version string        `xml:"version,attr"`
	Error   subsonicError `xml:"error"`
}

func TestRestProxyForwardsSearch(t *testing.T) {
	proxy, user, password := newRecordedProxy(t, "search_songs")

	// The cassette was recorded against a Navidrome library of three dummy
	// songs by "Test Artist" on "Dummy Album": "Apollo Sunrise",
	// "Apollo Moonwalk" and "Gravity Well". The recorder only matches a
	// request carrying the same query, so a replay also proves it was forwarded.
	query := subsonicQuery(user, password) + "&" + url.Values{
		"query":       {"apollo"},
		"artistCount": {"0"},
		"albumCount":  {"0"},
	}.Encode()
	rec := utils.Serve(proxy, "/rest/search3.view?"+query)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	if resp.Status != "ok" {
		t.Fatalf("subsonic status = %q, want %q; error = %+v", resp.Status, "ok", resp.Error)
	}
	if resp.SearchResult3 == nil {
		t.Fatalf("response has no searchResult3; body = %s", rec.Body)
	}

	got := resp.SearchResult3.Song
	slices.SortFunc(got, func(a, b song) int { return strings.Compare(a.Title, b.Title) })
	want := []song{
		{Title: "Apollo Moonwalk", Artist: "Test Artist", Album: "Dummy Album"},
		{Title: "Apollo Sunrise", Artist: "Test Artist", Album: "Dummy Album"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("songs = %+v, want %+v", got, want)
	}
}

func TestRestProxyPassesNavidromeErrorsThrough(t *testing.T) {
	proxy, user, _ := newRecordedProxy(t, "ping_wrong_password")

	rec := utils.Serve(proxy, "/rest/ping.view?"+subsonicQuery(user, "wrong-password"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	if resp.Status != "failed" {
		t.Errorf("subsonic status = %q, want %q", resp.Status, "failed")
	}
	if resp.Error.Code != 40 {
		t.Errorf("subsonic error code = %d, want 40 (wrong credentials)", resp.Error.Code)
	}
}

func TestRestProxyUnreachable(t *testing.T) {
	proxy := navidrome.NewRestProxy(closedUpstream(t), nil)

	t.Run("json", func(t *testing.T) {
		rec := utils.Serve(proxy, "/rest/ping.view?f=json")

		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
		want := jsonResponse{
			Status:  "failed",
			Version: subsonicVersion,
			Error:   subsonicError{Code: 0, Message: unavailableMessage},
		}
		if resp != want {
			t.Errorf("response = %+v, want %+v", resp, want)
		}
	})

	t.Run("xml", func(t *testing.T) {
		rec := utils.Serve(proxy, "/rest/ping.view")

		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/xml" {
			t.Errorf("Content-Type = %q, want application/xml", got)
		}
		var resp xmlResponse
		if err := xml.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode xml: %v; body = %s", err, rec.Body)
		}
		if resp.Status != "failed" || resp.Version != subsonicVersion ||
			resp.Error != (subsonicError{Code: 0, Message: unavailableMessage}) {
			t.Errorf("response = %+v", resp)
		}
	})
}

// newRecordedProxy returns a RestProxy backed by a go-vcr recorder for the
// named cassette, plus the Subsonic credentials to use for the request.
func newRecordedProxy(t *testing.T, cassetteName string) (*navidrome.RestProxy, string, string) {
	t.Helper()
	s := vcr.Start(t, cassetteName)
	return navidrome.NewRestProxy(s.Upstream, s.Transport), s.User, s.Password
}

func subsonicQuery(user, password string) string {
	return url.Values{
		"u": {user},
		"p": {password},
		"v": {subsonicVersion},
		"c": {"apollo-test"},
		"f": {"json"},
	}.Encode()
}

// closedUpstream returns a URL for a local port that refuses connections.
func closedUpstream(t *testing.T) *url.URL {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return &url.URL{Scheme: "http", Host: addr}
}
