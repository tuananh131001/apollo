package navidrome

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/tuananh131001/apollo/internal/lastfm"
)

// searchCommand starts a search query that apollo answers from Last.fm.
const searchCommand = "/s"

// artistSeparator splits a search command into track and artist, as in
// "/s Baby @ Justin Bieber".
const artistSeparator = "@"

// defaultSongCount is search3's songCount when the client does not send one.
const defaultSongCount = 20

// maxFormSize caps the form body read from a POST; larger bodies are not
// searches and are forwarded without being inspected.
const maxFormSize = 64 << 10

// authTimeout bounds the credential check sent to Navidrome.
const authTimeout = 10 * time.Second

// authParams are the Subsonic query parameters that identify and authenticate
// a client. Only these are sent to Navidrome to check a search command.
var authParams = []string{"u", "p", "t", "s", "apiKey", "v", "c"}

// NewInterceptSearchService returns an InterceptSearchService that looks "/s"
// searches up with tracks, after Navidrome at upstream has accepted the
// client's credentials. Navidrome is reached through transport, or
// http.DefaultTransport when transport is nil.
func NewInterceptSearchService(tracks *lastfm.TrackService, upstream *url.URL, transport http.RoundTripper) *InterceptSearchService {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &InterceptSearchService{
		tracks:   tracks,
		upstream: upstream,
		client:   &http.Client{Transport: transport, Timeout: authTimeout},
	}
}

// ParseUserInput reports whether input is a search command, "/s <track>" or
// "/s <track> @ <artist>", and if so returns its track and artist. The artist
// is empty when the command does not name one.
func ParseUserInput(input string) (track, artist string, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(input), searchCommand)
	if !ok || (rest != "" && rest[0] != ' ') {
		return "", "", false
	}
	track, artist, _ = strings.Cut(rest, artistSeparator)
	return strings.TrimSpace(track), strings.TrimSpace(artist), true
}

// Intercept answers r from Last.fm and returns true when it is a search3
// request whose query is a search command. Otherwise it writes nothing and
// returns false, so the request can be forwarded to Navidrome.
func (s *InterceptSearchService) Intercept(w http.ResponseWriter, r *http.Request) bool {
	if name := path.Base(r.URL.Path); name != "search3" && name != "search3.view" {
		return false
	}
	q := requestParams(r)
	track, artist, ok := ParseUserInput(q.Get("query"))
	if !ok {
		return false
	}
	// Answer as if every parameter came in the query string, so those sent in
	// a form body also pick the response format and authenticate the client.
	r = r.Clone(r.Context())
	r.URL.RawQuery = q.Encode()

	// Authenticate like Navidrome would, so a search command fails, and
	// succeeds, with the same envelope as a library search.
	nd, err := s.authenticate(r)
	if err != nil {
		writeUnavailable(w, r, err)
		return true
	}
	if nd.Status != "ok" {
		writeResponse(w, r, http.StatusOK, nd)
		return true
	}

	result := &searchResult3{}
	// Last.fm results come back as a single page, so later pages and requests
	// for no songs (clients searching only albums or artists) are left empty.
	limit, wantSongs := songPage(q)
	if track != "" && wantSongs {
		for _, t := range s.tracks.SearchTrack(r.Context(), track, artist, limit) {
			result.Song = append(result.Song, newLastFMSong(t))
		}
	}
	nd.SearchResult3 = result
	writeResponse(w, r, http.StatusOK, nd)
	return true
}

// authenticate sends r's credentials to Navidrome's ping endpoint and returns
// its reply: status "ok" with Navidrome's server details, or "failed" with the
// error Navidrome gives for those credentials.
func (s *InterceptSearchService) authenticate(r *http.Request) (subsonicResponse, error) {
	in := r.URL.Query()
	q := url.Values{"f": {"json"}}
	for _, k := range authParams {
		if v, ok := in[k]; ok {
			q[k] = v
		}
	}
	u := s.upstream.JoinPath("rest", "ping.view")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return subsonicResponse{}, err
	}
	resp, err := s.client.Do(req)
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

// requestParams returns r's Subsonic parameters: the query string plus, for a
// form POST (the OpenSubsonic formPost extension), the form body. The body is
// put back so the request can still be forwarded to Navidrome unchanged.
func requestParams(r *http.Request) url.Values {
	q := r.URL.Query()
	if r.Method != http.MethodPost || r.Body == nil {
		return q
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/x-www-form-urlencoded" {
		return q
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxFormSize+1))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
	if err != nil || len(body) > maxFormSize {
		return q
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return q
	}
	// Form values come first, as in http.Request.Form.
	for k, v := range q {
		form[k] = append(form[k], v...)
	}
	return form
}

// songPage reads search3's songCount and songOffset parameters. It returns the
// number of songs to fetch and whether the request wants the first page.
func songPage(q url.Values) (limit int, ok bool) {
	limit = defaultSongCount
	if v := q.Get("songCount"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, false
		}
		limit = n
	}
	if v := q.Get("songOffset"); v != "" && v != "0" {
		return 0, false
	}
	return limit, limit > 0
}

// newLastFMSong converts a Last.fm match to a Subsonic song. Last.fm tracks
// are not in Navidrome, so they have no file details; the fields are still
// sent because clients such as Feishin fail on songs without them.
func newLastFMSong(t lastfm.Track) song {
	return song{
		// Last.fm tracks have no Navidrome id; their page URL is unique.
		ID:         t.URL,
		Title:      t.Name,
		Artist:     t.Artist,
		Type:       "music",
		DiscNumber: 1,
	}
}
