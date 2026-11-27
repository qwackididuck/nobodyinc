package daemon

// Closing the conversations between agents that nobody answers (messaging.Closer,
// docs/events.md "Closing open conversations"). They close:
//   - on request: the human, from the wastebasket, and the director
//     (conversations_close);
//   - when a ticket they are about is done or canceled;
//   - when their feature is finished (the tickets of the feature);
//   - when they are old: still waiting after settings.CloseOpenAfterDays.
//
// Each time, the agents that waited are told by `nobody`.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/settings"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// sweepEvery is how often old open conversations are looked for.
const sweepEvery = time.Hour

// newCloser builds the Closer; what it deletes is announced on the bus.
func newCloser(st *store.Store, messages *messaging.Service, bus *events.Bus, logger *slog.Logger) *messaging.Closer {
	return &messaging.Closer{
		Store: st, Messages: messages, Logger: logger,
		OnDeleted: func(ids []string) {
			bus.Publish(events.Event{Type: events.MessagesDeleted, Data: events.DeletedData{IDs: ids}})
		},
	}
}

// conversationsOffice backs the director's conversations_close tool.
type conversationsOffice struct{ closer *messaging.Closer }

// CloseOpen closes every open conversation between agents.
func (c conversationsOffice) CloseOpen(ctx context.Context) ([]string, error) {
	return c.closer.Close(ctx, messaging.CloseScope{Reason: "the director closed it, with the human's agreement"})
}

// closeForTicket closes the open conversations about a ticket that is done
// or canceled.
func closeForTicket(ctx context.Context, closer *messaging.Closer, t work.Ticket, logger *slog.Logger) {
	_, err := closer.Close(ctx, messaging.CloseScope{
		Tickets: []string{t.ID}, Reason: fmt.Sprintf("its ticket %s is closed", t.ID),
	})
	if err != nil {
		logger.Warn("close the conversations of a closed ticket", "ticket", t.ID, "err", err)
	}
}

// closeForFeature closes the open conversations about the tickets of a
// finished feature (they are archived by then).
func closeForFeature(ctx context.Context, closer *messaging.Closer, tickets *work.Service, f work.Feature, logger *slog.Logger) {
	all, err := tickets.Store.ListTickets(ctx, work.Filter{Archived: true})
	if err != nil {
		logger.Warn("list the tickets of a finished feature", "feature", f.ID, "err", err)

		return
	}

	ids := []string{}

	for _, t := range all {
		if t.Feature == f.ID {
			ids = append(ids, t.ID)
		}
	}

	if _, err := closer.Close(ctx, messaging.CloseScope{Tickets: ids, Reason: fmt.Sprintf("its feature %s is finished", f.ID)}); err != nil {
		logger.Warn("close the conversations of a finished feature", "feature", f.ID, "err", err)
	}
}

// sweeping is a running sweep: stop it, then wait for done.
type sweeping struct {
	stop context.CancelFunc
	done chan struct{}
}

// sweepOldConversations closes the conversations still open after the
// configured number of days: once at start, then every sweepEvery, until ctx
// ends or the sweep is stopped (Daemon.Close waits for it before closing the
// database).
func sweepOldConversations(
	ctx context.Context, closer *messaging.Closer, clock schedule.Clock, current func() settings.Settings, logger *slog.Logger,
) sweeping {
	ctx, stop := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})

	sweep := func() {
		days := current().CloseOpenAfterDays
		if days <= 0 {
			return
		}

		_, err := closer.Close(ctx, messaging.CloseScope{
			Before: clock.Now().Add(-time.Duration(days) * 24 * time.Hour),
			Reason: fmt.Sprintf("nobody answered for %d days", days),
		})
		if err != nil && ctx.Err() == nil {
			logger.Warn("close the old conversations", "err", err)
		}
	}

	go func() {
		defer close(done)

		sweep()

		for {
			// A real timer, not the clock's: a test's manual clock counts the
			// timers armed on it (schedule.ManualClock.WaitForTimers). The
			// clock only tells how old a conversation is.
			timer := time.NewTimer(sweepEvery)

			select {
			case <-ctx.Done():
				timer.Stop()

				return
			case <-timer.C:
				sweep()
			}
		}
	}()

	return sweeping{stop: stop, done: done}
}
