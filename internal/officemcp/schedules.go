package officemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// The director's scheduled-task tools (ADR-0012): it reads the schedules and
// proposes new ones; only the human accepts a proposal, which the code
// enforces: there is no tool to create, edit, pause or delete a schedule.

// scheduleToolNames are the director's scheduled-task tools.
var scheduleToolNames = []string{"schedule_list", "schedule_propose"}

// Schedules is what backs the director's scheduled-task tools (implemented
// in internal/daemon).
type Schedules interface {
	List(ctx context.Context) ([]schedule.View, error)
	Propose(ctx context.Context, by string, s schedule.Schedule) (schedule.Proposal, error)
}

// ScheduleProposeInput is a scheduled task the director proposes.
type ScheduleProposeInput struct {
	ID    string `json:"id"              jsonschema:"the schedule's id: lowercase letters, digits and dashes, e.g. grafana-watch"`
	Title string `json:"title"           jsonschema:"short title, e.g. Watch the production dashboard"`
	Every string `json:"every,omitempty" jsonschema:"an interval of one minute or more, e.g. 10m or 2h; give every or cron"`
	//nolint:lll // a struct tag cannot be split
	Cron   string `json:"cron,omitempty" jsonschema:"a cron expression in the machine's time zone, e.g. 0 9 * * 1-5 (weekdays at 9) or @daily; give every or cron"`
	Target string `json:"target"         jsonschema:"who does it: an agent (monitor@ops), a team (@ops, its first free member) or you"`
	//nolint:lll // a struct tag cannot be split
	Mode string `json:"mode" jsonschema:"ticket (a ticket per occurrence, on the Kanban: for daily or weekly work) or message (mail, nothing on the Kanban: for work every few minutes); you can only be a target in message mode"`
	//nolint:lll // a struct tag cannot be split
	Instructions string `json:"instructions" jsonschema:"what the agent does at each occurrence, and when to write to the human (e.g. only if something is wrong)"`
}

// ScheduleProposeOutput confirms the proposal.
type ScheduleProposeOutput struct {
	ID      string `json:"id"`
	Warning string `json:"warning,omitempty"`
	Note    string `json:"note"`
}

// ScheduleListOutput lists the schedules.
type ScheduleListOutput struct {
	Schedules []schedule.View `json:"schedules"`
}

// addScheduleTools registers the director's scheduled-task tools.
func (t tools) addScheduleTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "schedule_list",
		Description: "List the scheduled tasks: recurring work the daemon hands to agents while it runs, with their " +
			"recurrence, target, mode, whether they are enabled, and their next and last occurrence.",
	}, t.scheduleList)
	mcp.AddTool(server, &mcp.Tool{
		Name: "schedule_propose",
		Description: "Propose a scheduled task to the human: recurring work for an agent (a dashboard to watch, " +
			"a daily report). Nothing runs until they accept it, possibly after editing it: you then get an " +
			"\"agree\" answer, or a \"refuse\" answer with their reason. Each occurrence waits until a target " +
			"agent is free, is signed by nobody, and is skipped while the previous one is not over.",
	}, t.schedulePropose)
}

func (t tools) scheduleList(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (
	*mcp.CallToolResult, ScheduleListOutput, error,
) {
	views, err := t.schedules.List(ctx)

	return nil, ScheduleListOutput{Schedules: views}, err
}

func (t tools) schedulePropose(ctx context.Context, _ *mcp.CallToolRequest, in ScheduleProposeInput) (
	*mcp.CallToolResult, ScheduleProposeOutput, error,
) {
	p, err := t.schedules.Propose(ctx, t.self, schedule.Schedule{
		ID: in.ID, Title: in.Title, Every: in.Every, Cron: in.Cron, Target: in.Target,
		Mode: schedule.Mode(in.Mode), Instructions: in.Instructions,
	})
	if err != nil {
		return nil, ScheduleProposeOutput{}, err
	}

	return nil, ScheduleProposeOutput{
		ID: p.ID, Warning: p.Warning, Note: "proposed to the human; wait for their answer",
	}, nil
}
