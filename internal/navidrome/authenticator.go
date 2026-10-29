package navidrome

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// authTimeout bounds the credential check sent to Navidrome.
const authTimeout = 10 * time.Second

// authParams are the Subsonic query parameters that identify and authenticate
// a client. Only these are sent to Navidrome to check a request apollo answers.
var authParams = []string{"u", "p", "t", "s", "apiKey", "v", "c"}

// authenticator checks Subsonic credentials with Navidrome, so requests apollo
// answers itself are accepted and rejected exactly as Navidrome would.
type authenticator struct {
	upstream *url.URL
	client   *http.Client
}

// newAuthenticator returns an authenticator for the Navidrome at upstream,
// reached through transport, or http.DefaultTransport when transport is nil.
func newAuthenticator(upstream *url.URL, transport http.RoundTripper) authenticator {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return authenticator{
		upstream: upstream,
		client:   &http.Client{Transport: transport, Timeout: authTimeout},
	}
}

// authenticate sends r's credentials to Navidrome's ping endpoint and returns
// its reply: status "ok" with Navidrome's server details, or "failed" with the
// error Navidrome gives for those credentials.
func (a authenticator) authenticate(r *http.Request) (subsonicResponse, error) {
	in := r.URL.Query()
	q := url.Values{"f": {"json"}}
	for _, k := range authParams {
		if v, ok := in[k]; ok {
			q[k] = v
		}
	}
	u := a.upstream.JoinPath("rest", "ping.view")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return subsonicResponse{}, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		// The *url.Error wrapper quotes the request URL, which holds credentials.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			return subsonicResponse{}, ue.Err
		}
		return subsonicResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var body jsonEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return subsonicResponse{}, fmt.Errorf("decode navidrome ping (HTTP %d): %w", resp.StatusCode, err)
	}
	if body.Response.Status == "" {
		return subsonicResponse{}, fmt.Errorf("navidrome ping returned no status (HTTP %d)", resp.StatusCode)
	}
	return body.Response, nil
}

// authorize authenticates r, whose Subsonic parameters are q, like Navidrome
// would. It returns a clone of r carrying q in its query string, so parameters
// sent in a form body also pick the response format, and Navidrome's "ok"
// envelope to answer in. When the client is not accepted it writes the failure
// and returns false.
func (a authenticator) authorize(w http.ResponseWriter, r *http.Request, q url.Values) (*http.Request, subsonicResponse, bool) {
	r = r.Clone(r.Context())
	r.URL.RawQuery = q.Encode()

	nd, err := a.authenticate(r)
	if err != nil {
		writeUnavailable(w, r, err)
		return r, nd, false
	}
	if nd.Status != "ok" {
		writeResponse(w, r, http.StatusOK, nd)
		return r, nd, false
	}
	return r, nd, true
}
