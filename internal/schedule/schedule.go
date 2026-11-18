// Package schedule describes the project's scheduled tasks and decides when
// they fall due (ADR-0012, docs/schedules.md).
//
// A schedule is recurring work handed to an agent: "every 10 minutes, look at
// the dashboard", "every weekday at 9, write the report". Schedules are
// declared in the project's .nobody/schedules.yaml (file.go), committed with
// it, and written by the daemon only.
//
// This package knows the schedules and their recurrence (recurrence.go), and
// runs the clock: the Scheduler (scheduler.go) calls a Dispatcher each time a
// schedule falls due, while the daemon runs. It never catches up on
// occurrences missed while the daemon was stopped. What an occurrence does
// (a ticket or mail for a free agent, its history in the database) is the
// daemon's business (internal/daemon/schedules.go); this package only names
// it (run.go).
package schedule

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Errors returned when a schedule is refused.
var (
	ErrInvalid  = errors.New("invalid schedule")
	ErrNotFound = errors.New("schedule not found")
	ErrExists   = errors.New("a schedule with this id already exists")
	// ErrBusy refuses "Run now" while an occurrence is waiting or running.
	ErrBusy = errors.New("an occurrence of this schedule is not over yet")
	// ErrDecided refuses deciding a proposal twice.
	ErrDecided = errors.New("this proposal is already decided")
)

// Mode says how an occurrence reaches the agent.
type Mode string

// The two modes, chosen per schedule depending on its frequency.
const (
	// ModeTicket creates a ticket assigned to the agent, outside any feature:
	// traceable, shown on the Kanban; for daily or weekly work.
	ModeTicket Mode = "ticket"
	// ModeMessage sends the instructions as mail: nothing on the Kanban; for
	// work every few minutes.
	ModeMessage Mode = "message"
)

// MinInterval is the shortest "every".
const MinInterval = time.Minute

// TeamPrefix marks a team target: "@ops" is the first free member of ops.
const TeamPrefix = "@"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Schedule is one scheduled task. Exactly one of Every and Cron is set.
type Schedule struct {
	ID    string `json:"id"    yaml:"id"`
	Title string `json:"title" yaml:"title"`
	// Every is a Go duration ("10m"), counted from when the schedule is
	// armed: the daemon starts, or the schedule is created, resumed or
	// edited.
	Every string `json:"every,omitempty" yaml:"every,omitempty"`
	// Cron is a standard five-field expression or a descriptor ("@daily"),
	// read in the machine's time zone.
	Cron string `json:"cron,omitempty" yaml:"cron,omitempty"`
	// Target is an agent address, a team ("@ops") or the director's name.
	Target string `json:"target" yaml:"target"`
	Mode   Mode   `json:"mode"   yaml:"mode"`
	// Enabled is false when the human paused the schedule, or the daemon
	// disabled it because its target is gone.
	Enabled      bool   `json:"enabled"      yaml:"enabled"`
	Instructions string `json:"instructions" yaml:"instructions"`
}

// Check validates a schedule on its own: id, title, recurrence, mode and the
// form of its target. Whether the target exists is checked by CheckTarget.
func (s Schedule) Check() error {
	var errs []error

	if !idPattern.MatchString(s.ID) {
		errs = append(errs, fmt.Errorf("id %q: use lowercase letters, digits and dashes (at most 64)", s.ID))
	}

	if strings.TrimSpace(s.Title) == "" {
		errs = append(errs, errors.New("a schedule needs a title"))
	}

	if strings.TrimSpace(s.Instructions) == "" {
		errs = append(errs, errors.New("a schedule needs instructions: what the agent should do"))
	}

	if s.Mode != ModeTicket && s.Mode != ModeMessage {
		errs = append(errs, fmt.Errorf("mode %q: use %q or %q", s.Mode, ModeTicket, ModeMessage))
	}

	if strings.TrimSpace(s.Target) == "" || s.Target == TeamPrefix {
		errs = append(errs, errors.New("a schedule needs a target: an agent, a team (@team) or the director"))
	}

	if _, err := s.recurrence(time.Local); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w %q: %w", ErrInvalid, s.ID, errors.Join(errs...))
	}

	return nil
}

// CheckTarget validates the target against the organization: the agent, the
// team or the director exists, a team has members, and the director (who has
// no team, so no tickets) is targeted in message mode only.
func (s Schedule) CheckTarget(o *org.Org) error {
	if len(s.Candidates(o)) > 0 {
		return nil
	}

	var why string

	switch {
	case o.IsGod(s.Target):
		why = "the director has no team, so no tickets: use the message mode"
	case strings.HasPrefix(s.Target, TeamPrefix):
		if _, ok := o.Team(strings.TrimPrefix(s.Target, TeamPrefix)); ok {
			why = "the team has no members"
		} else {
			why = "no such team"
		}
	default:
		why = "no such agent"
	}

	return fmt.Errorf("%w %q: target %q: %s", ErrInvalid, s.ID, s.Target, why)
}

// Candidates are the agents that may take the schedule's work, on leave or
// not, in the organization's order. None means the target is gone: the agent
// was dismissed, the team deleted or emptied, or the director is targeted in
// ticket mode.
func (s Schedule) Candidates(o *org.Org) []org.Member {
	if team, ok := strings.CutPrefix(s.Target, TeamPrefix); ok {
		var members []org.Member

		for _, m := range o.Members() {
			if m.Team.ID == team && team != "" {
				members = append(members, m)
			}
		}

		return members
	}

	m, ok := o.Member(s.Target)
	if !ok || (o.IsGod(m.Address) && s.Mode == ModeTicket) {
		return nil
	}

	return []org.Member{m}
}

// Warning returns what the human should know before relying on the
// schedule, or "": a target using the default permission profile sends every
// sensitive action to the approval queue, where the occurrence will wait.
func (s Schedule) Warning(o *org.Org) string {
	for _, m := range s.Candidates(o) {
		if m.Agent.Permissions.EffectiveProfile() == catalog.ProfileDefault {
			return fmt.Sprintf("%s uses the %q permission profile: every sensitive action of a scheduled run "+
				"waits for your approval, also at night.", m.Address, catalog.ProfileDefault)
		}
	}

	return ""
}

// Find returns the schedule with this id.
func Find(schedules []Schedule, id string) (Schedule, bool) {
	for _, s := range schedules {
		if s.ID == id {
			return s, true
		}
	}

	return Schedule{}, false
}
