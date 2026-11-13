package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// agentView is an agent as shown in the UI.
type agentView struct {
	Address string `json:"address"`
	Team    string `json:"team"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	OnLeave bool   `json:"on_leave"`
	From    string `json:"from,omitempty"`
	// PendingChange: a change waits for the agent to end its turn.
	PendingChange bool `json:"pending_change"`
}

func (s *server) listAgents(w http.ResponseWriter, _ *http.Request) {
	members := s.Org.Current().Members()

	agents := make([]agentView, 0, len(members))
	for _, m := range members {
		agents = append(agents, s.agentView(m))
	}

	s.writeJSON(w, http.StatusOK, agents)
}

// teamView is a team as shown in the UI.
type teamView struct {
	ID   string `json:"id"`
	Goal string `json:"goal,omitempty"`
}

// listTeams lists the declared teams, so that the UI can recruit into a team
// that has no agent yet.
func (s *server) listTeams(w http.ResponseWriter, _ *http.Request) {
	teams := s.Org.Current().Teams

	views := make([]teamView, 0, len(teams))
	for _, t := range teams {
		views = append(views, teamView{ID: t.ID, Goal: t.Goal})
	}

	s.writeJSON(w, http.StatusOK, views)
}

func (s *server) agentView(m org.Member) agentView {
	return agentView{
		Address:       m.Address,
		Team:          m.Team.ID,
		Name:          m.Agent.Name,
		Role:          m.Agent.Role,
		Status:        string(s.Statuses.Get(m.Address)),
		OnLeave:       m.Agent.OnLeave,
		From:          m.Agent.From,
		PendingChange: s.Agents != nil && s.Agents.Pending(m.Address),
	}
}

func (s *server) startAgent(w http.ResponseWriter, r *http.Request) {
	if s.Agents == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("agents are not managed by this daemon"))

		return
	}
	// Detached from the request: the agent outlives it.
	err := s.Agents.Start(context.WithoutCancel(r.Context()), r.PathValue("address"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, runtime.ErrUnknownAgent):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, runtime.ErrAlreadyRunning):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.Logger.Error("start agent", "err", err)
		s.writeError(w, http.StatusInternalServerError, err)
	}
}

const (
	defaultMessageLimit = 100
	maxMessageLimit     = 500
)

func (s *server) listMessages(w http.ResponseWriter, r *http.Request) {
	limit := defaultMessageLimit

	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxMessageLimit {
			s.writeError(w, http.StatusBadRequest, errors.New("limit must be between 1 and 500"))

			return
		}

		limit = n
	}

	messages, err := s.Store.RecentMessages(r.Context(), limit)
	if err != nil {
		s.Logger.Error("list messages", "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot list messages"))

		return
	}

	if messages == nil {
		messages = []messaging.Message{}
	}

	s.writeJSON(w, http.StatusOK, messages)
}

// humanDraft is the body of POST /api/messages.
type humanDraft struct {
	To        string             `json:"to"`
	Act       messaging.Act      `json:"act"`
	Priority  messaging.Priority `json:"priority"`
	Subject   string             `json:"subject"`
	Body      string             `json:"body"`
	InReplyTo string             `json:"in_reply_to"`
}

func (s *server) sendMessageAsHuman(w http.ResponseWriter, r *http.Request) {
	var d humanDraft
	if err := decodeJSON(w, r, &d, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	msg, err := s.Messages.Send(r.Context(), messaging.Draft{
		From:      org.HumanAddress,
		To:        d.To,
		Act:       d.Act,
		Priority:  d.Priority,
		Subject:   d.Subject,
		Body:      d.Body,
		InReplyTo: d.InReplyTo,
	})
	if err != nil {
		s.writeError(w, sendErrorStatus(err), err)

		return
	}

	s.writeJSON(w, http.StatusCreated, msg)
}

// sendErrorStatus maps a refused message to an HTTP status: forbidden when the
// rules refuse it, bad request when the message itself is wrong.
func sendErrorStatus(err error) int {
	if errors.Is(err, messaging.ErrNoChannel) || errors.Is(err, messaging.ErrTooManyHops) || errors.Is(err, messaging.ErrNothingToReply) {
		return http.StatusForbidden
	}

	return http.StatusBadRequest
}
