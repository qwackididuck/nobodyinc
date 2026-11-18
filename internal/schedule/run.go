package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// View is a schedule as the control room shows it.
type View struct {
	Schedule

	// NextRun is the next occurrence; nil when the schedule is not enabled.
	NextRun *time.Time `json:"next_run,omitempty"`
	LastRun *Run       `json:"last_run,omitempty"`
	// Warning is Schedule.Warning.
	Warning string `json:"warning,omitempty"`
}

// Run is one occurrence of a schedule, kept in the local database even after
// its schedule is deleted (ADR-0012, point 8).
type Run struct {
	ID            int64     `json:"id"`
	ScheduleID    string    `json:"schedule_id"`
	ScheduleTitle string    `json:"schedule_title"`
	Trigger       Trigger   `json:"trigger"`
	Status        RunStatus `json:"status"`
	// Mode is how the occurrence reaches its agent, copied from the schedule
	// when it fell due: it says how the run ends.
	Mode      Mode       `json:"-"`
	DueAt     time.Time  `json:"due_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Agent is the agent the occurrence went to, and Ticket or Message what
	// it was given.
	Agent   string `json:"agent,omitempty"`
	Ticket  string `json:"ticket,omitempty"`
	Message string `json:"message,omitempty"`
	// Note says why it was skipped, canceled, unavailable or interrupted.
	Note string `json:"note,omitempty"`
	// SpentUSD is what its agent spent while running it (ADR-0013);
	// read-only, computed by the store.
	SpentUSD float64 `json:"spent_usd"`
}

// Trigger says why an occurrence happened.
type Trigger string

// Triggers.
const (
	TriggerSchedule Trigger = "schedule" // the recurrence
	TriggerManual   Trigger = "manual"   // "Run now"
)

// RunStatus is where an occurrence stands.
type RunStatus string

// The life of an occurrence: waiting → running → done or canceled; or, at
// once, skipped (the previous one is not over) or unavailable (no target
// agent runs); interrupted when the daemon stopped before its end.
const (
	RunWaiting     RunStatus = "waiting"
	RunRunning     RunStatus = "running"
	RunDone        RunStatus = "done"
	RunCanceled    RunStatus = "canceled"
	RunSkipped     RunStatus = "skipped"
	RunUnavailable RunStatus = "unavailable"
	RunInterrupted RunStatus = "interrupted"
)

// Active reports whether the occurrence is not over yet.
func (s RunStatus) Active() bool {
	return s == RunWaiting || s == RunRunning
}

// Proposal is a schedule the director proposed; only the human accepts it,
// possibly after editing it, which writes it to schedules.yaml (ADR-0012,
// point 9).
type Proposal struct {
	ID         string         `json:"id"` // "SP-2"
	Schedule   Schedule       `json:"schedule"`
	Status     ProposalStatus `json:"status"`
	ProposedBy string         `json:"proposed_by"`
	// ProposalMessage is the "propose" message sent to the human; the
	// decision answers it.
	ProposalMessage string     `json:"proposal_message,omitempty"`
	RefusalReason   string     `json:"refusal_reason,omitempty"`
	Warning         string     `json:"warning,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
}

// ProposalStatus is where a proposal stands.
type ProposalStatus string

// Proposal statuses.
const (
	ProposalPending  ProposalStatus = "proposed"
	ProposalAccepted ProposalStatus = "accepted"
	ProposalRefused  ProposalStatus = "refused"
)

const proposalPrefix = "SP-"

// FormatProposalID turns a database id into "SP-<n>".
func FormatProposalID(n int64) string {
	return proposalPrefix + strconv.FormatInt(n, 10)
}

// ParseProposalID turns "SP-<n>" into its database id.
func ParseProposalID(id string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, proposalPrefix), 10, 64)
	if err != nil || !strings.HasPrefix(id, proposalPrefix) || n <= 0 {
		return 0, fmt.Errorf("%w: proposal %q (expected SP-<number>)", ErrNotFound, id)
	}

	return n, nil
}
