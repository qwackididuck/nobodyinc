package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// scheduledWork carries out the occurrences of scheduled tasks (ADR-0012,
// docs/schedules.md). When a schedule falls due (start):
//
//  1. its previous occurrence is still waiting or running: this one is
//     skipped;
//  2. no agent can ever take it (the target is gone): the schedule is
//     disabled and the human told;
//  3. none of its agents runs (on leave, stopped): skipped, and the human
//     told once per streak;
//  4. otherwise it waits until a target agent is free (idle, no ticket in
//     progress), then goes to that agent, signed by nobody: a ticket
//     assigned to it outside any feature, or mail in its normal tray.
//
// It ends when its ticket is done or canceled, or, in message mode, when
// the agent is idle again after reading the mail. Waiting occurrences are
// looked at again whenever an agent becomes idle or a ticket is closed.
//
// Occurrences are rows in the database (store.InsertRun), so a restart finds
// them (reconcile). One mutex serializes the whole life cycle. While it is
// held, the work service and the post office are called, which call back
// onTicketEvent synchronously for the ticket created: that callback takes
// the lock only for closed tickets, never created ones.
type scheduledWork struct {
	org      org.Source
	statuses *runtime.Statuses
	tickets  *work.Service
	messages *messaging.Service
	store    *store.Store
	clock    schedule.Clock
	bus      *events.Bus
	logger   *slog.Logger
	// lookup returns a schedule's current definition (schedule.Book.Get).
	lookup func(id string) (schedule.Schedule, bool)
	// disable turns off a schedule whose target is gone; called without the
	// lock held, as it writes schedules.yaml.
	disable func(id string) error

	mu sync.Mutex
	// unavailable remembers the schedules whose last occurrence found no
	// running agent, so the human is told once per streak.
	unavailable map[string]bool
}

// activeStatuses are the statuses of an occurrence that is not over.
var activeStatuses = []schedule.RunStatus{schedule.RunWaiting, schedule.RunRunning}

// due is the scheduler's callback: a schedule's occurrence is reached.
func (w *scheduledWork) due(ctx context.Context, s schedule.Schedule, at time.Time) {
	if _, err := w.start(ctx, s, at, schedule.TriggerSchedule); err != nil {
		w.logger.Error("start a scheduled task", "schedule", s.ID, "err", err)
	}
}

// start records a new occurrence of s and dispatches it if an agent is
// free. A manual occurrence ("Run now") is refused with schedule.ErrBusy
// while another one is not over, instead of being skipped.
func (w *scheduledWork) start(ctx context.Context, s schedule.Schedule, at time.Time, trigger schedule.Trigger) (schedule.Run, error) {
	run, gone, unavailable, err := w.record(ctx, s, at, trigger)
	if err != nil {
		return schedule.Run{}, err
	}

	w.changed()

	// Telling the human and writing schedules.yaml happen without the lock.
	if gone != "" {
		if err := w.disable(s.ID); err != nil {
			w.logger.Error("disable a schedule whose target is gone", "schedule", s.ID, "err", err)
		}

		w.tellHuman(ctx, "Schedule "+s.ID+" disabled", fmt.Sprintf(
			"The scheduled task %q (%s) targets %s, which cannot take it any more (%s). It is disabled. "+
				"Change its target in the Schedules tab, then resume it.", s.Title, s.ID, s.Target, gone))
	}

	if unavailable {
		w.tellHuman(ctx, "Schedule "+s.ID+": no agent to run it", fmt.Sprintf(
			"The scheduled task %q (%s) targets %s, but none of its agents is running (on leave, stopped, "+
				"or the daemon was started without agents). This occurrence is skipped, and so are the next ones "+
				"until one of them runs again; you will not be told again until then.", s.Title, s.ID, s.Target))
	}

	return run, nil
}

