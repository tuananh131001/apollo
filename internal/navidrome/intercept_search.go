package navidrome

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

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

// NewInterceptSearchService returns an InterceptSearchService that looks "/s"
// searches up with tracks, after Navidrome at upstream has accepted the
// client's credentials. Navidrome is reached through transport, or
// http.DefaultTransport when transport is nil.
func NewInterceptSearchService(tracks *lastfm.TrackService, upstream *url.URL, transport http.RoundTripper) *InterceptSearchService {
	return &InterceptSearchService{
		tracks: tracks,
		auth:   newAuthenticator(upstream, transport),
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
	// Authenticate like Navidrome would, so a search command fails, and
	// succeeds, with the same envelope as a library search.
	r, nd, ok := s.auth.authorize(w, r, q)
	if !ok {
		return true
	}

	result := &searchResult3{}
	// Last.fm results come back as a single page, so later pages and requests
	// for no songs (clients searching only albums or artists) are left empty.
	limit, wantSongs := songPage(q)
	if track != "" && wantSongs {
		for _, t := range s.tracks.SearchTrack(r.Context(), track, artist, limit) {
			result.Song = append(result.Song, lastFMSong(t.Artist, t.Name))
		}
	}
	nd.SearchResult3 = result
	writeResponse(w, r, http.StatusOK, nd)
	return true
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
