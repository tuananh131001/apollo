package navidrome

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/tuananh131001/apollo/internal/deezer"
)

// coverArtCacheControl lets clients keep a cover for good: the image behind a
// Deezer cover id never changes.
const coverArtCacheControl = "public, max-age=31536000, immutable"

// NewInterceptCoverArtService returns an InterceptCoverArtService that fetches
// Last.fm songs' covers with covers, after Navidrome at upstream has accepted
// the client's credentials. Navidrome is reached through transport, or
// http.DefaultTransport when transport is nil.
func NewInterceptCoverArtService(covers *deezer.CoverArtService, upstream *url.URL, transport http.RoundTripper) *InterceptCoverArtService {
	return &InterceptCoverArtService{
		covers: covers,
		auth:   newAuthenticator(upstream, transport),
	}
}

// Intercept answers r with the cover image and returns true when r is a
// getCoverArt request for a Last.fm song's cover. Otherwise it writes nothing
// and returns false, so the request can be forwarded to Navidrome.
func (s *InterceptCoverArtService) Intercept(w http.ResponseWriter, r *http.Request) bool {
	if name := path.Base(r.URL.Path); name != "getCoverArt" && name != "getCoverArt.view" {
		return false
	}
	q := requestParams(r)
	id := q.Get("id")
	cover, ok := strings.CutPrefix(id, coverArtIDPrefix)
	if !ok {
		return false
	}
	r, nd, ok := s.auth.authorize(w, r, q)
	if !ok {
		return true
	}

	// A size that is not a number gets the default size, as in Navidrome.
	size, _ := strconv.Atoi(q.Get("size"))
	resp, err := s.covers.Fetch(r.Context(), cover, size)
	if err != nil {
		writeCoverArtError(w, r, nd, id, err)
		return true
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if n := resp.Header.Get("Content-Length"); n != "" {
		w.Header().Set("Content-Length", n)
	}
	w.Header().Set("Cache-Control", coverArtCacheControl)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return true
	}
	if _, err := io.Copy(w, resp.Body); err != nil && r.Context().Err() == nil {
		slog.Error("last.fm cover art copy failed", songLogAttrs(r, id, err)...)
	}
	return true
}

// writeCoverArtError answers with a Subsonic error in nd's envelope and logs
// why cover id could not be fetched, unless the client has already gone. A
// cover Deezer does not have is error 70 and a warning; anything else is a
// failure apollo's operator should look at.
func writeCoverArtError(w http.ResponseWriter, r *http.Request, nd subsonicResponse, id string, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	nd.Status = "failed"
	if errors.Is(err, deezer.ErrNotFound) {
		slog.Warn("last.fm cover art not found", songLogAttrs(r, id, err)...)
		nd.Error = &subsonicError{Code: errorCodeNotFound, Message: "cover art not found"}
	} else {
		slog.Error("last.fm cover art unavailable", songLogAttrs(r, id, err)...)
		nd.Error = &subsonicError{Code: errorCodeGeneric, Message: "cover art unavailable"}
	}
	writeResponse(w, r, http.StatusOK, nd)
}
