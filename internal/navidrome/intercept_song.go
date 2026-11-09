package navidrome

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/tuananh131001/apollo/internal/deezer"
)

// NewInterceptSongService returns an InterceptSongService that describes
// Last.fm songs, with their album covers from covers, after Navidrome at
// upstream has accepted the client's credentials. Navidrome is reached through
// transport, or http.DefaultTransport when transport is nil.
func NewInterceptSongService(covers *deezer.CoverArtService, upstream *url.URL, transport http.RoundTripper) *InterceptSongService {
	return &InterceptSongService{
		covers: covers,
		auth:   newAuthenticator(upstream, transport),
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
	// The cover is looked up as the search did, usually from the cache, so the
	// song matches the one the search returned.
	ctx, cancel := context.WithTimeout(r.Context(), coverLookupTimeout)
	defer cancel()
	song := lastFMSong(artist, title, s.covers.FindCover(ctx, artist, title))
	nd.Song = &song
	writeResponse(w, r, http.StatusOK, nd)
	return true
}
