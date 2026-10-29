package navidrome

import (
	"net/http"
	"net/url"
	"path"
	"strings"
)

// NewInterceptSongService returns an InterceptSongService that describes
// Last.fm songs, after Navidrome at upstream has accepted the client's
// credentials. Navidrome is reached through transport, or
// http.DefaultTransport when transport is nil.
func NewInterceptSongService(upstream *url.URL, transport http.RoundTripper) *InterceptSongService {
	return &InterceptSongService{
		auth: newAuthenticator(upstream, transport),
	}
}

// Intercept answers r with its song and returns true when r is a getSong
// request for a Last.fm song. Clients such as Supersonic look a song up this
// way before streaming it. Otherwise it writes nothing and returns false, so
// the request can be forwarded to Navidrome.
func (s *InterceptSongService) Intercept(w http.ResponseWriter, r *http.Request) bool {
	if name := path.Base(r.URL.Path); name != "getSong" && name != "getSong.view" {
		return false
	}
	q := requestParams(r)
	id := q.Get("id")
	if !strings.HasPrefix(id, lastFMSongIDPrefix) {
		return false
	}
	r, nd, ok := s.auth.authorize(w, r, q)
	if !ok {
		return true
	}

	artist, title, ok := ParseLastFMSongID(id)
	if !ok {
		writeSongError(w, r, nd, id, errMalformedSongID)
		return true
	}
	song := lastFMSong(artist, title)
	nd.Song = &song
	writeResponse(w, r, http.StatusOK, nd)
	return true
}
