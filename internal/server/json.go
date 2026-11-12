package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// maxBodyBytes bounds request bodies: messages and hook events are small.
const maxBodyBytes = 1 << 20

type errorBody struct {
	Error string `json:"error"`
}

func (s *server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.Logger.Error("encode response", "err", err)
	}
}

func (s *server) writeError(w http.ResponseWriter, status int, err error) {
	s.writeJSON(w, status, errorBody{Error: err.Error()})
}

// decodeJSON reads a JSON body into v. When strict is true, unknown fields are
// refused (bodies we define); hook events come from Claude Code and may carry
// fields we do not use, so they are decoded leniently.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any, strict bool) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if strict {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}

	return nil
}
