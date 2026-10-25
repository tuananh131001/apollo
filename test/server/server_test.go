package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tuananh131001/apollo/internal/server"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	server.NewHandler(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// The REST proxy forwards RemoteAddr to Navidrome as X-Forwarded-For, so it
// must stay the TCP peer rather than an IP the client claims in a header.
func TestRestIgnoresClientIPHeaders(t *testing.T) {
	const peer = "198.51.100.10:4321"

	var got string
	restProxy := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	})

	req := httptest.NewRequest(http.MethodGet, "/rest/ping", nil)
	req.RemoteAddr = peer
	req.Header.Set("True-Client-IP", "203.0.113.1")
	req.Header.Set("X-Real-IP", "203.0.113.2")
	req.Header.Set("X-Forwarded-For", "203.0.113.3")
	server.NewHandler(restProxy).ServeHTTP(httptest.NewRecorder(), req)

	if got != peer {
		t.Fatalf("RemoteAddr = %q, want %q", got, peer)
	}
}
