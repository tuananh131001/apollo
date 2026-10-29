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
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/tuananh131001/apollo/internal/youtube"
)

// Subsonic error codes sent when a Last.fm song cannot be played.
const (
	errorCodeGeneric  = 0
	errorCodeNotFound = 70
)

// errMalformedSongID is reported when a lastfm- id does not decode.
var errMalformedSongID = errors.New("malformed last.fm song id")

// streamEndpoints are the Subsonic endpoints that send a song's audio.
var streamEndpoints = []string{"stream", "stream.view", "download", "download.view"}

// copyBufferSize is how much audio is copied to the client between flushes.
const copyBufferSize = 32 << 10

// NewInterceptStreamService returns an InterceptStreamService that plays
// Last.fm songs with audio, after Navidrome at upstream has accepted the
// client's credentials. A stream ends when the client accepts no audio for
// writeTimeout. Navidrome is reached through transport, or
// http.DefaultTransport when transport is nil.
func NewInterceptStreamService(audio *youtube.AudioService, writeTimeout time.Duration, upstream *url.URL, transport http.RoundTripper) *InterceptStreamService {
	return &InterceptStreamService{
		audio:        audio,
		writeTimeout: writeTimeout,
		auth:         newAuthenticator(upstream, transport),
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
	r, nd, ok := s.auth.authorize(w, r, q)
	if !ok {
		return true
	}

	artist, title, ok := ParseLastFMSongID(id)
	if !ok {
		writeSongError(w, r, nd, id, errMalformedSongID)
		return true
	}
	// The audio is piped as yt-dlp sends it, so it cannot be fetched by range.
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		// Answer a probe without downloading the song.
		w.Header().Set("Content-Type", youtube.ContentType)
		w.WriteHeader(http.StatusOK)
		return true
	}
	slog.Info("downloading last.fm song with yt-dlp",
		"artist", artist,
		"title", title,
		"request_id", middleware.GetReqID(r.Context()),
	)
	audio, err := s.audio.Fetch(r.Context(), artist, title)
	if err != nil {
		writeSongError(w, r, nd, id, err)
		return true
	}
	defer func() { _ = audio.Close() }()

	// Headers go out only once audio has arrived, so any earlier failure is
	// still answered with a Subsonic error.
	w.Header().Set("Content-Type", youtube.ContentType)
	w.WriteHeader(http.StatusOK)
	if err := copyFlushing(w, audio, s.writeTimeout); err != nil && r.Context().Err() == nil {
		slog.Error("last.fm song stream failed", songLogAttrs(r, id, err)...)
	}
	return true
}

// writeSongError answers with a Subsonic error in nd's envelope and logs why
// the song could not be played, unless the client has already gone. A song
// YouTube Music does not have is error 70 and a warning; anything else is a
// failure apollo's operator should look at.
func writeSongError(w http.ResponseWriter, r *http.Request, nd subsonicResponse, id string, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	nd.Status = "failed"
	if errors.Is(err, youtube.ErrNotFound) || errors.Is(err, errMalformedSongID) {
		slog.Warn("last.fm song not found", songLogAttrs(r, id, err)...)
		nd.Error = &subsonicError{Code: errorCodeNotFound, Message: "song not found"}
	} else {
		slog.Error("last.fm song unavailable", songLogAttrs(r, id, err)...)
		nd.Error = &subsonicError{Code: errorCodeGeneric, Message: "song unavailable"}
	}
	writeResponse(w, r, http.StatusOK, nd)
}

// songLogAttrs are the log attributes for a failure to play song id.
func songLogAttrs(r *http.Request, id string, err error) []any {
	return []any{
		"id", id,
		"err", err,
		"request_id", middleware.GetReqID(r.Context()),
	}
}

// copyFlushing copies src to w, flushing after every write so the client
// receives audio as soon as yt-dlp produces it. A write the client does not
// accept within writeTimeout fails, so a client that stops reading cannot
// keep yt-dlp running.
func copyFlushing(w http.ResponseWriter, src io.Reader, writeTimeout time.Duration) error {
	rc := http.NewResponseController(w)
	buf := make([]byte, copyBufferSize)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			// Writers without deadlines, such as test recorders, never block.
			if derr := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); derr != nil && !errors.Is(derr, http.ErrNotSupported) {
				return derr
			}
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
