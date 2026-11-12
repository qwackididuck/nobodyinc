package server

import (
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
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
