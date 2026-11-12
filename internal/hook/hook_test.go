package hook

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

type fakeInbox map[messaging.Priority][]messaging.Message

func (f fakeInbox) Pending(_ context.Context, p messaging.Priority) ([]messaging.Message, error) {
	return f[p], nil
}

type brokenInbox struct{}

func (brokenInbox) Pending(context.Context, messaging.Priority) ([]messaging.Message, error) {
	return nil, errors.New("disk on fire")
}

var (
	normalMsg = messaging.Message{ID: "n1", From: "dev1@dev", Act: messaging.ActQuery, Subject: "question", Body: "Why Postgres?", Priority: messaging.Normal}
	urgentMsg = messaging.Message{ID: "u1", From: "secu@specs", Act: messaging.ActInform, Subject: "stop", Body: "Do not log tokens.", Priority: messaging.Urgent}
)

func TestHandle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		event        Event
		inbox        fakeInbox
		wantIDs      []string
		wantDecision string
		wantContext  bool
	}{
		{
			name:  "stop with empty mailbox lets the agent stop",
			event: Event{Name: EventStop},
			inbox: fakeInbox{},
		},
		{
			name:         "stop delivers urgent then normal mail and blocks",
			event:        Event{Name: EventStop},
			inbox:        fakeInbox{messaging.Normal: {normalMsg}, messaging.Urgent: {urgentMsg}},
			wantIDs:      []string{"u1", "n1"},
			wantDecision: "block",
		},
		{
			name:         "stop still delivers when already continuing from a stop hook",
			event:        Event{Name: EventStop, StopHookActive: true},
			inbox:        fakeInbox{messaging.Normal: {normalMsg}},
			wantIDs:      []string{"n1"},
			wantDecision: "block",
		},
		{
			name:        "post tool use injects urgent mail only",
			event:       Event{Name: EventPostToolUse, ToolName: "Read"},
			inbox:       fakeInbox{messaging.Normal: {normalMsg}, messaging.Urgent: {urgentMsg}},
			wantIDs:     []string{"u1"},
			wantContext: true,
		},
		{
			name:  "post tool use without urgent mail says nothing",
			event: Event{Name: EventPostToolUse, ToolName: "Read"},
			inbox: fakeInbox{messaging.Normal: {normalMsg}},
		},
		{
			name:        "prompt submit attaches all mail, urgent first",
			event:       Event{Name: EventUserPromptSubmit},
			inbox:       fakeInbox{messaging.Normal: {normalMsg}, messaging.Urgent: {urgentMsg}},
			wantIDs:     []string{"u1", "n1"},
			wantContext: true,
		},
		{
			name:  "prompt submit without mail says nothing",
			event: Event{Name: EventUserPromptSubmit},
			inbox: fakeInbox{},
		},
		{
			name:  "other events are ignored",
			event: Event{Name: "SessionStart"},
			inbox: fakeInbox{messaging.Urgent: {urgentMsg}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Handle(context.Background(), tt.event, tt.inbox)
			if err != nil {
				t.Fatal(err)
			}

			if len(tt.wantIDs) == 0 {
				if got.Response != nil || len(got.Messages) != 0 {
					t.Fatalf("expected no delivery, got %+v", got)
				}

				return
			}

			if ids := messageIDs(got.Messages); strings.Join(ids, ",") != strings.Join(tt.wantIDs, ",") {
				t.Fatalf("delivered %v, want %v", ids, tt.wantIDs)
			}

			if got.Response.Decision != tt.wantDecision {
				t.Fatalf("decision = %q, want %q", got.Response.Decision, tt.wantDecision)
			}

			if (got.Response.HookSpecificOutput != nil) != tt.wantContext {
				t.Fatalf("additional context present = %v, want %v", got.Response.HookSpecificOutput != nil, tt.wantContext)
			}

			text := got.Response.Reason
			if tt.wantContext {
				text = got.Response.HookSpecificOutput.AdditionalContext
			}

			for _, m := range got.Messages {
				for _, want := range []string{m.Body, "ID: " + m.ID, "From: " + m.From} {
					if !strings.Contains(text, want) {
						t.Fatalf("rendered text misses %q:\n%s", want, text)
					}
				}
			}
		})
	}
}

func TestRenderShowsWhatAMessageAnswers(t *testing.T) {
	t.Parallel()

	answer := messaging.Message{ID: "a1", From: "archi@specs", Act: messaging.ActInform, InReplyTo: "q7", Subject: "Re", Body: "PostgreSQL"}
	if text := renderMail(stopIntro, []messaging.Message{answer}); !strings.Contains(text, "In reply to: q7") {
		t.Fatalf("the agent must see which message is answered:\n%s", text)
	}

	if text := renderMail(stopIntro, []messaging.Message{normalMsg}); strings.Contains(text, "In reply to") {
		t.Fatalf("a new conversation has nothing to refer to:\n%s", text)
	}
}

func TestRenderAsksToActOnSystemNotices(t *testing.T) {
	t.Parallel()

	notice := messaging.Message{ID: "s1", From: org.SystemAddress, Act: messaging.ActInform, Subject: "New ticket", Body: "Claim T-1"}
	if text := renderMail(promptIntro, []messaging.Message{notice}); !strings.Contains(text, "do what it asks") {
		t.Fatalf("a system notice must ask the agent to act:\n%s", text)
	}
}

func TestRenderFlagsExpectedAnswers(t *testing.T) {
	t.Parallel()

	text := renderMail(stopIntro, []messaging.Message{normalMsg, urgentMsg})
	if !strings.Contains(text, "Act: query (an answer is expected)") {
		t.Fatalf("query should ask for an answer:\n%s", text)
	}

	if !strings.Contains(text, "Act: inform (no answer needed: do not reply)") {
		t.Fatalf("inform should say no answer is needed:\n%s", text)
	}

	if !strings.Contains(text, "in_reply_to") {
		t.Fatalf("the agent should be told how to reply:\n%s", text)
	}
}

func TestHandleReportsInboxErrors(t *testing.T) {
	t.Parallel()

	for _, name := range []string{EventUserPromptSubmit, EventPostToolUse, EventStop} {
		if _, err := Handle(context.Background(), Event{Name: name}, brokenInbox{}); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func messageIDs(messages []messaging.Message) []string {
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}

	return ids
}
