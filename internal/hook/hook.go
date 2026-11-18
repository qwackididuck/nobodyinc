// Package hook decides what to tell a Claude Code agent about its mail.
//
// Claude Code runs the nobody-hook binary at three moments of an agent's life.
// nobody-hook relays the event to the daemon, which calls Handle:
//
//   - UserPromptSubmit: a prompt starts a turn, typically the wake-up prompt
//     the daemon types into an idle agent. All waiting mail is attached to
//     the prompt as extra context, so the agent reads it in that same turn.
//   - PostToolUse: the agent has just used a tool, in the middle of its turn.
//     Only urgent mail is handed over here, as extra context, so the agent can
//     take it into account without being stopped.
//   - Stop: the agent has finished its turn. If mail arrived during the turn,
//     we refuse the stop and pass the mail as the reason. The agent then acts
//     on it before ending its turn for real. (Claude Code labels such a block
//     "Stop hook error" in its UI; nothing is wrong, see docs/hooks.md.)
//
// Any other event is ignored here. Two events are not about mail and are
// handled by the daemon itself: PermissionRequest (the approval queue,
// docs/approvals.md) and SessionStart, which hands the agent the skills it
// preloads (PreloadResponse, ADR-0011).
//
// Delivery is "at least once": Handle only decides what to say. The messages
// are marked delivered once nobody-hook confirms it printed the answer, so a
// failure in between re-delivers a message rather than losing it.
//
// See docs/hooks.md for the full picture.
package hook

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
)

// Names of the Claude Code hook events we act on.
const (
	EventUserPromptSubmit = "UserPromptSubmit"
	EventPostToolUse      = "PostToolUse"
	EventStop             = "Stop"
	// EventPermissionRequest is not about mail: the daemon handles it with
	// the approval queue (internal/approval), see PermissionResponse.
	EventPermissionRequest = "PermissionRequest"
	// EventSessionStart is not about mail either: the daemon hands the agent
	// the skills it preloads, see PreloadResponse.
	EventSessionStart = "SessionStart"
)

// Event is the part of the JSON sent by Claude Code on stdin that we use.
type Event struct {
	Name      string `json:"hook_event_name"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	// ToolName and ToolInput are set for PostToolUse and PermissionRequest.
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	// StopHookActive is true when the agent is already continuing because a
	// Stop hook blocked it. We still deliver new mail in that case: messages
	// leave the tray once delivered, so this cannot loop forever.
	StopHookActive bool `json:"stop_hook_active,omitempty"`
	// Source is set for SessionStart: startup, resume, clear or compact.
	Source string `json:"source,omitempty"`
}

// Response is what nobody-hook prints on stdout for Claude Code.
type Response struct {
	// Decision "block" on a Stop event keeps the agent working; Reason is
	// then shown to the agent as the reason it must continue.
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// HookSpecificOutput carries the extra context injected on
	// UserPromptSubmit, PostToolUse and SessionStart.
	HookSpecificOutput *SpecificOutput `json:"hookSpecificOutput,omitempty"` //nolint:tagliatelle // field name imposed by Claude Code
}

// SpecificOutput is the event-specific part of a Response.
type SpecificOutput struct {
	HookEventName     string              `json:"hookEventName"`               //nolint:tagliatelle // field name imposed by Claude Code
	AdditionalContext string              `json:"additionalContext,omitempty"` //nolint:tagliatelle // field name imposed by Claude Code
	Decision          *PermissionDecision `json:"decision,omitempty"`
}

// PermissionDecision answers a PermissionRequest.
type PermissionDecision struct {
	Behavior string `json:"behavior"` // "allow" or "deny"
	Message  string `json:"message,omitempty"`
}

// PermissionResponse builds the answer to a PermissionRequest event.
func PermissionResponse(allow bool, message string) *Response {
	behavior := "deny"
	if allow {
		behavior = "allow"
	}

	return &Response{HookSpecificOutput: &SpecificOutput{
		HookEventName: EventPermissionRequest,
		Decision:      &PermissionDecision{Behavior: behavior, Message: message},
	}}
}

// Delivery is the outcome of handling an event: the response to print and the
// messages it carries, to mark delivered once the response has been printed.
// A nil Response means there is nothing to say and the agent carries on.
type Delivery struct {
	Response *Response
	Messages []messaging.Message
}

// Inbox gives access to one agent's trays.
type Inbox interface {
	Pending(ctx context.Context, p messaging.Priority) ([]messaging.Message, error)
}

// Handle decides what to tell the agent for the given event.
func Handle(ctx context.Context, ev Event, inbox Inbox) (Delivery, error) {
	switch ev.Name {
	case EventUserPromptSubmit:
		return onUserPromptSubmit(ctx, inbox)
	case EventPostToolUse:
		return onPostToolUse(ctx, inbox)
	case EventStop:
		return onStop(ctx, inbox)
	default:
		return Delivery{}, nil
	}
}

// onUserPromptSubmit attaches all waiting mail, urgent first, to the prompt
// that starts the turn.
func onUserPromptSubmit(ctx context.Context, inbox Inbox) (Delivery, error) {
	messages, err := allPending(ctx, inbox)
	if err != nil || len(messages) == 0 {
		return Delivery{}, err
	}

	return contextDelivery(EventUserPromptSubmit, promptIntro, messages), nil
}

// onStop hands over all waiting mail, urgent first, and keeps the agent
// working so it can act on it.
func onStop(ctx context.Context, inbox Inbox) (Delivery, error) {
	messages, err := allPending(ctx, inbox)
	if err != nil || len(messages) == 0 {
		return Delivery{}, err
	}

	return Delivery{
		Response: &Response{
			Decision: "block",
			Reason:   renderMail(stopIntro, messages),
		},
		Messages: messages,
	}, nil
}

// onPostToolUse hands over urgent mail only, without interrupting the agent.
func onPostToolUse(ctx context.Context, inbox Inbox) (Delivery, error) {
	urgent, err := inbox.Pending(ctx, messaging.Urgent)
	if err != nil {
		return Delivery{}, fmt.Errorf("read urgent tray: %w", err)
	}

	if len(urgent) == 0 {
		return Delivery{}, nil
	}

	return contextDelivery(EventPostToolUse, urgentIntro, urgent), nil
}

// allPending returns every undelivered message, urgent first.
func allPending(ctx context.Context, inbox Inbox) ([]messaging.Message, error) {
	urgent, err := inbox.Pending(ctx, messaging.Urgent)
	if err != nil {
		return nil, fmt.Errorf("read urgent tray: %w", err)
	}

	normal, err := inbox.Pending(ctx, messaging.Normal)
	if err != nil {
		return nil, fmt.Errorf("read normal tray: %w", err)
	}

	return append(urgent, normal...), nil
}

// contextDelivery hands messages over as extra context for the model.
func contextDelivery(event, intro string, messages []messaging.Message) Delivery {
	return Delivery{
		Response: &Response{
			HookSpecificOutput: &SpecificOutput{
				HookEventName:     event,
				AdditionalContext: renderMail(intro, messages),
			},
		},
		Messages: messages,
	}
}
