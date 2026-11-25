package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// defaultApprovalWait is how long a permission request waits for the human.
// It is shorter than runtime.PermissionHookTimeout so the daemon always
// answers before Claude Code cuts the hook off.
const defaultApprovalWait = runtime.PermissionHookTimeout - time.Minute

// askHuman files an agent's permission request and waits for the human. With
// a decision, Claude Code gets an allow or deny; without one, it gets nothing
// and shows its own prompt in the agent's terminal.
func (s *server) askHuman(w http.ResponseWriter, r *http.Request, address string, ev hook.Event) {
	s.Statuses.ObserveHook(address, ev, 0)

	ctx, cancel := context.WithTimeout(r.Context(), s.approvalWait())
	defer cancel()

	decision, err := s.Approvals.Ask(ctx, approval.Request{
		ID:          messaging.RandomID(),
		Agent:       address,
		ToolName:    ev.ToolName,
		ToolInput:   ev.ToolInput,
		Suggestions: ev.PermissionSuggestions,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		s.Logger.Info("permission request left to the terminal", "agent", address, "tool", ev.ToolName)
		s.writeJSON(w, http.StatusOK, hookDecision{MessageIDs: []string{}})

		return
	}

	// Queue.Decide checked the index against these same suggestions.
	var updates []json.RawMessage
	if decision.Suggestion != nil {
		updates = []json.RawMessage{ev.PermissionSuggestions[*decision.Suggestion]}
	}

	s.Statuses.Set(address, runtime.StatusWorking)
	s.Logger.Info("permission decided", "agent", address, "tool", ev.ToolName, "allow", decision.Allow,
		"suggestion", decision.Suggestion != nil)
	s.writeJSON(w, http.StatusOK, hookDecision{
		Response:   hook.PermissionResponse(decision.Allow, decision.Message, updates),
		MessageIDs: []string{},
	})
}

func (s *server) approvalWait() time.Duration {
	if s.ApprovalWait > 0 {
		return s.ApprovalWait
	}

	return defaultApprovalWait
}

func (s *server) listApprovals(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Approvals.Pending())
}

// approvalDecision is the body of POST /api/approvals/{id}.
type approvalDecision struct {
	Decision string `json:"decision"` // "allow" or "deny"
	Message  string `json:"message"`
	// Suggestion applies one of the request's suggestions (by index) with an
	// allow, e.g. "always allow this command" or "switch to auto mode".
	Suggestion *int `json:"suggestion,omitempty"`
}

func (s *server) decideApproval(w http.ResponseWriter, r *http.Request) {
	var d approvalDecision
	if err := decodeJSON(w, r, &d, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if d.Decision != "allow" && d.Decision != "deny" {
		s.writeError(w, http.StatusBadRequest, errors.New(`decision must be "allow" or "deny"`))

		return
	}

	err := s.Approvals.Decide(r.PathValue("id"), approval.Decision{Allow: d.Decision == "allow", Message: d.Message, Suggestion: d.Suggestion})
	if errors.Is(err, approval.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, err)

		return
	}

	if errors.Is(err, approval.ErrBadSuggestion) {
		s.writeError(w, http.StatusBadRequest, errors.New("suggestion must be the index of one of the request's suggestions, with an allow"))

		return
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
