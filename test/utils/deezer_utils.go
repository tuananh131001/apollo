package utils

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tuananh131001/apollo/internal/deezer"
)

// SayCover is the Deezer cover id of Lena's "Say".
const SayCover = "911a6e5e5e5d64dff4e73094beae7783"

// NewDeezer returns a CoverArtService whose API calls, under /api/, and image
// fetches, under /images/cover/, go to a test server running handler.
func NewDeezer(t *testing.T, handler http.HandlerFunc) *deezer.CoverArtService {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)
	return newDeezer(base, nil)
}

// NewDeezerOn returns a CoverArtService whose requests, to the same paths as
// NewDeezer's, are answered by transport without a network, as synctest
// bubbles need.
func NewDeezerOn(transport http.RoundTripper) *deezer.CoverArtService {
	return newDeezer(&url.URL{Scheme: "http", Host: "deezer.test"}, transport)
}

func newDeezer(base *url.URL, transport http.RoundTripper) *deezer.CoverArtService {
	return deezer.NewCoverArtService(base.JoinPath("api/"), base.JoinPath("images/cover/"), transport)
}
