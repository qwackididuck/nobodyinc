package daemon

import (
	"context"

	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// announcingWorkStore is the store of the ticket service, which announces on
// the bus every ticket and feature it writes (docs/events.md). Wrapping the
// store catches every change, including comments, deliverables and
// cancellations, without the ticket service knowing about the control room.
type announcingWorkStore struct {
	work.Store

	bus *events.Bus
}

func (a announcingWorkStore) InsertTicket(ctx context.Context, t work.Ticket) (work.Ticket, error) {
	t, err := a.Store.InsertTicket(ctx, t)
	if err == nil {
		a.ticketChanged(t.ID)
	}

	return t, err
}

func (a announcingWorkStore) UpdateTicket(ctx context.Context, t work.Ticket) error {
	err := a.Store.UpdateTicket(ctx, t)
	if err == nil {
		a.ticketChanged(t.ID)
	}

	return err
}

func (a announcingWorkStore) InsertDeliverable(ctx context.Context, d work.Deliverable) (work.Deliverable, error) {
	d, err := a.Store.InsertDeliverable(ctx, d)
	if err == nil {
		a.ticketChanged(d.Ticket)
	}

	return d, err
}

func (a announcingWorkStore) InsertComment(ctx context.Context, c work.Comment) (work.Comment, error) {
	c, err := a.Store.InsertComment(ctx, c)
	if err == nil {
		a.ticketChanged(c.Ticket)
	}

	return c, err
}

func (a announcingWorkStore) InsertFeature(ctx context.Context, f work.Feature) (work.Feature, error) {
	f, err := a.Store.InsertFeature(ctx, f)
	if err == nil {
		a.featureChanged(f.ID)
	}

	return f, err
}

func (a announcingWorkStore) UpdateFeature(ctx context.Context, f work.Feature) error {
	err := a.Store.UpdateFeature(ctx, f)
	if err == nil {
		a.featureChanged(f.ID)
	}

	return err
}

func (a announcingWorkStore) ActivateFeature(ctx context.Context, id string) error {
	err := a.Store.ActivateFeature(ctx, id)
	if err == nil {
		a.featureChanged(id)
	}

	return err
}

func (a announcingWorkStore) DeactivateFeature(ctx context.Context, id string) error {
	err := a.Store.DeactivateFeature(ctx, id)
	if err == nil {
		a.featureChanged(id)
	}

	return err
}

func (a announcingWorkStore) ArchiveFeatureTickets(ctx context.Context, feature string) ([]string, error) {
	ids, err := a.Store.ArchiveFeatureTickets(ctx, feature)
	for _, id := range ids {
		a.ticketChanged(id)
	}

	return ids, err
}

func (a announcingWorkStore) DeleteTickets(ctx context.Context, ids []string) error {
	err := a.Store.DeleteTickets(ctx, ids)
	if err == nil {
		for _, id := range ids {
			a.ticketChanged(id)
		}
	}

	return err
}

func (a announcingWorkStore) ticketChanged(id string) {
	a.bus.Publish(events.Event{Type: events.TicketsChanged, Data: events.ChangedData{ID: id}})
}

func (a announcingWorkStore) featureChanged(id string) {
	a.bus.Publish(events.Event{Type: events.FeaturesChanged, Data: events.ChangedData{ID: id}})
}
