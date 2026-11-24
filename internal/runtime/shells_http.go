package runtime

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
)

// sortShells orders shells by when they started (then by name, as clocks tie).
func sortShells(list []Shell) {
	sort.Slice(list, func(i, j int) bool {
		if !list[i].Started.Equal(list[j].Started) {
			return list[i].Started.Before(list[j].Started)
		}

		return list[i].ID < list[j].ID
	})
}

// ShellsHandlers returns the routes of the shells, by pattern, to mount on
// the daemon's API (api/openapi.yaml):
//
//	GET    /api/shells               the open shells
//	POST   /api/shells               open one
//	DELETE /api/shells/{id}          close one
//	GET    /api/shells/{id}/terminal WebSocket, same protocol as an agent's
//	                                 terminal (TerminalHandler)
func ShellsHandlers(s *Shells, allowedOrigins []string, logger *slog.Logger) map[string]http.Handler {
	return map[string]http.Handler{
		"GET /api/shells": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeShellJSON(w, http.StatusOK, s.List(), logger)
		}),
		"POST /api/shells": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			shell, err := s.Open(r.Context())
			if err != nil {
				writeShellError(w, err, logger)

				return
			}

			writeShellJSON(w, http.StatusCreated, shell, logger)
		}),
		"DELETE /api/shells/{id}": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := s.Close(r.PathValue("id")); err != nil {
				writeShellError(w, err, logger)

				return
			}

			w.WriteHeader(http.StatusNoContent)
		}),
		"GET /api/shells/{id}/terminal": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")

			output, ok := s.Output(id)
			if !ok {
				writeShellError(w, ErrUnknownShell, logger)

				return
			}

			serveTerminal(w, r, terminal{
				output:  output,
				input:   func(data []byte) error { return s.Input(id, data) },
				resize:  func(rows, cols uint16) error { return s.Resize(id, rows, cols) },
				ended:   "the shell ended",
				subject: "shell " + id,
			}, allowedOrigins, logger)
		}),
	}
}

func writeShellError(w http.ResponseWriter, err error, logger *slog.Logger) {
	status := http.StatusInternalServerError

	switch {
	case errors.Is(err, ErrUnknownShell):
		status = http.StatusNotFound
	case errors.Is(err, ErrTooManyShells), errors.Is(err, ErrStopping):
		status = http.StatusConflict
	}

	writeShellJSON(w, status, map[string]string{"error": err.Error()}, logger)
}

func writeShellJSON(w http.ResponseWriter, status int, v any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Warn("write shells response", "err", err)
	}
}
