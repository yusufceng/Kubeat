// Package api exposes the HTTP API consumed by the frontend.
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// Health is the response body of the health endpoint.
type Health struct {
	Status  string    `json:"status"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

// NewHandler returns the root HTTP handler of the server.
func NewHandler(version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, Health{
			Status:  "ok",
			Version: version,
			Time:    time.Now().UTC(),
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
