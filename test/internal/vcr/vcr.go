// Package vcr records and replays Navidrome HTTP interactions for tests using
// go-vcr cassettes.
//
// Cassettes are replayed by default. To re-record them against a real Navidrome:
//
//	NAVIDROME_RECORD=1 APOLLO_NAVIDROME_URL=http://localhost:4533 \
//	NAVIDROME_USER=admin NAVIDROME_PASSWORD=secret go test ./test/...
//
// Credentials and the upstream host are scrubbed before cassettes are saved.
package vcr

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// replayHost replaces the real Navidrome host in cassettes and in replay mode.
const replayHost = "navidrome.test"

// secretParams are Subsonic auth query parameters that must never be saved.
var secretParams = []string{"u", "p", "t", "s"}

// Session is a recorder for one cassette, plus the upstream and Subsonic
// credentials a test should use with it.
type Session struct {
	Transport http.RoundTripper
	Upstream  *url.URL
	User      string
	Password  string
}

// Start opens the named cassette under testdata/cassettes of the calling test
// package and stops the recorder when the test finishes.
func Start(t *testing.T, cassetteName string) Session {
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

	return Session{Transport: rec, Upstream: upstream, User: user, Password: password}
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
