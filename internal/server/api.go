package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/qwackididuck/nobodyinc/internal/events"
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
	// Expertise: what to ask this agent about, and what it decides.
	Expertise string `json:"expertise,omitempty"`
	Status    string `json:"status"`
	OnLeave   bool   `json:"on_leave"`
	From      string `json:"from,omitempty"`
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

// teamView is a team as shown in the UI, and the body of POST /api/teams.
type teamView struct {
	ID     string `json:"id"`
	Goal   string `json:"goal,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	// Block lists the teams its members may not start a conversation with
	// (ADR-0009); always present, empty when flows are fully open.
	Block []string `json:"block"`
}

// viewTeam turns a team into its view.
func viewTeam(t org.Team) teamView {
	return teamView{ID: t.ID, Goal: t.Goal, Prompt: t.Prompt, Block: append([]string{}, t.Block...)}
}

// listTeams lists the declared teams, so that the UI can recruit into a team
// that has no agent yet.
func (s *server) listTeams(w http.ResponseWriter, _ *http.Request) {
	teams := s.Org.Current().Teams

	views := make([]teamView, 0, len(teams))
	for _, t := range teams {
		views = append(views, viewTeam(t))
	}

	s.writeJSON(w, http.StatusOK, views)
}

func (s *server) agentView(m org.Member) agentView {
	return agentView{
		Address:       m.Address,
		Team:          m.Team.ID,
		Name:          m.Agent.Name,
		Role:          m.Agent.Role,
		Expertise:     m.Agent.Expertise,
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
	case errors.Is(err, runtime.ErrAlreadyRunning), errors.Is(err, runtime.ErrOnLeave), errors.Is(err, runtime.ErrStopping):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.Logger.Error("start agent", "err", err)
		s.writeError(w, http.StatusInternalServerError, err)
	}
}

// restartAgent stops an agent and starts it again with a fresh session.
func (s *server) restartAgent(w http.ResponseWriter, r *http.Request) {
	if s.Agents == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("agents are not managed by this daemon"))

		return
	}

	// Detached from the request: the agent outlives it.
	err := s.Agents.Restart(context.WithoutCancel(r.Context()), r.PathValue("address"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, runtime.ErrUnknownAgent):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, runtime.ErrOnLeave), errors.Is(err, runtime.ErrStopping):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.Logger.Error("restart agent", "err", err)
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

	messages, err := s.queryMessages(r, limit)
	if errors.Is(err, errBadFilter) {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

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

// errBadFilter is returned for an invalid combination of message filters.
var errBadFilter = errors.New("invalid filter")

// queryMessages applies the filters of GET /api/messages: with=<address>
// (the human's conversation with that address, the chat of the control room)
// or scope=agents (delivered messages between agents, its wastebasket).
func (s *server) queryMessages(r *http.Request, limit int) ([]messaging.Message, error) {
	q := r.URL.Query()
	with, scope := q.Get("with"), q.Get("scope")

	switch {
	case with != "" && scope != "":
		return nil, fmt.Errorf("%w: use either with or scope", errBadFilter)
	case with != "":
		return s.Store.ConversationMessages(r.Context(), org.HumanAddress, with, limit)
	case scope == "agents":
		return s.Store.DeliveredMessagesAmong(r.Context(), []string{org.HumanAddress, org.SystemAddress}, limit)
	case scope != "":
		return nil, fmt.Errorf("%w: scope must be agents", errBadFilter)
	default:
		return s.Store.RecentMessages(r.Context(), limit)
	}
}

// deletedMessages is the body answered by DELETE /api/messages.
type deletedMessages struct {
	IDs []string `json:"ids"`
}

// deleteFinishedAgentConversations empties the control room's wastebasket:
// the finished conversations between agents are deleted for good; open ones
// stay, unless include_open=true closes them too (messaging.Closer).
// Conversations involving the human or nobody are never touched.
func (s *server) deleteFinishedAgentConversations(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("scope") != "agents" {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("%w: scope must be agents", errBadFilter))

		return
	}

	ids, err := s.Store.DeleteFinishedConversations(r.Context(),
		[]string{org.HumanAddress, org.SystemAddress}, messaging.ActsRequiringReply())
	if err != nil {
		s.Logger.Error("empty the wastebasket", "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot delete the finished conversations"))

		return
	}

	if ids == nil {
		ids = []string{}
	}

	if len(ids) > 0 {
		s.Events.Publish(events.Event{Type: events.MessagesDeleted, Data: events.DeletedData{IDs: ids}})
	}

	// The open ones too, on request: their senders are told (the Closer
	// announces what it deletes).
	if r.URL.Query().Get("include_open") == "true" && s.Closer != nil {
		closed, err := s.Closer.Close(r.Context(), messaging.CloseScope{Reason: "the human closed it from the wastebasket"})
		if err != nil {
			s.Logger.Error("close the open conversations", "err", err)
			s.writeError(w, http.StatusInternalServerError, errors.New("cannot close the open conversations"))

			return
		}

		ids = append(ids, closed...)
	}

	s.writeJSON(w, http.StatusOK, deletedMessages{IDs: ids})
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
	if errors.Is(err, messaging.ErrBlocked) || errors.Is(err, messaging.ErrTooManyHops) || errors.Is(err, messaging.ErrNothingToReply) {
		return http.StatusForbidden
	}

	return http.StatusBadRequest
}
