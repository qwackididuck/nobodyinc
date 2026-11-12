package work

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Errors returned by the service. Like the messaging errors, they are shown
// to agents, so they say what to do instead.
var (
	ErrNotFound  = errors.New("ticket not found")
	ErrInvalid   = errors.New("invalid ticket request")
	ErrForbidden = errors.New("not allowed")
	ErrBlocked   = errors.New("this ticket depends on tickets that are not done yet")
	ErrConflict  = errors.New("this ticket cannot change that way")
	ErrNoTicket  = errors.New("no ticket is available in your team's queue")
)

// Store keeps tickets (implemented by internal/store).
type Store interface {
	InsertTicket(ctx context.Context, t Ticket) (Ticket, error)
	GetTicket(ctx context.Context, id string) (Ticket, error)
	UpdateTicket(ctx context.Context, t Ticket) error
	ListTickets(ctx context.Context, f Filter) ([]Ticket, error)
	InsertDeliverable(ctx context.Context, d Deliverable) (Deliverable, error)
	ListDeliverables(ctx context.Context, ticket string) ([]Deliverable, error)
	InsertComment(ctx context.Context, c Comment) (Comment, error)
	ListComments(ctx context.Context, ticket string) ([]Comment, error)
}

// Filter narrows ListTickets; empty fields match everything.
type Filter struct {
	Team     string
	Assignee string
	Status   Status
}

// Event is something that happened to a ticket, for notifications.
type Event struct {
	Kind   EventKind
	Ticket Ticket
	By     string
}

// EventKind says what happened.
type EventKind string

// The ticket events notifications react to.
const (
	EventCreated  EventKind = "created"
	EventClaimed  EventKind = "claimed"
	EventReleased EventKind = "released"
	EventDone     EventKind = "done"
)

// Draft is what a creator provides.
type Draft struct {
	Title              string
	Description        string
	AcceptanceCriteria string
	Team               string
	Assignee           string // optional: assign right away instead of queueing
	BlockedBy          []string
}

// Service applies the ticket rules described in the package documentation.
type Service struct {
	Store Store
	Org   *org.Org
	// OnEvent, when set, is called after a change is stored.
	OnEvent func(Event)
	Now     func() time.Time
}

