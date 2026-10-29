package navidrome

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/tuananh131001/apollo/internal/deezer"
	"github.com/tuananh131001/apollo/internal/youtube"
)

// NewInterceptSongService returns an InterceptSongService that describes
// Last.fm songs, with their album covers from covers and their lengths from
// audio, after Navidrome at upstream has accepted the client's credentials.
// Navidrome is reached through transport, or http.DefaultTransport when
// transport is nil.
func NewInterceptSongService(covers *deezer.CoverArtService, audio *youtube.AudioService, upstream *url.URL, transport http.RoundTripper) *InterceptSongService {
	return &InterceptSongService{
		covers: covers,
		audio:  audio,
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
	// The song is about to be played, so it gets the length of the audio
	// YouTube Music will send, even when Deezer does not know it. Both lookups
	// run at once.
	yt := make(chan youtube.Song, 1)
	go func() { yt <- s.lookupAudio(r, id, artist, title) }()
	// The cover is looked up as the search did, usually from the cache, so the
	// song matches the one the search returned.
	ctx, cancel := context.WithTimeout(r.Context(), coverLookupTimeout)
	defer cancel()
	dz := s.covers.FindTrack(ctx, artist, title)
	if found := <-yt; found.Duration > 0 {
		dz.Duration = found.Duration
	}
	song := lastFMSong(artist, title, dz)
	nd.Song = &song
	writeResponse(w, r, http.StatusOK, nd)
	return true
}

// lookupAudio returns what YouTube Music knows of artist's song title. A song
// it cannot find is logged and described without its length, as clients may
// look songs up for more than playing them.
func (s *InterceptSongService) lookupAudio(r *http.Request, id, artist, title string) youtube.Song {
	found, err := s.audio.Lookup(r.Context(), artist, title)
	if err != nil && r.Context().Err() == nil {
		slog.Warn("last.fm song length unknown", songLogAttrs(r, id, err)...)
	}
	return found
}
