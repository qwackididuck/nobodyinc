package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/projectenv"
)

// The project's environment variables (ADR-0020, docs/projectenv.md): the
// agents' processes get them on top of the daemon's environment. The API
// lists names and takes values, but never returns a value.

var errNoEnv = errors.New("this daemon keeps no environment variables for the project")

// routeEnv mounts the environment endpoints.
func (s *server) routeEnv(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/env", s.listEnv)
	mux.HandleFunc("PUT /api/env/{name}", s.setEnv)
	mux.HandleFunc("DELETE /api/env/{name}", s.deleteEnv)
}

// envVariable is one entry of GET /api/env: a name, never its value.
type envVariable struct {
	Name string `json:"name"`
}

// envValue is the body of PUT /api/env/{name}.
type envValue struct {
	Value string `json:"value"`
}

func (s *server) listEnv(w http.ResponseWriter, _ *http.Request) {
	if !s.hasEnv(w) {
		return
	}

	s.writeJSON(w, http.StatusOK, s.envList())
}

func (s *server) setEnv(w http.ResponseWriter, r *http.Request) {
	if !s.hasEnv(w) {
		return
	}

	var body envValue
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.Env.Set(r.PathValue("name"), body.Value); err != nil {
		s.envError(w, err)

		return
	}

	s.restartAgents(w, r, func() { s.writeJSON(w, http.StatusOK, s.envList()) })
}

func (s *server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	if !s.hasEnv(w) {
		return
	}

	if err := s.Env.Delete(r.PathValue("name")); err != nil {
		s.envError(w, err)

		return
	}

	s.restartAgents(w, r, func() { w.WriteHeader(http.StatusNoContent) })
}

// restartAgents makes every agent restart once idle, to get the new
// environment, then answers with ok. The variables are saved already: when
// agents cannot be restarted, the error says so and they get them at their
// next start.
func (s *server) restartAgents(w http.ResponseWriter, r *http.Request, ok func()) {
	if s.Staff != nil {
		if err := s.Staff.ApplyEveryone(context.WithoutCancel(r.Context())); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)

			return
		}
	}

	ok()
}

func (s *server) envList() []envVariable {
	names := s.Env.Names()
	list := make([]envVariable, 0, len(names))

	for _, name := range names {
		list = append(list, envVariable{Name: name})
	}

	return list
}

func (s *server) hasEnv(w http.ResponseWriter) bool {
	if s.Env == nil {
		s.writeError(w, http.StatusNotImplemented, errNoEnv)

		return false
	}

	return true
}

func (s *server) envError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectenv.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, projectenv.ErrInvalid), errors.Is(err, projectenv.ErrReserved):
		s.writeError(w, http.StatusBadRequest, err)
	default:
		s.writeError(w, http.StatusInternalServerError, err)
	}
}