// Create files a new ticket in a team's queue, or assigns it directly.
func (s *Service) Create(ctx context.Context, by string, d Draft) (Ticket, error) {
	if strings.TrimSpace(d.Title) == "" {
		return Ticket{}, fmt.Errorf("%w: a ticket needs a title", ErrInvalid)
	}

	if !s.teamExists(d.Team) {
		return Ticket{}, fmt.Errorf("%w: unknown team %q", ErrInvalid, d.Team)
	}

	if !s.mayCreateFor(by, d.Team) {
		return Ticket{}, fmt.Errorf("%w: you can create tickets for your own team and for teams your team has a channel to", ErrForbidden)
	}

	if d.Assignee != "" && org.TeamOf(d.Assignee) != d.Team {
		return Ticket{}, fmt.Errorf("%w: the assignee must belong to team %q", ErrInvalid, d.Team)
	}

	blockedBy, err := s.normalizeDependencies(ctx, d.BlockedBy)
	if err != nil {
		return Ticket{}, err
	}

	now := s.now()

	t := Ticket{
		Title:              strings.TrimSpace(d.Title),
		Description:        d.Description,
		AcceptanceCriteria: d.AcceptanceCriteria,
		Team:               d.Team,
		Assignee:           d.Assignee,
		Status:             StatusOpen,
		BlockedBy:          blockedBy,
		CreatedBy:          by,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if t.Assignee != "" {
		t.Status = StatusInProgress
	}

	t, err = s.Store.InsertTicket(ctx, t)
	if err != nil {
		return Ticket{}, err
	}

	s.emit(EventCreated, t, by)

	return t, nil
}

// Claim gives a ticket to an agent. With an empty id, the agent takes the
// oldest ticket of its team's queue that is not blocked.
func (s *Service) Claim(ctx context.Context, agent, id string) (Ticket, error) {
	t, err := s.ticketToClaim(ctx, agent, id)
	if err != nil {
		return Ticket{}, err
	}

	if org.TeamOf(agent) != t.Team {
		return Ticket{}, fmt.Errorf("%w: %s belongs to team %q", ErrForbidden, t.ID, t.Team)
	}

	if t.Status != StatusOpen || (t.Assignee != "" && t.Assignee != agent) {
		return Ticket{}, fmt.Errorf("%w: %s is %s (assignee %q)", ErrConflict, t.ID, t.Status, t.Assignee)
	}

	blocked, err := s.blocked(ctx, t)
	if err != nil {
		return Ticket{}, err
	}

	if blocked {
		return Ticket{}, fmt.Errorf("%w: %s waits for %s", ErrBlocked, t.ID, strings.Join(t.BlockedBy, ", "))
	}

	t.Assignee = agent
	t.Status = StatusInProgress

	return s.save(ctx, t, EventClaimed, agent)
}

// SetStatus moves a ticket on: done, canceled, or open again (released back
// to the team's queue).
func (s *Service) SetStatus(ctx context.Context, by, id string, status Status) (Ticket, error) {
	t, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return Ticket{}, err
	}

	if !s.isPrivileged(by) && by != t.Assignee {
		return Ticket{}, fmt.Errorf("%w: only the assignee of %s or the human can change its status", ErrForbidden, t.ID)
	}

	if t.Status.Closed() {
		return Ticket{}, fmt.Errorf("%w: %s is already %s", ErrConflict, t.ID, t.Status)
	}

	var event EventKind

	switch status {
	case StatusDone:
		event = EventDone
	case StatusOpen:
		t.Assignee = ""
		event = EventReleased
	case StatusCanceled:
		event = ""
	case StatusInProgress:
		return Ticket{}, fmt.Errorf("%w: claim a ticket to start it", ErrInvalid)
	default:
		return Ticket{}, fmt.Errorf("%w: status must be done, open (release) or canceled", ErrInvalid)
	}

	t.Status = status

	return s.save(ctx, t, event, by)
}

// Comment adds a remark to a ticket.
func (s *Service) Comment(ctx context.Context, by, id, body string) (Comment, error) {
	if strings.TrimSpace(body) == "" {
		return Comment{}, fmt.Errorf("%w: a comment needs a body", ErrInvalid)
	}

	t, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return Comment{}, err
	}

	return s.Store.InsertComment(ctx, Comment{Ticket: t.ID, Author: by, Body: body, CreatedAt: s.now()})
}

// AddDeliverable records something produced for a ticket.
func (s *Service) AddDeliverable(ctx context.Context, by, id string, d Deliverable) (Deliverable, error) {
	if !slices.Contains(DeliverableKinds, d.Kind) {
		return Deliverable{}, fmt.Errorf("%w: kind must be one of %s", ErrInvalid, strings.Join(DeliverableKinds, ", "))
	}

	if strings.TrimSpace(d.Summary) == "" {
		return Deliverable{}, fmt.Errorf("%w: a deliverable needs a summary", ErrInvalid)
	}

	t, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return Deliverable{}, err
	}

	d.Ticket, d.Author, d.CreatedAt = t.ID, by, s.now()

	return s.Store.InsertDeliverable(ctx, d)
}

// Details is a ticket with everything attached to it.
type Details struct {
	Ticket       Ticket        `json:"ticket"`
	Deliverables []Deliverable `json:"deliverables"`
	Comments     []Comment     `json:"comments"`
}

// Get returns a ticket with its deliverables and comments.
func (s *Service) Get(ctx context.Context, id string) (Details, error) {
	t, err := s.Store.GetTicket(ctx, id)
	if err != nil {
		return Details{}, err
	}

	deliverables, err := s.Store.ListDeliverables(ctx, t.ID)
	if err != nil {
		return Details{}, err
	}

	comments, err := s.Store.ListComments(ctx, t.ID)
	if err != nil {
		return Details{}, err
	}
	// Empty lists rather than null, for JSON consumers.
	return Details{
		Ticket:       t,
		Deliverables: append([]Deliverable{}, deliverables...),
		Comments:     append([]Comment{}, comments...),
	}, nil
}

