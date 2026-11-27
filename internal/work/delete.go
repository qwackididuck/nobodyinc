package work

// Deleting tickets for good: the Kanban's shredder and its bulk cleaning.
// Only the human deletes one ticket; the human and the director clean the
// board in bulk (CleanBoard). Archived tickets (those of finished features)
// stay. A deleted ticket an agent was working on is reported through
// OnTicketDeleted, so that the agent is told to stop.

import (
	"context"
	"fmt"
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// DeleteTicket deletes a ticket, with its comments and deliverables, and
// returns the IDs of the deleted tickets. With cascade, the tickets that
// depend on it, directly or not, are deleted too; otherwise they no longer
// wait for it.
func (s *Service) DeleteTicket(ctx context.Context, by, id string, cascade bool) ([]string, error) {
	if by != org.HumanAddress {
		return nil, fmt.Errorf("%w: only the human deletes tickets", ErrForbidden)
	}

	target, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return nil, err
	}

	if target.Archived {
		return nil, fmt.Errorf("%w: %s is archived with its feature", ErrConflict, target.ID)
	}

	all, err := s.Store.ListTickets(ctx, Filter{})
	if err != nil {
		return nil, err
	}

	ids := []string{target.ID}
	if cascade {
		ids = append(ids, Dependents(all, target.ID)...)
	}

	if err := s.Store.DeleteTickets(ctx, ids); err != nil {
		return nil, err
	}

	s.tellAgentsToStop(all, ids)

	return ids, nil
}

// CleanScope tells which tickets of the Kanban CleanBoard deletes.
type CleanScope string

// The scopes of CleanBoard.
const (
	// CleanClosed deletes the done and canceled tickets.
	CleanClosed CleanScope = "closed"
	// CleanAll deletes every ticket of the board, open and in progress too.
	CleanAll CleanScope = "all"
)

// CleanBoard deletes, for good, the tickets of the Kanban that scope covers,
// and returns their IDs (none when there was nothing to clean). Archived
// tickets stay. The human and the director may clean the board: a team agent
// may not. Like DeleteTicket, it tells the agents working on a deleted
// ticket to stop.
func (s *Service) CleanBoard(ctx context.Context, by string, scope CleanScope) ([]string, error) {
	if by != org.HumanAddress && !s.Org.Current().IsGod(by) {
		return nil, fmt.Errorf("%w: only the human and the director clean the board", ErrForbidden)
	}

	if scope != CleanClosed && scope != CleanAll {
		return nil, fmt.Errorf("%w: scope must be %q or %q, got %q", ErrInvalid, CleanClosed, CleanAll, scope)
	}

	all, err := s.Store.ListTickets(ctx, Filter{})
	if err != nil {
		return nil, err
	}

	ids := []string{}

	for _, t := range all {
		if scope == CleanAll || t.Status.Closed() {
			ids = append(ids, t.ID)
		}
	}

	if err := s.Store.DeleteTickets(ctx, ids); err != nil {
		return nil, err
	}

	s.tellAgentsToStop(all, ids)

	return ids, nil
}

// tellAgentsToStop reports, among the deleted tickets, those an agent was
// working on.
func (s *Service) tellAgentsToStop(tickets []Ticket, deleted []string) {
	if s.OnTicketDeleted == nil {
		return
	}

	for _, t := range tickets {
		if slices.Contains(deleted, t.ID) && t.Status == StatusInProgress && t.Assignee != "" {
			s.OnTicketDeleted(t)
		}
	}
}

// Dependents lists the tickets that depend on id, directly or through other
// tickets, in the order they are found.
func Dependents(tickets []Ticket, id string) []string {
	waiting := map[string][]string{} // ticket → tickets that wait for it

	for _, t := range tickets {
		for _, dep := range t.BlockedBy {
			waiting[dep] = append(waiting[dep], t.ID)
		}
	}

	var found []string

	seen := map[string]bool{id: true}
	queue := []string{id}

	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]

		for _, d := range waiting[next] {
			if !seen[d] {
				seen[d] = true
				found = append(found, d)
				queue = append(queue, d)
			}
		}
	}

	return found
}