// record applies the dispatch rules under the lock. It returns why the
// target is gone ("" if not), and whether the human must be told that no
// target agent runs.
func (w *scheduledWork) record(
	ctx context.Context, s schedule.Schedule, at time.Time, trigger schedule.Trigger,
) (schedule.Run, string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	previous, err := w.activeRuns(ctx, s.ID)
	if err != nil {
		return schedule.Run{}, "", false, err
	}

	if len(previous) > 0 && trigger == schedule.TriggerManual {
		return schedule.Run{}, "", false, fmt.Errorf("%w: occurrence %d is %s", schedule.ErrBusy, previous[0].ID, previous[0].Status)
	}

	o := w.org.Current()
	run := schedule.Run{ScheduleID: s.ID, ScheduleTitle: s.Title, Trigger: trigger, Mode: s.Mode, DueAt: at}
	outcome := w.decide(s, o, previous)
	run.Status, run.Note = outcome.status, outcome.note

	if run, err = w.store.InsertRun(ctx, run); err != nil {
		return schedule.Run{}, "", false, err
	}

	unavailable := false

	switch {
	case outcome.gone != "":
	case run.Status == schedule.RunUnavailable:
		unavailable = !w.unavailable[s.ID]
		w.unavailable[s.ID] = true
	case run.Status == schedule.RunWaiting:
		delete(w.unavailable, s.ID)
		w.dispatchWaiting(ctx)

		// The dispatch may have started it.
		if latest, err := w.store.ListRuns(ctx, store.RunFilter{Schedule: s.ID, Limit: 1}); err == nil && len(latest) == 1 {
			run = latest[0]
		}
	}

	return run, outcome.gone, unavailable, nil
}

// outcome is what becomes of a new occurrence.
type outcome struct {
	status schedule.RunStatus
	note   string
	// gone says why no agent can ever take it, when the target is gone.
	gone string
}

// decide applies the dispatch rules (ADR-0012, point 6) to a new occurrence.
func (w *scheduledWork) decide(s schedule.Schedule, o *org.Org, previous []schedule.Run) outcome {
	candidates := s.Candidates(o)

	switch {
	case len(previous) > 0:
		return outcome{status: schedule.RunSkipped, note: fmt.Sprintf("the previous occurrence (%d) is not over", previous[0].ID)}
	case len(candidates) == 0:
		return outcome{
			status: schedule.RunUnavailable, note: "the target is gone; the schedule is disabled", gone: targetGone(s, o),
		}
	case len(w.running(candidates)) == 0:
		return outcome{status: schedule.RunUnavailable, note: "none of the target's agents is running"}
	default:
		return outcome{status: schedule.RunWaiting}
	}
}

// targetGone says why no agent can take s, from CheckTarget's error.
func targetGone(s schedule.Schedule, o *org.Org) string {
	err := s.CheckTarget(o)
	if err == nil {
		return "it has no agent"
	}

	_, why, _ := strings.Cut(err.Error(), s.Target+"\": ")

	return why
}

// running keeps the candidates that are running and not on leave.
func (w *scheduledWork) running(candidates []org.Member) []org.Member {
	var running []org.Member

	for _, m := range candidates {
		st := w.statuses.Get(m.Address)
		if !m.Agent.OnLeave && st != runtime.StatusStopped && st != runtime.StatusExited {
			running = append(running, m)
		}
	}

	return running
}

// activeRuns returns a schedule's occurrences that are not over, after
// ending those whose ticket or agent is gone (refresh).
func (w *scheduledWork) activeRuns(ctx context.Context, scheduleID string) ([]schedule.Run, error) {
	runs, err := w.store.ListRuns(ctx, store.RunFilter{Schedule: scheduleID, Statuses: activeStatuses})
	if err != nil {
		return nil, err
	}

	var active []schedule.Run

	for _, r := range runs {
		if w.refresh(ctx, &r) {
			active = append(active, r)
		}
	}

	return active, nil
}

// refresh ends a running occurrence whose ticket was closed or deleted, or
// whose agent was dismissed, without being told (a ticket deleted while in
// the queue, an event missed). It reports whether the occurrence is still
// active.
func (w *scheduledWork) refresh(ctx context.Context, r *schedule.Run) bool {
	if r.Status != schedule.RunRunning {
		return true
	}

	if r.Ticket != "" {
		t, err := w.tickets.Store.GetTicket(ctx, r.Ticket)

		switch {
		case err != nil:
			w.end(ctx, r, schedule.RunCanceled, "the ticket was deleted")
		case t.Status == work.StatusDone:
			w.end(ctx, r, schedule.RunDone, "")
		case t.Status == work.StatusCanceled:
			w.end(ctx, r, schedule.RunCanceled, "the ticket was canceled")
		default:
			return true
		}

		return false
	}

	if _, ok := w.org.Current().Member(r.Agent); !ok {
		w.end(ctx, r, schedule.RunCanceled, r.Agent+" was dismissed")

		return false
	}

	return true
}

