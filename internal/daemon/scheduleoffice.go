package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// scheduleOffice is what the API and the director's tools do with scheduled
// tasks (ADR-0012): the book (schedules.yaml), the scheduler, the
// occurrences (scheduledWork) and the director's proposals.
type scheduleOffice struct {
	book      *schedule.Book
	scheduler *schedule.Scheduler
	work      *scheduledWork
	store     *store.Store
	messages  *messaging.Service
	org       org.Source
	logger    *slog.Logger
}

// List returns every schedule with its next and last occurrences.
func (o *scheduleOffice) List(ctx context.Context) ([]schedule.View, error) {
	views := []schedule.View{}

	for _, s := range o.book.List() {
		v, err := o.view(ctx, s)
		if err != nil {
			return nil, err
		}

		views = append(views, v)
	}

	return views, nil
}

// Create adds a schedule, enabled.
func (o *scheduleOffice) Create(ctx context.Context, s schedule.Schedule) (schedule.View, error) {
	created, err := o.book.Create(s)
	if err != nil {
		return schedule.View{}, err
	}

	return o.view(ctx, created)
}

// Update edits a schedule: armed again from now, an occurrence still
// waiting is canceled.
func (o *scheduleOffice) Update(ctx context.Context, id string, s schedule.Schedule) (schedule.View, error) {
	if s.ID != "" && s.ID != id {
		return schedule.View{}, fmt.Errorf("%w: the id cannot change (%q in the body, %q in the path)", schedule.ErrInvalid, s.ID, id)
	}

	updated, err := o.book.Update(id, s)
	if err != nil {
		return schedule.View{}, err
	}

	o.work.cancelWaiting(ctx, id, "the schedule was edited")

	return o.view(ctx, updated)
}

// Delete removes a schedule; its history stays.
func (o *scheduleOffice) Delete(ctx context.Context, id string) error {
	if err := o.book.Delete(id); err != nil {
		return err
	}

	o.work.cancelWaiting(ctx, id, "the schedule was deleted")

	return nil
}

// SetEnabled pauses or resumes a schedule.
func (o *scheduleOffice) SetEnabled(ctx context.Context, id string, enabled bool) (schedule.View, error) {
	s, err := o.book.SetEnabled(id, enabled)
	if err != nil {
		return schedule.View{}, err
	}

	if !enabled {
		o.work.cancelWaiting(ctx, id, "the schedule was paused")
	}

	return o.view(ctx, s)
}

// RunNow starts one occurrence at once, paused or not.
func (o *scheduleOffice) RunNow(ctx context.Context, id string) (schedule.Run, error) {
	s, ok := o.book.Get(id)
	if !ok {
		return schedule.Run{}, fmt.Errorf("%w: %q", schedule.ErrNotFound, id)
	}

	return o.work.start(ctx, s, o.work.clock.Now(), schedule.TriggerManual)
}

// Runs returns occurrences, newest first.
func (o *scheduleOffice) Runs(ctx context.Context, scheduleID string, limit int) ([]schedule.Run, error) {
	runs, err := o.store.ListRuns(ctx, store.RunFilter{Schedule: scheduleID, Limit: limit})
	if runs == nil {
		runs = []schedule.Run{}
	}

	return runs, err
}

// Propose records a schedule the director proposes and sends it to the
// human, who accepts or refuses it (ADR-0012, point 9). Nothing is written
// to schedules.yaml yet.
func (o *scheduleOffice) Propose(ctx context.Context, by string, s schedule.Schedule) (schedule.Proposal, error) {
	cur := o.org.Current()
	if !cur.IsGod(by) {
		return schedule.Proposal{}, errors.New("only the director proposes schedules")
	}

	s.Enabled = true

	if err := s.Check(); err != nil {
		return schedule.Proposal{}, err
	}

	if err := s.CheckTarget(cur); err != nil {
		return schedule.Proposal{}, err
	}

	if _, ok := o.book.Get(s.ID); ok {
		return schedule.Proposal{}, fmt.Errorf("%w: %q (see schedule_list)", schedule.ErrExists, s.ID)
	}

	p, err := o.store.InsertProposal(ctx, schedule.Proposal{
		Schedule: s, Status: schedule.ProposalPending, ProposedBy: by, CreatedAt: o.work.clock.Now(),
	})
	if err != nil {
		return schedule.Proposal{}, err
	}

	msg, err := o.messages.Send(ctx, messaging.Draft{
		From: by, To: org.HumanAddress, Act: messaging.ActPropose, Priority: messaging.Normal,
		Subject: "Schedule proposal " + p.ID + ": " + s.Title, Body: proposalText(p, cur),
	})
	if err != nil {
		o.logger.Error("send a schedule proposal", "proposal", p.ID, "err", err)
	} else {
		p.ProposalMessage = msg.ID
		if err := o.store.UpdateProposal(ctx, p); err != nil {
			return schedule.Proposal{}, err
		}
	}

	o.work.changed()

	return o.withWarning(p), nil
}

// Proposals returns the director's proposals, newest first.
func (o *scheduleOffice) Proposals(ctx context.Context) ([]schedule.Proposal, error) {
	proposals, err := o.store.ListProposals(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]schedule.Proposal, 0, len(proposals))
	for _, p := range proposals {
		out = append(out, o.withWarning(p))
	}

	return out, nil
}

