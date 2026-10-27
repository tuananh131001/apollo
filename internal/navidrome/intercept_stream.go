package navidrome

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/tuananh131001/apollo/internal/youtube"
)

// errNotFound is Subsonic's "requested data was not found" error, sent when a
// Last.fm song cannot be played.
var errNotFound = subsonicError{Code: 70, Message: "song not found"}

// streamEndpoints are the Subsonic endpoints that send a song's audio.
var streamEndpoints = []string{"stream", "stream.view", "download", "download.view"}

// copyBufferSize is how much audio is copied to the client between flushes.
const copyBufferSize = 32 << 10

// NewInterceptStreamService returns an InterceptStreamService that plays
// Last.fm songs with audio, after Navidrome at upstream has accepted the
// client's credentials. Navidrome is reached through transport, or
// http.DefaultTransport when transport is nil.
func NewInterceptStreamService(audio *youtube.AudioService, upstream *url.URL, transport http.RoundTripper) *InterceptStreamService {
	return &InterceptStreamService{
		audio: audio,
		auth:  newAuthenticator(upstream, transport),
	}
}

// Intercept plays r's song from YouTube Music and returns true when r is a
// stream or download request for a Last.fm song. Otherwise it writes nothing
// and returns false, so the request can be forwarded to Navidrome.
func (s *InterceptStreamService) Intercept(w http.ResponseWriter, r *http.Request) bool {
	if !slices.Contains(streamEndpoints, path.Base(r.URL.Path)) {
		return false
	}
	q := requestParams(r)
	id := q.Get("id")
	if !strings.HasPrefix(id, lastFMSongIDPrefix) {
		return false
	}
	// Answer as if every parameter came in the query string, so those sent in
	// a form body also pick the response format and authenticate the client.
	r = r.Clone(r.Context())
	r.URL.RawQuery = q.Encode()

	nd, err := s.auth.authenticate(r)
	if err != nil {
		writeUnavailable(w, r, err)
		return true
	}
	if nd.Status != "ok" {
		writeResponse(w, r, http.StatusOK, nd)
		return true
	}

	artist, title, ok := ParseLastFMSongID(id)
	if !ok {
		writeSongNotFound(w, r, nd, id, errors.New("malformed last.fm song id"))
		return true
	}
	audio, err := s.audio.Fetch(r.Context(), artist, title)
	if err != nil {
		writeSongNotFound(w, r, nd, id, err)
		return true
	}
	defer func() { _ = audio.Close() }()

	// Headers go out only once audio has arrived, so any earlier failure is
	// still answered with a Subsonic error.
	w.Header().Set("Content-Type", youtube.ContentType)
	w.WriteHeader(http.StatusOK)
	if err := copyFlushing(w, audio); err != nil && r.Context().Err() == nil {
		logSongError(r, id, "last.fm song stream failed", err)
	}
	return true
}

// writeSongNotFound answers with error 70 in nd's envelope and logs why the
// song could not be played, unless the client has already gone.
func writeSongNotFound(w http.ResponseWriter, r *http.Request, nd subsonicResponse, id string, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	logSongError(r, id, "last.fm song not playable", err)
	nd.Status = "failed"
	nd.Error = &subsonicError{Code: errNotFound.Code, Message: errNotFound.Message}
	writeResponse(w, r, http.StatusOK, nd)
}

func logSongError(r *http.Request, id, msg string, err error) {
	slog.Warn(msg,
		"id", id,
		"err", err,
		"request_id", middleware.GetReqID(r.Context()),
	)
}

// copyFlushing copies src to w, flushing after every write so the client
// receives audio as soon as yt-dlp produces it.
func copyFlushing(w http.ResponseWriter, src io.Reader) error {
	rc := http.NewResponseController(w)
	buf := make([]byte, copyBufferSize)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			_ = rc.Flush()
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