// Mine lists an agent's own unfinished tickets, then its team's queue.
func (s *Service) Mine(ctx context.Context, agent string) ([]Ticket, []Ticket, error) {
	assigned, err := s.Store.ListTickets(ctx, Filter{Assignee: agent, Status: StatusInProgress})
	if err != nil {
		return nil, nil, err
	}

	queue, err := s.Available(ctx, org.TeamOf(agent))

	return assigned, queue, err
}

// Available lists the tickets of a team's queue that can be claimed now,
// oldest first.
func (s *Service) Available(ctx context.Context, team string) ([]Ticket, error) {
	queue, err := s.Store.ListTickets(ctx, Filter{Team: team, Status: StatusOpen})
	if err != nil {
		return nil, err
	}

	var available []Ticket

	for _, t := range queue {
		if t.Assignee != "" {
			continue
		}

		blocked, err := s.blocked(ctx, t)
		if err != nil {
			return nil, err
		}

		if !blocked {
			available = append(available, t)
		}
	}

	return available, nil
}

// Unblocked lists the open tickets that were waiting for done and can now be
// claimed.
func (s *Service) Unblocked(ctx context.Context, done Ticket) ([]Ticket, error) {
	open, err := s.Store.ListTickets(ctx, Filter{Status: StatusOpen})
	if err != nil {
		return nil, err
	}

	var unblocked []Ticket

	for _, t := range open {
		if !slices.Contains(t.BlockedBy, done.ID) {
			continue
		}

		blocked, err := s.blocked(ctx, t)
		if err != nil {
			return nil, err
		}

		if !blocked {
			unblocked = append(unblocked, t)
		}
	}

	return unblocked, nil
}

func (s *Service) ticketToClaim(ctx context.Context, agent, id string) (Ticket, error) {
	if id != "" {
		return s.Store.GetTicket(ctx, id)
	}

	available, err := s.Available(ctx, org.TeamOf(agent))
	if err != nil {
		return Ticket{}, err
	}

	if len(available) == 0 {
		return Ticket{}, ErrNoTicket
	}

	return available[0], nil
}

// blocked reports whether a ticket still waits for an unfinished dependency.
func (s *Service) blocked(ctx context.Context, t Ticket) (bool, error) {
	for _, dep := range t.BlockedBy {
		d, err := s.Store.GetTicket(ctx, dep)
		if err != nil {
			return false, err
		}

		if d.Status != StatusDone {
			return true, nil
		}
	}

	return false, nil
}

func (s *Service) normalizeDependencies(ctx context.Context, ids []string) ([]string, error) {
	var deps []string

	for _, id := range ids {
		n, err := ParseID(id)
		if err != nil {
			return nil, err
		}

		dep := FormatID(n)
		if _, err := s.Store.GetTicket(ctx, dep); err != nil {
			return nil, fmt.Errorf("%w: dependency %s: %w", ErrInvalid, dep, err)
		}

		if !slices.Contains(deps, dep) {
			deps = append(deps, dep)
		}
	}

	return deps, nil
}

func (s *Service) save(ctx context.Context, t Ticket, event EventKind, by string) (Ticket, error) {
	t.UpdatedAt = s.now()
	if err := s.Store.UpdateTicket(ctx, t); err != nil {
		return Ticket{}, err
	}

	if event != "" {
		s.emit(event, t, by)
	}

	return t, nil
}

func (s *Service) emit(kind EventKind, t Ticket, by string) {
	if s.OnEvent != nil {
		s.OnEvent(Event{Kind: kind, Ticket: t, By: by})
	}
}

func (s *Service) mayCreateFor(by, team string) bool {
	if s.isPrivileged(by) {
		return true
	}

	myTeam := org.TeamOf(by)

	return myTeam == team || s.Org.HasChannel(myTeam, team)
}

func (s *Service) isPrivileged(address string) bool {
	return address == org.HumanAddress || address == org.SystemAddress
}

func (s *Service) teamExists(team string) bool {
	return slices.ContainsFunc(s.Org.Teams, func(t org.Team) bool { return t.ID == team })
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now().UTC()
}
