package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// staff deals with what happens around an agent that is on leave or has been
// dismissed (docs/agents.md, "Lifecycle"):
//
//   - once it has stopped, its ticket in progress goes back to its team's
//     queue, so that a teammate can take it over;
//   - a dismissed agent's undelivered mail is returned: each sender is told
//     the message was not delivered;
//   - mail sent to an agent on leave waits in its mailbox until it returns,
//     and the sender is told so.
//
// Team addressing and queue notifications skip agents on leave (pickMember).
type staff struct {
	org      org.Source
	store    *store.Store
	messages *messaging.Service
	tickets  *work.Service
	logger   *slog.Logger
}

// onRetired is called by the supervisor once an agent on leave, or no longer
// in the organization, has stopped.
func (s *staff) onRetired(address string) {
	ctx := context.Background()

	_, inOrg := s.org.Current().Member(address)

	reason := "is on leave"
	if !inOrg {
		reason = "has left Nobody Inc"
	}

	s.releaseTickets(ctx, address, reason)

	if !inOrg {
		s.returnMail(ctx, address)
	}
}

// onSent tells the sender of a message to an agent on leave that it will wait.
func (s *staff) onSent(ctx context.Context, m messaging.Message) {
	if m.From == org.SystemAddress {
		return // never answer a notice with a notice
	}

	member, ok := s.org.Current().Member(m.To)
	if !ok || !member.Agent.OnLeave {
		return
	}

	body := fmt.Sprintf("%s is on leave: your message %q waits in their mailbox until they return. "+
		"If it cannot wait, write to their team (@%s) or to the human instead.", m.To, m.Subject, org.TeamOf(m.To))
	s.notice(ctx, m.From, m.Ticket, m.To+" is on leave", body)
}

// onConfigDelivered warns the human that an agent delivered changes to the
// project's .nobody/ (settings.ProtectWarn): they must review them before
// merging the feature, or the agents' configuration changes with it.
func (s *staff) onConfigDelivered(agent string, f work.Feature, files []string) {
	body := fmt.Sprintf("%s delivered changes to the agents' configuration (.nobody/) on the feature branch %s:\n%s\n\n"+
		"Agents must not change their own configuration. Review these changes before merging the feature, "+
		"and revert them if they were not intended.", agent, f.Branch, strings.Join(files, "\n"))
	s.notice(context.Background(), org.HumanAddress, "", agent+" changed .nobody/", body)
}

// releaseTickets puts the agent's tickets in progress back in their team's
// queue. The usual "back in your team's queue" notice then reaches a teammate.
func (s *staff) releaseTickets(ctx context.Context, address, reason string) {
	assigned, _, err := s.tickets.Mine(ctx, address)
	if err != nil {
		s.logger.Error("list tickets of a retired agent", "agent", address, "err", err)

		return
	}

	for _, t := range assigned {
		comment := fmt.Sprintf("%s %s: the ticket goes back to the team's queue. "+
			"Its work so far is in the team's worktree and in its comments and deliverables.", address, reason)
		if _, err := s.tickets.Comment(ctx, org.SystemAddress, t.ID, comment); err != nil {
			s.logger.Warn("comment released ticket", "ticket", t.ID, "err", err)
		}

		if _, err := s.tickets.SetStatus(ctx, org.SystemAddress, t.ID, work.StatusOpen); err != nil {
			s.logger.Error("release ticket of a retired agent", "agent", address, "ticket", t.ID, "err", err)
		}
	}
}

// returnMail tells the senders of a dismissed agent's undelivered messages
// that they will not be read, and clears its mailbox.
func (s *staff) returnMail(ctx context.Context, address string) {
	for _, priority := range []messaging.Priority{messaging.Urgent, messaging.Normal} {
		pending, err := s.store.PendingMessages(ctx, address, priority)
		if err != nil {
			s.logger.Error("list mail of a dismissed agent", "agent", address, "err", err)

			return
		}

		ids := make([]string, 0, len(pending))
		for _, m := range pending {
			ids = append(ids, m.ID)

			if m.From != org.SystemAddress {
				body := fmt.Sprintf("%s has left Nobody Inc: your message %q was not delivered. "+
					"Send it to someone else in their team (@%s) if it still matters.", address, m.Subject, org.TeamOf(address))
				s.notice(ctx, m.From, m.Ticket, "Message not delivered: "+m.Subject, body)
			}
		}

		if len(ids) == 0 {
			continue
		}

		if err := s.store.MarkDelivered(ctx, address, ids, time.Now().UTC()); err != nil {
			s.logger.Error("clear mail of a dismissed agent", "agent", address, "err", err)
		}
	}
}

func (s *staff) notice(ctx context.Context, to, ticket, subject, body string) {
	_, err := s.messages.Send(ctx, messaging.Draft{
		From:     org.SystemAddress,
		To:       to,
		Act:      messaging.ActInform,
		Priority: messaging.Normal,
		Subject:  subject,
		Body:     body,
		Ticket:   ticket,
	})
	if err != nil {
		s.logger.Warn("staff notice", "to", to, "err", err)
	}
}
