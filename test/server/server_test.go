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