// dispatchWaiting gives waiting occurrences, oldest first, to free target
// agents. The lock must be held.
func (w *scheduledWork) dispatchWaiting(ctx context.Context) {
	waiting, err := w.store.ListRuns(ctx, store.RunFilter{Statuses: []schedule.RunStatus{schedule.RunWaiting}})
	if err != nil {
		w.logger.Error("list waiting scheduled tasks", "err", err)

		return
	}

	slices.Reverse(waiting) // oldest first

	o := w.org.Current()
	taken := map[string]bool{}

	for _, r := range waiting {
		s, ok := w.lookup(r.ScheduleID)
		if !ok {
			w.end(ctx, &r, schedule.RunCanceled, "the schedule was deleted")

			continue
		}

		agent := w.freeAgent(ctx, s.Candidates(o), taken)
		if agent == "" {
			continue
		}

		taken[agent] = true
		w.dispatch(ctx, r, s, agent)
	}
}

// freeAgent returns the first candidate that is idle, not on leave and has
// no ticket in progress; "" if none.
func (w *scheduledWork) freeAgent(ctx context.Context, candidates []org.Member, taken map[string]bool) string {
	for _, m := range candidates {
		if m.Agent.OnLeave || taken[m.Address] || w.statuses.Get(m.Address) != runtime.StatusIdle {
			continue
		}

		inProgress, err := w.tickets.Store.ListTickets(ctx, work.Filter{Assignee: m.Address, Status: work.StatusInProgress})
		if err != nil {
			w.logger.Warn("list an agent's tickets", "agent", m.Address, "err", err)

			continue
		}

		if len(inProgress) == 0 {
			return m.Address
		}
	}

	return ""
}

// dispatch hands an occurrence to an agent: a ticket or mail from nobody.
// The occurrence is marked running first, as the ticket or mail wakes the
// agent up. If handing it over fails, it waits again.
func (w *scheduledWork) dispatch(ctx context.Context, r schedule.Run, s schedule.Schedule, agent string) {
	now := w.clock.Now()
	r.Status, r.StartedAt, r.Agent = schedule.RunRunning, &now, agent

	if err := w.store.UpdateRun(ctx, r); err != nil {
		w.logger.Error("start a scheduled task", "schedule", s.ID, "err", err)

		return
	}

	var err error

	switch r.Mode {
	case schedule.ModeTicket:
		var t work.Ticket

		t, err = w.tickets.Create(ctx, org.SystemAddress, work.Draft{
			Title: s.Title, Description: ticketText(s, r), Team: org.TeamOf(agent), Assignee: agent, NoFeature: true,
		})
		r.Ticket = t.ID
	case schedule.ModeMessage:
		var m messaging.Message

		m, err = w.messages.Send(ctx, messaging.Draft{
			From: org.SystemAddress, To: agent, Act: messaging.ActInform, Priority: messaging.Normal,
			Subject: "Scheduled task: " + s.Title, Body: messageText(s, r),
		})
		r.Message = m.ID
	}

	if err != nil {
		w.logger.Error("hand a scheduled task over, it waits again", "schedule", s.ID, "agent", agent, "err", err)

		r.Status, r.StartedAt, r.Agent = schedule.RunWaiting, nil, ""
	}

	if err := w.store.UpdateRun(ctx, r); err != nil {
		w.logger.Error("record a scheduled task", "schedule", s.ID, "err", err)
	}
}

// end closes an occurrence.
func (w *scheduledWork) end(ctx context.Context, r *schedule.Run, status schedule.RunStatus, note string) {
	now := w.clock.Now()
	r.Status, r.EndedAt = status, &now

	if note != "" {
		r.Note = note
	}

	if err := w.store.UpdateRun(ctx, *r); err != nil {
		w.logger.Error("end a scheduled task", "run", r.ID, "err", err)
	}
}

// onIdle is called when an agent ends its turn, after the supervisor has
// handled it (which may already have woken it up for mail). An occurrence
// in message mode is over once its mail was delivered and the agent is idle
// again; then the agent may take a waiting one.
func (w *scheduledWork) onIdle(ctx context.Context, address string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	runs, err := w.store.ListRuns(ctx, store.RunFilter{Statuses: []schedule.RunStatus{schedule.RunRunning}})
	if err != nil {
		w.logger.Error("list running scheduled tasks", "err", err)

		return
	}

	for _, r := range runs {
		if r.Agent != address || r.Message == "" {
			continue
		}

		m, err := w.store.GetMessage(ctx, r.Message)
		if err != nil || m.DeliveredAt == nil {
			continue // not read yet: this idle comes before the mail
		}

		w.end(ctx, &r, schedule.RunDone, "")
	}

	w.dispatchWaiting(ctx)
	w.changed()
}

