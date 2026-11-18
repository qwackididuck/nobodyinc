package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// ticketNotifier turns ticket events into notices from "nobody", so the
// right agent hears about the work (and is woken up if idle):
//
//   - a new ticket in a team's queue: one available member of that team;
//   - a ticket created with an assignee: that assignee;
//   - a released ticket: another available member of the team;
//   - a finished ticket: its creator, plus the teams whose tickets it was
//     blocking.
//
// Only one member is told about a queued ticket, so a whole team is not
// woken up for a single piece of work.
type ticketNotifier struct {
	org      org.Source
	statuses *runtime.Statuses
	messages *messaging.Service
	tickets  *work.Service
	logger   *slog.Logger
}

func (n *ticketNotifier) onEvent(e work.Event) {
	ctx := context.Background()

	t := e.Ticket
	switch e.Kind {
	case work.EventCreated:
		if t.Assignee != "" {
			n.notify(ctx, t.Assignee, t, "Ticket assigned to you: "+t.Title, assignedText(t))
		} else if len(t.BlockedBy) == 0 {
			n.notifyTeam(ctx, t, "", "New ticket in your team's queue: "+t.Title, queuedText(t))
		}
	case work.EventReleased:
		n.notifyTeam(ctx, t, e.By, "Ticket back in your team's queue: "+t.Title, queuedText(t))
	case work.EventDone:
		if t.CreatedBy != e.By {
			body := fmt.Sprintf("%s finished %s (%s). Use ticket_get to see its deliverables.", e.By, t.ID, t.Title)
			n.notify(ctx, t.CreatedBy, t, "Ticket done: "+t.Title, body)
		}

		n.notifyUnblocked(ctx, t)
	case work.EventClaimed, work.EventCanceled:
		// The claimer knows; a canceled ticket is nobody's work any more.
	}
}

func (n *ticketNotifier) notifyUnblocked(ctx context.Context, done work.Ticket) {
	unblocked, err := n.tickets.Unblocked(ctx, done)
	if err != nil {
		n.logger.Warn("list unblocked tickets", "ticket", done.ID, "err", err)

		return
	}

	for _, t := range unblocked {
		n.notifyTeam(ctx, t, "", "Ticket ready in your team's queue: "+t.Title,
			fmt.Sprintf("%s is done, so %s can start.\n\n%s", done.ID, t.ID, queuedText(t)))
	}
}

// notifyTeam tells one member of the ticket's team, preferring idle ones.
func (n *ticketNotifier) notifyTeam(ctx context.Context, t work.Ticket, exclude, subject, body string) {
	if member := pickMember(n.org.Current(), n.statuses, t.Team, exclude); member != "" {
		n.notify(ctx, member, t, subject, body)
	}
}

func (n *ticketNotifier) notify(ctx context.Context, to string, t work.Ticket, subject, body string) {
	if to == org.SystemAddress {
		return
	}

	_, err := n.messages.Send(ctx, messaging.Draft{
		From:     org.SystemAddress,
		To:       to,
		Act:      messaging.ActInform,
		Priority: messaging.Normal,
		Subject:  subject,
		Body:     body,
		Ticket:   t.ID,
	})
	if err != nil {
		n.logger.Warn("ticket notice", "to", to, "ticket", t.ID, "err", err)
	}
}

func queuedText(t work.Ticket) string {
	return fmt.Sprintf("%s is waiting in your team's queue.\n%s\nIf you can take it, claim it with ticket_claim (id %s) and work on it; "+
		"otherwise leave it for a teammate. Use ticket_get for the full description.", t.ID, summary(t), t.ID)
}

func assignedText(t work.Ticket) string {
	return fmt.Sprintf("%s is assigned to you.\n%s\nWork on it, report with deliverable_add and ticket_comment, "+
		"then close it with ticket_update (status done).", t.ID, summary(t))
}

func summary(t work.Ticket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", t.Title)

	if t.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", t.Description)
	}

	if t.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "Acceptance criteria: %s\n", t.AcceptanceCriteria)
	}

	return b.String()
}
