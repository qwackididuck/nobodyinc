package server

import (
	"errors"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
	"github.com/qwackididuck/nobodyinc/internal/workspace"
)

func (s *server) listFeatures(w http.ResponseWriter, r *http.Request) {
	features, err := s.Tickets.Store.ListFeatures(r.Context())
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, append([]work.Feature{}, features...))
}

// featureDraft is the body of POST /api/features.
type featureDraft struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	BaseBranch  string `json:"base_branch"`
}

func (s *server) startFeature(w http.ResponseWriter, r *http.Request) {
	var d featureDraft
	if err := decodeJSON(w, r, &d, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	f, err := s.Tickets.StartFeature(r.Context(), org.HumanAddress, work.FeatureDraft{
		Title: d.Title, Description: d.Description, BaseBranch: d.BaseBranch,
	})
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, f)
}

// startProposedFeature starts a feature the director proposed.
func (s *server) startProposedFeature(w http.ResponseWriter, r *http.Request) {
	f, err := s.Tickets.StartProposedFeature(r.Context(), org.HumanAddress, r.PathValue("id"))
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, f)
}

// refusal is the body of POST /api/features/{id}/refuse.
type refusal struct {
	Reason string `json:"reason"`
}

// refuseProposedFeature turns down a feature the director proposed.
func (s *server) refuseProposedFeature(w http.ResponseWriter, r *http.Request) {
	var body refusal
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	f, err := s.Tickets.RefuseFeature(r.Context(), org.HumanAddress, r.PathValue("id"), body.Reason)
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, f)
}

// reviewFeature shows what a started feature brings to its base branch.
func (s *server) reviewFeature(w http.ResponseWriter, r *http.Request) {
	review, err := s.Tickets.ReviewFeature(r.Context(), r.PathValue("id"))
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, review)
}

// finishing is the body of POST /api/features/{id}/finish.
type finishing struct {
	Force bool `json:"force"`
}

// finishFeature merges the active feature into its base branch and ends it
// (ADR-0007).
func (s *server) finishFeature(w http.ResponseWriter, r *http.Request) {
	var body finishing
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	f, err := s.Tickets.FinishFeature(r.Context(), org.HumanAddress, r.PathValue("id"), body.Force)
	if err != nil {
		s.ticketError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, f)
}

// isFinishRefusal tells whether git refused to finish a feature for a reason
// the human can fix (uncommitted changes, a conflict...): nothing changed.
func isFinishRefusal(err error) bool {
	return errors.Is(err, workspace.ErrBaseDirty) || errors.Is(err, workspace.ErrBaseElsewhere) ||
		errors.Is(err, workspace.ErrBaseConflict) || errors.Is(err, workspace.ErrNoBaseBranch)
}
