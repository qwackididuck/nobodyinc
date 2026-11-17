package server

import (
	"context"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Teams and the project prompt (docs/agents.md, "The prompt cascade"). The
// changes are made by staffing.Service: the agents they concern restart with
// a fresh session once idle, hence the contexts detached from the request.

// teamChange is the body of PUT /api/teams/{team}. Block is a pointer: an
// absent block list is left as it is, an empty one opens every flow.
type teamChange struct {
	Goal   string    `json:"goal"`
	Prompt string    `json:"prompt"`
	Block  *[]string `json:"block"`
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

	t, err := s.Staff.CreateTeam(context.WithoutCancel(r.Context()),
		org.Team{ID: body.ID, Goal: body.Goal, Prompt: body.Prompt, Block: body.Block})
	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, viewTeam(t))
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

	id := r.PathValue("team")

	change := org.Team{ID: id, Goal: body.Goal, Prompt: body.Prompt}
	if body.Block != nil {
		change.Block = *body.Block
	} else if current, ok := s.Org.Current().Team(id); ok {
		change.Block = current.Block
	}

	t, err := s.Staff.UpdateTeam(context.WithoutCancel(r.Context()), change)
	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, viewTeam(t))
}

// deleteTeam deletes a team and dismisses its members (202: they stop once
// idle, or at once with now=true).
func (s *server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	opts, ok := s.applyOptions(w, r)
	if !ok {
		return
	}

	if err := s.Staff.DeleteTeam(context.WithoutCancel(r.Context()), r.PathValue("team"), opts); err != nil {
		s.staffError(w, err)

		return
	}

	w.WriteHeader(http.StatusAccepted)
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
