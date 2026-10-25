// Package server wires the HTTP routes for the apollo service.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/tuananh131001/apollo/internal/version"
)

// NewHandler returns the root HTTP handler.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
}
