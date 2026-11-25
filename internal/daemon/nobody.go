package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/automation"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// nobodyOffice is the Nobody level of the automatic mode (docs/automation.md):
// a question an agent asks the human goes to the director, who decides in
// their place and reports it with decision_log.
//
// Only a query is taken over. Proposals (features, schedules, budgets) stay
// for the human: they commit money or change the project's configuration.
type nobodyOffice struct {
	mode     *automation.Mode
	org      org.Source
	store    *store.Store
	messages *messaging.Service
	bus      *events.Bus
	logger   *slog.Logger
	now      func() time.Time
}

var _ officemcp.Decisions = (*nobodyOffice)(nil)

// newNobodyOffice builds the Nobody level on top of the daemon's services,
// and makes every sent message go through it after next (the daemon's own
// handling of a sent message).
func newNobodyOffice(
	ctx context.Context, next func(messaging.Message),
	mode *automation.Mode, o org.Source, st *store.Store, messages *messaging.Service, bus *events.Bus, logger *slog.Logger,
) *nobodyOffice {
	n := &nobodyOffice{
		mode: mode, org: o, store: st, messages: messages, bus: bus, logger: logger,
		now: func() time.Time { return time.Now().UTC() },
	}
	messages.OnSent = func(m messaging.Message) {
		next(m)
		n.route(context.WithoutCancel(ctx), m)
	}

	return n
}

// Log records the director's decision, sends it to the agent that asked, and
// closes the question that was waiting for the human.
func (n *nobodyOffice) Log(ctx context.Context, by string, in officemcp.DecisionLogInput) (officemcp.DecisionLogOutput, error) {
	impact := automation.Impact(in.Impact)
	if !impact.Valid() {
		return officemcp.DecisionLogOutput{}, errors.New("impact must be low, medium or high")
	}

	if strings.TrimSpace(in.Decision) == "" || strings.TrimSpace(in.Rationale) == "" {
		return officemcp.DecisionLogOutput{}, errors.New("a decision needs the decision itself and its rationale")
	}

	q, err := n.store.GetMessage(ctx, in.MessageID)
	if err != nil {
		return officemcp.DecisionLogOutput{}, fmt.Errorf("question %q: %w", in.MessageID, err)
	}

	if q.To != org.HumanAddress || q.Act != messaging.ActQuery {
		return officemcp.DecisionLogOutput{}, fmt.Errorf("message %s is not a question to the human", q.ID)
	}

	d, err := n.store.InsertDecision(ctx, automation.Decision{
		AskedBy: q.From, Message: q.ID, Question: q.Subject, Decision: in.Decision, Rationale: in.Rationale,
		Impact: impact, DecidedAt: n.now(),
	})
	if err != nil {
		return officemcp.DecisionLogOutput{}, err
	}

	n.bus.Publish(events.Event{Type: events.DecisionsChanged, Data: events.Empty{}})
	n.answer(ctx, by, q, d)

	return officemcp.DecisionLogOutput{ID: d.ID, Note: "recorded for the human's report and sent to " + q.From}, nil
}

// route is called for every message sent. A query to the human, in Nobody
// mode, becomes a notice asking the director to decide.
func (n *nobodyOffice) route(ctx context.Context, m messaging.Message) {
	if !n.mode.DirectorDecides() || m.To != org.HumanAddress || m.Act != messaging.ActQuery || m.From == org.SystemAddress {
		return
	}

	god := n.org.Current().God
	if god == nil {
		return
	}

	_, err := n.messages.Send(ctx, messaging.Draft{
		From: org.SystemAddress, To: god.Name, Act: messaging.ActInform, Priority: m.Priority,
		Subject: "Decide in the human's place: " + m.Subject, Body: noticeBody(m, m.From == god.Name),
	})
	if err != nil {
		n.logger.Error("ask the director to decide", "message", m.ID, "err", err)
	}
}

func noticeBody(q messaging.Message, askedByDirector bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Nobody mode is on: the human is away and has handed the decisions to you.\n\n")
	fmt.Fprintf(&b, "%s asked the human (message %s):\n\n%s\n\n%s\n\n", q.From, q.ID, q.Subject, q.Body)
	b.WriteString("Decide yourself: do not ask the human. Prefer what they would most likely choose, and the most " +
		"reversible option. If the question is about how the project works, ask the agent whose expertise covers it first.\n\n")
	fmt.Fprintf(&b, "Then call decision_log with message_id %q, your decision, your rationale and its impact ", q.ID)

	if askedByDirector {
		b.WriteString("(it is your own question: nothing is sent to anyone), and carry on.")
	} else {
		b.WriteString("(your decision is sent to " + q.From + " for you: do not also write to them).")
	}

	return b.String()
}

// answer hands the decision to the asker (unless it is the director, who
// already knows) and closes the human's card, so that it does not wait for an
// answer nobody will give.
func (n *nobodyOffice) answer(ctx context.Context, by string, q messaging.Message, d automation.Decision) {
	if q.From != by {
		n.send(ctx, messaging.Draft{
			From: by, To: q.From, Act: messaging.ActInform, Priority: q.Priority, InReplyTo: q.ID,
			Subject: "Answer: " + q.Subject,
			Body: d.Decision + "\n\nWhy: " + d.Rationale +
				"\n\n(The human is away: the director decided in their place, and the human will review it.)",
		})
	}

	n.send(ctx, messaging.Draft{
		From: org.SystemAddress, To: org.HumanAddress, Act: messaging.ActInform, Priority: messaging.Normal, InReplyTo: q.ID,
		Subject: fmt.Sprintf("Answered by the director in your place (impact %s): %s", d.Impact, q.Subject),
		Body:    d.Decision + "\n\nWhy: " + d.Rationale,
	})
}

func (n *nobodyOffice) send(ctx context.Context, d messaging.Draft) {
	if _, err := n.messages.Send(ctx, d); err != nil {
		n.logger.Warn("send a decision", "to", d.To, "err", err)
	}
}
