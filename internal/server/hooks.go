package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/hook"
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

	if ev.Name == hook.EventPermissionRequest {
		s.askHuman(w, r, address, ev)

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
	if _, ok := s.Org.Member(address); !ok {
		s.writeError(w, http.StatusNotFound, fmt.Errorf("unknown agent %q", address))

		return "", false
	}

	return address, true
}
