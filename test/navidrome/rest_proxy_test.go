package navidrome_test

import (
	"encoding/json"
	"encoding/xml"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/tuananh131001/apollo/internal/navidrome"
)

// Cassettes are replayed by default. To re-record them against a real Navidrome:
//
//	NAVIDROME_RECORD=1 APOLLO_NAVIDROME_URL=http://localhost:4533 \
//	NAVIDROME_USER=admin NAVIDROME_PASSWORD=secret go test ./test/navidrome
//
// Credentials and the upstream host are scrubbed before cassettes are saved.

// replayHost replaces the real Navidrome host in cassettes and in replay mode.
const replayHost = "navidrome.test"

// secretParams are Subsonic auth query parameters that must never be saved.
var secretParams = []string{"u", "p", "t", "s"}

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

	mode := recorder.ModeReplayOnly
	upstream := &url.URL{Scheme: "http", Host: replayHost}
	user, password := "REDACTED", "REDACTED"

	if os.Getenv("NAVIDROME_RECORD") == "1" {
		mode = recorder.ModeRecordOnly
		u, err := url.Parse(os.Getenv("APOLLO_NAVIDROME_URL"))
		if err != nil || u.Host == "" {
			t.Fatalf("APOLLO_NAVIDROME_URL must be set to record cassettes")
		}
		upstream = u
		user, password = os.Getenv("NAVIDROME_USER"), os.Getenv("NAVIDROME_PASSWORD")
	}

	rec, err := recorder.New("testdata/cassettes/"+cassetteName,
		recorder.WithMode(mode),
		recorder.WithMatcher(matchIgnoringSecrets),
		recorder.WithHook(scrubInteraction, recorder.BeforeSaveHook),
		recorder.WithSkipRequestLatency(true),
	)
	if err != nil {
		t.Fatalf("start recorder: %v", err)
	}
	t.Cleanup(func() {
		if err := rec.Stop(); err != nil {
			t.Errorf("stop recorder: %v", err)
		}
	})

	return navidrome.NewRestProxy(upstream, rec), user, password
}

// matchIgnoringSecrets matches on method, path and non-secret query
// parameters, so replay works without credentials or the real host.
func matchIgnoringSecrets(r *http.Request, i cassette.Request) bool {
	recorded, err := url.Parse(i.URL)
	if err != nil {
		return false
	}
	return r.Method == i.Method &&
		r.URL.Path == recorded.Path &&
		withoutSecrets(r.URL.Query()).Encode() == withoutSecrets(recorded.Query()).Encode()
}

// scrubInteraction removes credentials and the real upstream host from an
// interaction before it is written to disk.
func scrubInteraction(i *cassette.Interaction) error {
	u, err := url.Parse(i.Request.URL)
	if err != nil {
		return err
	}
	q := u.Query()
	for _, k := range secretParams {
		if q.Has(k) {
			q.Set(k, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	u.Host = replayHost
	i.Request.URL = u.String()
	i.Request.Host = replayHost
	i.Request.RequestURI = u.RequestURI()
	i.Request.Form = nil

	for _, h := range []string{"Authorization", "Cookie", "X-Forwarded-Host"} {
		i.Request.Headers.Del(h)
	}
	i.Response.Headers.Del("Set-Cookie")
	return nil
}

func withoutSecrets(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = v
	}
	for _, k := range secretParams {
		out.Del(k)
	}
	return out
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
