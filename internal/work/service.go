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
	ErrBusy      = errors.New("one ticket at a time: finish or release the ticket you are working on first")
)

// Store keeps tickets and features (implemented by internal/store).
type Store interface {
	TicketStore
	FeatureStore
}

// TicketStore keeps tickets with their deliverables and comments.
type TicketStore interface {
	InsertTicket(ctx context.Context, t Ticket) (Ticket, error)
	GetTicket(ctx context.Context, id string) (Ticket, error)
	UpdateTicket(ctx context.Context, t Ticket) error
	ListTickets(ctx context.Context, f Filter) ([]Ticket, error)
	InsertDeliverable(ctx context.Context, d Deliverable) (Deliverable, error)
	ListDeliverables(ctx context.Context, ticket string) ([]Deliverable, error)
	InsertComment(ctx context.Context, c Comment) (Comment, error)
	ListComments(ctx context.Context, ticket string) ([]Comment, error)
}

// FeatureStore keeps features.
type FeatureStore interface {
	InsertFeature(ctx context.Context, f Feature) (Feature, error)
	GetFeature(ctx context.Context, id string) (Feature, error)
	ListFeatures(ctx context.Context) ([]Feature, error)
	// ActivateFeature makes one feature the active one (the others are not).
	ActivateFeature(ctx context.Context, id string) error
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
	// Workspace, when set, ties tickets to git branches (see feature.go).
	Workspace Workspace
	// OnEvent, when set, is called after a change is stored.
	OnEvent func(Event)
	// OnFeatureStarted, when set, is called once a new feature is active.
	OnFeatureStarted func(Feature)
	Now              func() time.Time
}

// Create files a new ticket in a team's queue, or assigns it directly.
func (s *Service) Create(ctx context.Context, by string, d Draft) (Ticket, error) {
	if err := s.validateDraft(ctx, by, d); err != nil {
		return Ticket{}, err
	}

	blockedBy, err := s.normalizeDependencies(ctx, d.BlockedBy)
	if err != nil {
		return Ticket{}, err
	}

	feature, err := s.activeFeatureID(ctx)
	if err != nil {
		return Ticket{}, err
	}

	now := s.now()

	t := Ticket{
		Title:              strings.TrimSpace(d.Title),
		Description:        d.Description,
		AcceptanceCriteria: d.AcceptanceCriteria,
		Team:               d.Team,
		Feature:            feature,
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

	if err := s.checkFree(ctx, agent, t.ID); err != nil {
		return Ticket{}, err
	}

	if err := s.beforeClaim(ctx, t, agent); err != nil {
		return Ticket{}, err
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

	event, err := transition(status)
	if err != nil {
		return Ticket{}, err
	}

	var delivered []Deliverable
	if status == StatusDone {
		if delivered, err = s.beforeDone(ctx, t); err != nil {
			return Ticket{}, err
		}
	}

	if status == StatusOpen {
		t.Assignee = "" // back to the team's queue
	}

	t.Status = status

	t, err = s.save(ctx, t, event, by)
	if err != nil {
		return Ticket{}, err
	}

	return t, s.recordDeliverables(ctx, t, delivered)
}

// transition says which event a status change emits, or why it is refused.
func transition(status Status) (EventKind, error) {
	switch status {
	case StatusDone:
		return EventDone, nil
	case StatusOpen:
		return EventReleased, nil
	case StatusCanceled:
		return "", nil
	case StatusInProgress:
		return "", fmt.Errorf("%w: claim a ticket to start it", ErrInvalid)
	default:
		return "", fmt.Errorf("%w: status must be done, open (release) or canceled", ErrInvalid)
	}
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

// validateDraft applies the creation rules to a draft.
func (s *Service) validateDraft(ctx context.Context, by string, d Draft) error {
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("%w: a ticket needs a title", ErrInvalid)
	}

	if !s.teamExists(d.Team) {
		return fmt.Errorf("%w: unknown team %q", ErrInvalid, d.Team)
	}

	if !s.mayCreateFor(by, d.Team) {
		return fmt.Errorf("%w: you can create tickets for your own team and for teams your team has a channel to", ErrForbidden)
	}

	if d.Assignee != "" && org.TeamOf(d.Assignee) != d.Team {
		return fmt.Errorf("%w: the assignee must belong to team %q", ErrInvalid, d.Team)
	}

	return s.checkFree(ctx, d.Assignee, "")
}

// recordDeliverables attaches the commits delivered with a ticket.
func (s *Service) recordDeliverables(ctx context.Context, t Ticket, delivered []Deliverable) error {
	for _, d := range delivered {
		d.Ticket, d.CreatedAt = t.ID, s.now()
		if _, err := s.Store.InsertDeliverable(ctx, d); err != nil {
			return err
		}
	}

	return nil
}

// checkFree refuses a new ticket to an agent already working on another one.
func (s *Service) checkFree(ctx context.Context, agent, except string) error {
	if agent == "" {
		return nil
	}

	current, err := s.Store.ListTickets(ctx, Filter{Assignee: agent, Status: StatusInProgress})
	if err != nil {
		return err
	}

	for _, t := range current {
		if t.ID != except {
			return fmt.Errorf("%w: %s is working on %s", ErrBusy, agent, t.ID)
		}
	}

	return nil
}

// activeFeatureID returns the ID of the active feature, or "".
func (s *Service) activeFeatureID(ctx context.Context) (string, error) {
	features, err := s.Store.ListFeatures(ctx)
	if err != nil {
		return "", err
	}

	for _, f := range features {
		if f.Active {
			return f.ID, nil
		}
	}

	return "", nil
}

// beforeClaim brings the latest validated work into the claimer's worktree.
func (s *Service) beforeClaim(ctx context.Context, t Ticket, agent string) error {
	if s.Workspace == nil || t.Feature == "" {
		return nil
	}

	f, err := s.Store.GetFeature(ctx, t.Feature)
	if err != nil {
		return err
	}

	if err := s.Workspace.BeforeClaim(ctx, f, agent); err != nil {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}

	return nil
}

// beforeDone delivers the assignee's committed work to the feature branch.
func (s *Service) beforeDone(ctx context.Context, t Ticket) ([]Deliverable, error) {
	if s.Workspace == nil || t.Feature == "" || t.Assignee == "" {
		return nil, nil
	}

	f, err := s.Store.GetFeature(ctx, t.Feature)
	if err != nil {
		return nil, err
	}

	delivered, err := s.Workspace.BeforeDone(ctx, f, t.Assignee)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConflict, err)
	}

	return delivered, nil
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
