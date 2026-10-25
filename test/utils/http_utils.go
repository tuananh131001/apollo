// Package utils holds HTTP helpers shared by the test packages.
package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Serve sends a GET request for target to h and returns the recorded response.
func Serve(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// DecodeJSON decodes the recorded response body into a T, failing the test if
// the body is not valid JSON.
func DecodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode json: %v; body = %s", err, rec.Body)
	}
	return v
}
