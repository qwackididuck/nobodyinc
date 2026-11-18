package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// hookDecision is what nobody-hook receives: the response to print for Claude
// Code (absent when there is nothing to say) and the IDs to acknowledge once
// it has been printed.
type hookDecision struct {
	Response   *hook.Response `json:"response,omitempty"`
	MessageIDs []string       `json:"message_ids"`
}

type hookAck struct {
	MessageIDs []string `json:"message_ids"`
}

// handleHookEvent decides what to tell an agent on a Claude Code hook event.
func (s *server) handleHookEvent(w http.ResponseWriter, r *http.Request) {
	address, ok := s.agentAddress(w, r)
	if !ok {
		return
	}

	var ev hook.Event
	if err := decodeJSON(w, r, &ev, false); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	switch ev.Name {
	case hook.EventPermissionRequest:
		s.askHuman(w, r, address, ev)

		return
	case hook.EventSessionStart:
		s.startSession(w, address, ev)

		return
	}

	if ev.Name == hook.EventStop && s.holdAtStop(w, r, address, ev) {
		return
	}

	delivery, err := hook.Handle(r.Context(), ev, s.Store.Tray(address))
	if err != nil {
		s.Logger.Error("handle hook event", "agent", address, "event", ev.Name, "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot read the agent's mailbox"))

		return
	}

	s.Statuses.ObserveHook(address, ev, len(delivery.Messages))

	decision := hookDecision{Response: delivery.Response, MessageIDs: []string{}}
	for _, m := range delivery.Messages {
		decision.MessageIDs = append(decision.MessageIDs, m.ID)
	}

	s.Logger.Info("hook event", "agent", address, "event", ev.Name, "delivered", len(decision.MessageIDs))
	s.writeJSON(w, http.StatusOK, decision)
}

// holdAtStop handles the Stop of an agent a budget holds (ADR-0013): it is
// asked once to justify an extension, or ends its turn on budget hold, its
// mail left waiting. It reports whether it answered.
func (s *server) holdAtStop(w http.ResponseWriter, r *http.Request, address string, ev hook.Event) bool {
	if s.BudgetStop == nil {
		return false
	}

	response, held, err := s.BudgetStop(r.Context(), address)
	if err != nil {
		// Never block an agent on a failure of ours: it stops as usual.
		s.Logger.Error("check the budgets at the end of a turn", "agent", address, "err", err)

		return false
	}

	if !held {
		return false
	}

	if response != nil {
		s.Statuses.ObserveHook(address, ev, 1) // still working: it must answer first
	} else {
		s.Statuses.ObserveHeldStop(address, ev)
	}

	s.Logger.Info("hook event", "agent", address, "event", ev.Name, "budget_hold", true, "asked", response != nil)
	s.writeJSON(w, http.StatusOK, hookDecision{Response: response, MessageIDs: []string{}})

	return true
}

// acknowledgeHookDelivery marks messages delivered once nobody-hook has handed
// them to the agent.
func (s *server) acknowledgeHookDelivery(w http.ResponseWriter, r *http.Request) {
	address, ok := s.agentAddress(w, r)
	if !ok {
		return
	}

	var ack hookAck
	if err := decodeJSON(w, r, &ack, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.Store.MarkDelivered(r.Context(), address, ack.MessageIDs, time.Now().UTC()); err != nil {
		s.Logger.Error("acknowledge delivery", "agent", address, "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot record the delivery"))

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// agentAddress reads the {address} path value and checks the agent exists.
func (s *server) agentAddress(w http.ResponseWriter, r *http.Request) (string, bool) {
	address := r.PathValue("address")
	if _, ok := s.Org.Current().Member(address); !ok {
		s.writeError(w, http.StatusNotFound, fmt.Errorf("unknown agent %q", address))

		return "", false
	}

	return address, true
}

// startSession answers a SessionStart event. It runs at every session start,
// resumed sessions included:
//
//   - it hands over the skills the agent preloads (its file's skills:,
//     ADR-0011), read where it works: that is how a change of preloaded
//     skills reaches an agent without losing its memory;
//   - for a session resumed because the project opened again, it adds the
//     current organization, and a silent resume makes the agent idle, ready
//     for its mail, or on budget hold (ADR-0015).
func (s *server) startSession(w http.ResponseWriter, address string, ev hook.Event) {
	o := s.Org.Current()

	m, ok := o.Member(address)
	if !ok {
		s.writeError(w, http.StatusNotFound, fmt.Errorf("no agent %s", address))

		return
	}

	workDir := ev.Cwd
	if workDir == "" {
		workDir = o.ProjectDir
	}

	preloaded, missing := skills.Preload(m.Agent.Skills, workDir, o.Skills)
	if len(missing) > 0 {
		s.Logger.Warn("preloaded skills not found", "agent", address, "skills", missing, "work_dir", workDir)
	}

	var (
		reopen  string
		settled runtime.Status
	)

	if s.SessionStarted != nil {
		reopen, settled = s.SessionStarted(address)
	}

	s.Logger.Info("hook event", "agent", address, "event", ev.Name, "source", ev.Source,
		"preloaded", len(preloaded), "reopened", reopen != "", "settled", settled)

	// Answered before the agent becomes idle: becoming idle may wake it up
	// for its mail, which should find the session ready.
	s.writeJSON(w, http.StatusOK, hookDecision{Response: hook.SessionStartResponse(preloaded, reopen), MessageIDs: []string{}})
	s.Statuses.ObserveSessionStart(address, ev, settled)
}
