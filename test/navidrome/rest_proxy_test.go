package navidrome_test

import (
	"encoding/json"
	"encoding/xml"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/test/internal/vcr"
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
	Status  string        `json:"status"`
	Version string        `json:"version"`
	Error   subsonicError `json:"error"`
}

type xmlResponse struct {
	XMLName xml.Name      `xml:"subsonic-response"`
	Status  string        `xml:"status,attr"`
	Version string        `xml:"version,attr"`
	Error   subsonicError `xml:"error"`
}

func TestRestProxyForwardsSuccess(t *testing.T) {
	proxy, user, password := newRecordedProxy(t, "ping_ok")

	rec := serve(proxy, "/rest/ping.view?"+subsonicQuery(user, password))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	if got := decodeJSON(t, rec).Status; got != "ok" {
		t.Errorf("subsonic status = %q, want %q", got, "ok")
	}
}

func TestRestProxyPassesNavidromeErrorsThrough(t *testing.T) {
	proxy, user, _ := newRecordedProxy(t, "ping_wrong_password")

	rec := serve(proxy, "/rest/ping.view?"+subsonicQuery(user, "wrong-password"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	resp := decodeJSON(t, rec)
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
		rec := serve(proxy, "/rest/ping.view?f=json")

		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		resp := decodeJSON(t, rec)
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
		rec := serve(proxy, "/rest/ping.view")

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

func serve(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) jsonResponse {
	t.Helper()
	var env jsonEnvelope
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&env); err != nil {
		t.Fatalf("decode json: %v; body = %s", err, rec.Body)
	}
	return env.Response
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
