package server

import (
	"context"
	"net/http"
)

// Teams and the project prompt (docs/agents.md, "The prompt cascade"). The
// changes are made by staffing.Service: the agents they concern restart with
// a fresh session once idle, hence the contexts detached from the request.

// teamChange is the body of PUT /api/teams/{team}.
type teamChange struct {
	Goal   string `json:"goal"`
	Prompt string `json:"prompt"`
}

// projectView is the project's shared prompt (.nobody/project.md).
type projectView struct {
	Prompt string `json:"prompt"`
}

func (s *server) createTeam(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body teamView
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	t, err := s.Staff.CreateTeam(context.WithoutCancel(r.Context()), body.ID, body.Goal, body.Prompt)
	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, teamView{ID: t.ID, Goal: t.Goal, Prompt: t.Prompt})
}

func (s *server) updateTeam(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body teamChange
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	t, err := s.Staff.UpdateTeam(context.WithoutCancel(r.Context()), r.PathValue("team"), body.Goal, body.Prompt)
	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, teamView{ID: t.ID, Goal: t.Goal, Prompt: t.Prompt})
}

func (s *server) getProject(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, projectView{Prompt: s.Org.Current().ProjectPrompt})
}

func (s *server) updateProject(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body projectView
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.Staff.SetProjectPrompt(context.WithoutCancel(r.Context()), body.Prompt); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, projectView{Prompt: s.Org.Current().ProjectPrompt})
}
