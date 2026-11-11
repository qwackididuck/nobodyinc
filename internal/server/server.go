// Package server exposes the nobodyd HTTP API.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Health is the liveness payload returned by GET /healthz.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// NewHandler returns the router serving the daemon API.
func NewHandler(version string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(Health{Status: "ok", Version: version}); err != nil {
			logger.Error("encode health response", "err", err)
		}
	})
	return mux
}