// onTicketEvent ends the occurrence of a ticket that is done or canceled,
// and lets waiting occurrences go to the agent it freed.
func (w *scheduledWork) onTicketEvent(ctx context.Context, e work.Event) {
	var status schedule.RunStatus

	switch e.Kind {
	case work.EventDone:
		status = schedule.RunDone
	case work.EventCanceled:
		status = schedule.RunCanceled
	case work.EventCreated, work.EventClaimed, work.EventReleased:
		return // never take the lock here: Create calls this while dispatch holds it
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	runs, err := w.store.ListRuns(ctx, store.RunFilter{Statuses: []schedule.RunStatus{schedule.RunRunning}})
	if err != nil {
		w.logger.Error("list running scheduled tasks", "err", err)

		return
	}

	for _, r := range runs {
		if r.Ticket == e.Ticket.ID {
			note := ""
			if status == schedule.RunCanceled {
				note = "the ticket was canceled by " + e.By
			}

			w.end(ctx, &r, status, note)
		}
	}

	w.dispatchWaiting(ctx)
	w.changed()
}

// cancelWaiting cancels a schedule's occurrence that still waits for an
// agent (the schedule was paused, edited or deleted). One already running
// goes on to its end.
func (w *scheduledWork) cancelWaiting(ctx context.Context, scheduleID, why string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	runs, err := w.store.ListRuns(ctx, store.RunFilter{Schedule: scheduleID, Statuses: []schedule.RunStatus{schedule.RunWaiting}})
	if err != nil {
		w.logger.Error("list waiting scheduled tasks", "schedule", scheduleID, "err", err)

		return
	}

	for _, r := range runs {
		w.end(ctx, &r, schedule.RunCanceled, why)
	}

	delete(w.unavailable, scheduleID)
	w.changed()
}

// reconcile settles, when the daemon starts, the occurrences it left behind:
// one that was waiting, or running in message mode (the agents start a new
// session), is interrupted; one running in ticket mode follows its ticket,
// which survived.
func (w *scheduledWork) reconcile(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	runs, err := w.store.ListRuns(ctx, store.RunFilter{Statuses: activeStatuses})
	if err != nil {
		w.logger.Error("list scheduled tasks left over", "err", err)

		return
	}

	for _, r := range runs {
		if r.Status == schedule.RunRunning && r.Ticket != "" {
			w.refresh(ctx, &r)

			continue
		}

		w.end(ctx, &r, schedule.RunInterrupted, "the daemon stopped")
	}
}

// tellHuman sends the human a notice from nobody.
func (w *scheduledWork) tellHuman(ctx context.Context, subject, body string) {
	_, err := w.messages.Send(ctx, messaging.Draft{
		From: org.SystemAddress, To: org.HumanAddress, Act: messaging.ActInform, Priority: messaging.Normal,
		Subject: subject, Body: body,
	})
	if err != nil {
		w.logger.Error("tell the human about a schedule", "subject", subject, "err", err)
	}
}

// changed tells the control room.
func (w *scheduledWork) changed() {
	if w.bus != nil {
		w.bus.Publish(events.Event{Type: events.SchedulesChanged, Data: events.Empty{}})
	}
}

// recurrenceText describes when a schedule runs.
func recurrenceText(s schedule.Schedule) string {
	if s.Every != "" {
		return "every " + s.Every
	}

	return "cron " + s.Cron
}

func ticketText(s schedule.Schedule, r schedule.Run) string {
	return fmt.Sprintf("%s\n\n---\nScheduled task %s (%s), occurrence due at %s. "+
		"Do the work, report with ticket_comment or deliverable_add, then close this ticket with ticket_update (status done). "+
		"Nobody waits for an answer: write to someone only if the instructions above ask you to.",
		s.Instructions, s.ID, recurrenceText(s), r.DueAt.Local().Format(time.DateTime))
}

func messageText(s schedule.Schedule, r schedule.Run) string {
	return fmt.Sprintf("Scheduled task %q (%s, %s), due at %s:\n\n%s\n\n---\n"+
		"Do it now. This is a notice from nobody: do not reply to it, and write to someone only if the instructions "+
		"above ask you to. The task is over when you end your turn.",
		s.Title, s.ID, recurrenceText(s), r.DueAt.Local().Format(time.DateTime), s.Instructions)
}