// Accept writes a proposed schedule, possibly as the human edited it, and
// answers the director with "agree".
func (o *scheduleOffice) Accept(ctx context.Context, id string, edited *schedule.Schedule) (schedule.Proposal, error) {
	p, err := o.pending(ctx, id)
	if err != nil {
		return schedule.Proposal{}, err
	}

	if edited != nil {
		p.Schedule = *edited
	}

	if p.Schedule, err = o.book.Create(p.Schedule); err != nil {
		return schedule.Proposal{}, err
	}

	now := o.work.clock.Now()
	p.Status, p.DecidedAt = schedule.ProposalAccepted, &now

	if err := o.store.UpdateProposal(ctx, p); err != nil {
		return schedule.Proposal{}, err
	}

	note := ""
	if edited != nil {
		note = " The human edited it before accepting; see schedule_list for what runs."
	}

	o.answer(ctx, p, messaging.ActAgree, "Schedule "+p.Schedule.ID+" accepted: "+p.Schedule.Title,
		fmt.Sprintf("The schedule %s is written and runs %s.%s", p.Schedule.ID, recurrenceText(p.Schedule), note))
	o.work.changed()

	return o.withWarning(p), nil
}

// Refuse turns a proposal down and answers the director with "refuse".
func (o *scheduleOffice) Refuse(ctx context.Context, id, reason string) (schedule.Proposal, error) {
	p, err := o.pending(ctx, id)
	if err != nil {
		return schedule.Proposal{}, err
	}

	now := o.work.clock.Now()
	p.Status, p.DecidedAt, p.RefusalReason = schedule.ProposalRefused, &now, strings.TrimSpace(reason)

	if err := o.store.UpdateProposal(ctx, p); err != nil {
		return schedule.Proposal{}, err
	}

	o.answer(ctx, p, messaging.ActRefuse, "Schedule proposal "+p.ID+" refused: "+p.Schedule.Title, "Reason: "+orNone(p.RefusalReason))
	o.work.changed()

	return o.withWarning(p), nil
}

func (o *scheduleOffice) view(ctx context.Context, s schedule.Schedule) (schedule.View, error) {
	v := schedule.View{Schedule: s, Warning: s.Warning(o.org.Current())}

	if next, ok := o.scheduler.Next(s.ID); ok {
		v.NextRun = &next
	}

	last, err := o.store.ListRuns(ctx, store.RunFilter{Schedule: s.ID, Limit: 1})
	if err != nil {
		return schedule.View{}, err
	}

	if len(last) == 1 {
		v.LastRun = &last[0]
	}

	return v, nil
}

// disable turns off a schedule whose target is gone (scheduledWork).
func (o *scheduleOffice) disable(id string) error {
	_, err := o.book.SetEnabled(id, false)

	return err
}

// onBookChanged arms the scheduler with the schedules as written, and tells
// the control room.
func (o *scheduleOffice) onBookChanged(schedules []schedule.Schedule) {
	o.scheduler.Set(schedules)
	o.work.changed()
}

// pending returns a proposal not decided yet.
func (o *scheduleOffice) pending(ctx context.Context, id string) (schedule.Proposal, error) {
	p, err := o.store.GetProposal(ctx, id)
	if err != nil {
		return schedule.Proposal{}, err
	}

	if p.Status != schedule.ProposalPending {
		return schedule.Proposal{}, fmt.Errorf("%w: %s is %s", schedule.ErrDecided, p.ID, p.Status)
	}

	return p, nil
}

// answer replies to the director's proposal message.
func (o *scheduleOffice) answer(ctx context.Context, p schedule.Proposal, act messaging.Act, subject, body string) {
	_, err := o.messages.Send(ctx, messaging.Draft{
		From: org.HumanAddress, To: p.ProposedBy, Act: act, Priority: messaging.Normal,
		InReplyTo: p.ProposalMessage, Subject: subject, Body: body,
	})
	if err != nil {
		o.logger.Error("answer a schedule proposal", "proposal", p.ID, "err", err)
	}
}

func (o *scheduleOffice) withWarning(p schedule.Proposal) schedule.Proposal {
	if p.Status == schedule.ProposalPending {
		p.Warning = p.Schedule.Warning(o.org.Current())
	}

	return p
}

func proposalText(p schedule.Proposal, o *org.Org) string {
	s := p.Schedule

	text := fmt.Sprintf("I propose the scheduled task %s: %q, %s, for %s, in %s mode.\n\nInstructions:\n%s\n\n"+
		"Accept it (possibly after editing it) or refuse it from the control room (this chat, or the Schedules tab).",
		s.ID, s.Title, recurrenceText(s), s.Target, s.Mode, s.Instructions)

	if w := s.Warning(o); w != "" {
		text += "\n\nWarning: " + w
	}

	return text
}

// scheduleDeps is what the scheduled tasks are wired to.
type scheduleDeps struct {
	statuses *runtime.Statuses
	tickets  *work.Service
	messages *messaging.Service
	store    *store.Store
	bus      *events.Bus
}

// newScheduleOffice reads the project's schedules, settles the occurrences
// the daemon left behind when it last stopped, and arms the scheduler (not
// running yet: see New).
func newScheduleOffice(ctx context.Context, opts Options, deps scheduleDeps) (*scheduleOffice, error) {
	clock := opts.Clock
	if clock == nil {
		clock = schedule.RealClock{}
	}

	w := &scheduledWork{
		org: opts.Org, statuses: deps.statuses, tickets: deps.tickets, messages: deps.messages, store: deps.store,
		clock: clock, bus: deps.bus, logger: opts.Logger, unavailable: map[string]bool{},
	}
	office := &scheduleOffice{
		work: w, store: deps.store, messages: deps.messages, org: opts.Org, logger: opts.Logger,
		scheduler: schedule.NewScheduler(clock, w.due),
	}

	book, err := schedule.OpenBook(opts.Org.Current().ProjectDir, opts.Org, office.onBookChanged)
	if err != nil {
		return nil, fmt.Errorf("read the scheduled tasks: %w", err)
	}

	office.book = book
	w.lookup, w.disable = book.Get, office.disable

	w.reconcile(ctx)
	office.scheduler.Set(book.List())

	return office, nil
}
