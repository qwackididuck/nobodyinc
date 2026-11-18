package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/projects"
)

// windowOpening is the body of POST /api/windows.
type windowOpening struct {
	Project string `json:"project"`
}

// openWindow serves POST /api/windows: the desktop app opens a new window,
// on a project or on the projects page (ADR-0016). It is routed only when
// the app gives Options.OpenWindow: a web view has no browser tabs.
func (l *Launcher) openWindow(w http.ResponseWriter, r *http.Request) {
	var body windowOpening

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %w", err))

		return
	}

	if body.Project != "" {
		if _, err := l.registry.Get(body.Project); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, projects.ErrUnknownProject) {
				status = http.StatusNotFound
			}

			writeError(w, status, err)

			return
		}
	}

	if err := l.opts.OpenWindow(body.Project); err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
