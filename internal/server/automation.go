package server

import (
	"errors"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/automation"
)

// automationState is the body of GET and PUT /api/automation.
type automationState struct {
	Level automation.Level `json:"level"`
}

func (s *server) routeAutomation(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/automation", s.getAutomation)
	mux.HandleFunc("PUT /api/automation", s.setAutomation)
}

func (s *server) getAutomation(w http.ResponseWriter, _ *http.Request) {
	level := automation.Off
	if s.Automation != nil {
		level = s.Automation.Level()
	}

	s.writeJSON(w, http.StatusOK, automationState{Level: level})
}

func (s *server) setAutomation(w http.ResponseWriter, r *http.Request) {
	if s.Automation == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("automatic mode is not available"))

		return
	}

	var body automationState
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if _, err := s.Automation.Set(body.Level); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	s.Logger.Warn("automatic mode set", "level", body.Level)

	if s.Automation.AutoApprove() {
		s.allowPending()
	}

	s.writeJSON(w, http.StatusOK, automationState{Level: s.Automation.Level()})
}

// allowPending answers the requests already waiting when the mode is switched
// on: the human chose to stop being asked.
func (s *server) allowPending() {
	for _, req := range s.Approvals.Pending() {
		// ErrNotFound: the human or the terminal answered it in the meantime.
		if err := s.Approvals.Decide(req.ID, approval.Decision{Allow: true}); err != nil && !errors.Is(err, approval.ErrNotFound) {
			s.Logger.Error("allowing a pending approval", "id", req.ID, "err", err)
		}
	}
}
