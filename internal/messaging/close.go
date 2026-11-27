package messaging

// Closing open conversations by force: a request, query or proposal between
// agents that nobody answers stays open from one feature, one session, to
// the next. The human (wastebasket), the director, the end of a feature, the
// closing of a ticket and old age close them: the conversation is deleted
// for good, and the agent that was waiting for an answer is told so by
// `nobody`, so that it stops waiting. Conversations involving the human or
// `nobody` are never touched: they wait for a person (cards, proposals).

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// OpenStore finds and deletes open conversations. It is implemented by
// internal/store.
type OpenStore interface {
	OpenAgentConversations(ctx context.Context, excluded []string, openActs []Act, tickets []string) ([]Message, error)
	DeleteConversations(ctx context.Context, conversations []string) ([]string, error)
}

// Closer closes open conversations between agents.
type Closer struct {
	Store    OpenStore
	Messages *Service
	Logger   *slog.Logger
	// OnDeleted, when set, is called with the IDs of the deleted messages
	// (the control room forgets them).
	OnDeleted func(ids []string)
}

// CloseScope tells which open conversations to close.
type CloseScope struct {
	// Tickets, when not nil, keeps the conversations with a message about
	// one of these tickets.
	Tickets []string
	// Before, when not zero, keeps the conversations whose last message was
	// sent before it.
	Before time.Time
	// Reason is told to the agents: "closed by the human", "its ticket T-3
	// is done"…
	Reason string
}

// keep returns, among the last messages of open conversations, those of the
// conversations the scope covers by age.
func (s CloseScope) keep(last []Message) []Message {
	var kept []Message

	for _, m := range last {
		if s.Before.IsZero() || m.SentAt.Before(s.Before) {
			kept = append(kept, m)
		}
	}

	return kept
}

// Close closes the open conversations of the scope and returns the IDs of the
// deleted messages (none when nothing was open).
func (c *Closer) Close(ctx context.Context, scope CloseScope) ([]string, error) {
	if scope.Tickets != nil && len(scope.Tickets) == 0 {
		return nil, nil
	}

	excluded := []string{org.HumanAddress, org.SystemAddress}

	last, err := c.Store.OpenAgentConversations(ctx, excluded, ActsRequiringReply(), scope.Tickets)
	if err != nil {
		return nil, fmt.Errorf("find open conversations: %w", err)
	}

	waiting := scope.keep(last)

	conversations := make([]string, len(waiting))
	for i, m := range waiting {
		conversations[i] = m.Conversation
	}

	ids, err := c.Store.DeleteConversations(ctx, conversations)
	if err != nil {
		return nil, err
	}

	if len(ids) > 0 && c.OnDeleted != nil {
		c.OnDeleted(ids)
	}

	for _, m := range waiting {
		c.tellSender(ctx, m, scope.Reason)
	}

	return ids, nil
}

// tellSender tells the agent whose message was waiting for an answer that
// the conversation is closed.
func (c *Closer) tellSender(ctx context.Context, m Message, reason string) {
	subject := m.Subject
	if subject == "" {
		subject = m.Body
	}

	_, err := c.Messages.Send(ctx, Draft{
		From: org.SystemAddress, To: m.From, Act: ActInform, Priority: Normal,
		Subject: "Conversation closed: " + subject,
		Body: fmt.Sprintf("Your %s to %s (%q) was closed without an answer: %s. "+
			"Do not wait for an answer; ask again if you still need it.", m.Act, m.To, subject, reason),
	})
	if err != nil && c.Logger != nil {
		c.Logger.Warn("tell an agent its conversation was closed", "agent", m.From, "conversation", m.Conversation, "err", err)
	}
}
