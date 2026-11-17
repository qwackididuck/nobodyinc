package officemcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// The director's tools (docs/god.md): following the organization's progress
// and changing its agents. Only the director gets them; the ticket tools are
// shared, the director being privileged in the ticket service.

// directorToolNames are the tools only the director gets.
var directorToolNames = []string{
	"feature_propose", "org_overview", "template_list", "team_create", "team_delete", "team_block",
	"agent_get", "agent_recruit", "agent_create", "agent_edit", "agent_leave", "agent_return", "agent_dismiss",
}

// errSelf refuses a director changing its own definition: like every agent,
// it must not widen its own permissions (ADR-0004). The human does it.
var errSelf = errors.New("you cannot change your own definition, leave or dismiss yourself: ask the human")

// Director is what backs the director's tools (implemented in
// internal/daemon on top of the staffing service). Changes are written by
// the daemon, never by the director itself (ADR-0004).
type Director interface {
	TeamDirector
	Overview(ctx context.Context) (Overview, error)
	Templates() ([]catalog.Template, error)
	Definition(address string) (catalog.Definition, error)
	Recruit(ctx context.Context, team, name, template string) (org.Member, error)
	Create(ctx context.Context, team, name string, d catalog.Definition) (org.Member, error)
	Edit(ctx context.Context, address string, d catalog.Definition, fresh bool) (catalog.Definition, error)
	Leave(ctx context.Context, address string) error
	Return(ctx context.Context, address string) error
	Dismiss(ctx context.Context, address string) error
}

// TeamDirector is the part of Director that creates, deletes and restricts
// teams.
type TeamDirector interface {
	CreateTeam(ctx context.Context, id, goal, prompt string) (org.Team, error)
	DeleteTeam(ctx context.Context, id string) error
	// BlockTeam replaces the teams team id may not start a conversation with
	// (ADR-0009).
	BlockTeam(ctx context.Context, id string, block []string) (org.Team, error)
}

// Overview is the state of the organization, for the director.
type Overview struct {
	// ActiveFeature is the feature the teams work on; absent without one.
	ActiveFeature *FeatureSummary `json:"active_feature,omitempty"`
	Teams         []TeamOverview  `json:"teams"`
}

// FeatureSummary is a feature in the overview.
type FeatureSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Branch string `json:"branch"`
}

// TeamOverview is one team: its members and its tickets.
type TeamOverview struct {
	Team       string          `json:"team"`
	Goal       string          `json:"goal,omitempty"`
	Prompt     string          `json:"prompt,omitempty"`
	Agents     []AgentState    `json:"agents"`
	Open       []TicketSummary `json:"open"`
	InProgress []TicketSummary `json:"in_progress"`
	Done       int             `json:"done"`
	Canceled   int             `json:"canceled"`
}

// AgentState is an agent in the overview.
type AgentState struct {
	Address   string `json:"address"`
	Role      string `json:"role,omitempty"`
	Expertise string `json:"expertise,omitempty"`
	// Status: stopped, starting, working, idle, exited or awaiting_approval.
	Status  string `json:"status"`
	OnLeave bool   `json:"on_leave,omitempty"`
}

// TicketSummary is a ticket in the overview.
type TicketSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Assignee  string   `json:"assignee,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

// FeatureProposeInput is a feature the director proposes.
type FeatureProposeInput struct {
	Title              string `json:"title"                 jsonschema:"short title, e.g. Add the order API"`
	Need               string `json:"need"                  jsonschema:"what is wanted and why, as agreed with the human"`
	AcceptanceCriteria string `json:"acceptance_criteria"   jsonschema:"how to know the feature is done, testable"`
	BaseBranch         string `json:"base_branch,omitempty" jsonschema:"branch to start from; omit for the project's current branch"`
}

// FeatureProposeOutput confirms the proposal.
type FeatureProposeOutput struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

// TeamCreateInput declares a new team.
type TeamCreateInput struct {
	ID   string `json:"id"   jsonschema:"the team's id, used in addresses (agent@id): lowercase letters, digits and dashes"`
	Goal string `json:"goal" jsonschema:"what the team is for, in one sentence"`
	//nolint:lll // a struct tag cannot be split
	Prompt string `json:"prompt,omitempty" jsonschema:"instructions every member shares, written from the goal: how the team works, its conventions; omit for the goal alone"`
}

// TeamBlockInput is a team's new block list.
type TeamBlockInput struct {
	ID string `json:"id" jsonschema:"the team whose members are restricted"`
	//nolint:lll // a struct tag cannot be split
	Block []string `json:"block" jsonschema:"the whole new list of teams its members may not start a conversation with; empty opens every flow again"`
}

// TeamBlockOutput is the team after the change.
type TeamBlockOutput struct {
	Team  string   `json:"team"`
	Block []string `json:"block"`
	Note  string   `json:"note"`
}

// TeamDeleteInput names the team to delete.
type TeamDeleteInput struct {
	ID string `json:"id" jsonschema:"the team's id"`
}

// TeamCreateOutput confirms the new team.
type TeamCreateOutput struct {
	Team string `json:"team"`
	Note string `json:"note"`
}

// AddressInput names an agent.
type AddressInput struct {
	Address string `json:"address" jsonschema:"the agent's address, e.g. dev1@dev"`
}

// RecruitInput recruits an agent from a template.
type RecruitInput struct {
	Team     string `json:"team"     jsonschema:"team to recruit into, e.g. dev"`
	Name     string `json:"name"     jsonschema:"the new agent's name: lowercase letters, digits and dashes"`
	Template string `json:"template" jsonschema:"template from template_list"`
}

// CreateInput creates an agent from a definition written from scratch.
type CreateInput struct {
	Team       string             `json:"team"       jsonschema:"team to add the agent to"`
	Name       string             `json:"name"       jsonschema:"the new agent's name: lowercase letters, digits and dashes"`
	Definition catalog.Definition `json:"definition" jsonschema:"role, model, permissions, tools, mcp and prompt (its instructions)"`
}

// EditInput replaces an agent's definition.
type EditInput struct {
	Address    string             `json:"address"         jsonschema:"the agent's address"`
	Definition catalog.Definition `json:"definition"      jsonschema:"the whole new definition: start from agent_get, change what you need"`
	Fresh      bool               `json:"fresh,omitempty" jsonschema:"restart with a new session even if role and instructions are unchanged"`
}

// TemplateListOutput lists the templates.
type TemplateListOutput struct {
	Templates []catalog.Template `json:"templates"`
}

// MemberOutput is an agent after a change.
type MemberOutput struct {
	Address string `json:"address"`
	Note    string `json:"note"`
}

// DoneOutput confirms a change that is applied once the agent is idle.
type DoneOutput struct {
	Note string `json:"note"`
}

// addDirectorTools registers the director's tools.
func (t tools) addDirectorTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_propose",
		Description: "Propose a feature to the human: a title, the need and the acceptance criteria. Nothing starts " +
			"until they agree: you then get an \"agree\" answer (split it into tickets for the teams), or a \"refuse\" " +
			"answer with their reason.",
	}, t.featurePropose)
	mcp.AddTool(server, &mcp.Tool{
		Name: "org_overview",
		Description: "The state of the organization: the active feature, and for each team its agents (status, on leave) " +
			"and its tickets (open, in progress, and how many are done or canceled).",
	}, t.orgOverview)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_list",
		Description: "List the agent templates you can recruit from.",
	}, t.templateList)
	mcp.AddTool(server, &mcp.Tool{
		Name: "team_create",
		Description: "Create a team: a new room of the office, with its goal and the instructions its members share. " +
			"It has no agent yet: recruit some with agent_recruit. When this is your own idea, get the human's agreement first.",
	}, t.teamCreate)
	mcp.AddTool(server, &mcp.Tool{
		Name: "team_delete",
		Description: "Delete a team: its room goes, and every member is dismissed (each stops once its turn is over; " +
			"its ticket in progress goes back to open, its undelivered mail is returned). The team's tickets stay. " +
			"Only with the human's explicit agreement: ask them first.",
	}, t.teamDelete)
	mcp.AddTool(server, &mcp.Tool{
		Name: "team_block",
		Description: "Replace a team's block list: the teams its members may not start a conversation with (flows are " +
			"open by default). A block is one-way and never stops an answer, a ticket, or a message to or from the human " +
			"or you. Nobody restarts: the members of the teams concerned get a notice. " +
			"Only with the human's explicit agreement: ask them first.",
	}, t.teamBlock)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "agent_get",
		Description: "Read an agent's definition: role, model, permissions, tools, MCP servers and instructions (prompt).",
	}, t.agentGet)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "agent_recruit",
		Description: "Recruit an agent from a template into a team. It starts at once.",
	}, t.agentRecruit)
	mcp.AddTool(server, &mcp.Tool{
		Name: "agent_create",
		Description: "Create an agent from a definition written from scratch, in a team. It starts at once. " +
			"When this is your own idea, show the whole definition to the human and get their agreement first.",
	}, t.agentCreate)
	mcp.AddTool(server, &mcp.Tool{
		Name: "agent_edit",
		Description: "Replace an agent's definition. It restarts once its current turn is over: with a new session " +
			"(forgetting the conversation) if its role or instructions change, otherwise keeping its memory.",
	}, t.agentEdit)
	mcp.AddTool(server, &mcp.Tool{
		Name: "agent_leave",
		Description: "Put an agent on leave: it stops once its turn is over, its ticket in progress goes back to its team's " +
			"queue, and its mail waits until it returns.",
	}, t.agentLeave)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "agent_return",
		Description: "Bring an agent back from leave; it reads the mail that waited.",
	}, t.agentReturn)
	mcp.AddTool(server, &mcp.Tool{
		Name: "agent_dismiss",
		Description: "Dismiss an agent: it stops once its turn is over, its ticket goes back to its team's queue, " +
			"its undelivered mail is returned to the senders.",
	}, t.agentDismiss)
}

func (t tools) featurePropose(ctx context.Context, _ *mcp.CallToolRequest, in FeatureProposeInput) (
	*mcp.CallToolResult, FeatureProposeOutput, error,
) {
	f, err := t.tickets.ProposeFeature(ctx, t.self, work.FeatureDraft{
		Title: in.Title, Description: in.Need, AcceptanceCriteria: in.AcceptanceCriteria, BaseBranch: in.BaseBranch,
	})
	if err != nil {
		return nil, FeatureProposeOutput{}, err
	}

	return nil, FeatureProposeOutput{ID: f.ID, Note: "proposed to the human; wait for their answer"}, nil
}

func (t tools) orgOverview(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, Overview, error) {
	overview, err := t.director.Overview(ctx)

	return nil, overview, err
}

func (t tools) templateList(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, TemplateListOutput, error) {
	templates, err := t.director.Templates()

	return nil, TemplateListOutput{Templates: append([]catalog.Template{}, templates...)}, err
}

func (t tools) teamCreate(ctx context.Context, _ *mcp.CallToolRequest, in TeamCreateInput) (
	*mcp.CallToolResult, TeamCreateOutput, error,
) {
	team, err := t.director.CreateTeam(ctx, in.ID, in.Goal, in.Prompt)
	if err != nil {
		return nil, TeamCreateOutput{}, err
	}

	return nil, TeamCreateOutput{Team: team.ID, Note: "created, without agents: recruit into it with agent_recruit"}, nil
}

func (t tools) teamDelete(ctx context.Context, _ *mcp.CallToolRequest, in TeamDeleteInput) (*mcp.CallToolResult, DoneOutput, error) {
	if err := t.director.DeleteTeam(ctx, in.ID); err != nil {
		return nil, DoneOutput{}, err
	}

	return nil, DoneOutput{Note: "team " + in.ID + " deleted; its members stop once their turn is over"}, nil
}

func (t tools) teamBlock(ctx context.Context, _ *mcp.CallToolRequest, in TeamBlockInput) (
	*mcp.CallToolResult, TeamBlockOutput, error,
) {
	team, err := t.director.BlockTeam(ctx, in.ID, in.Block)
	if err != nil {
		return nil, TeamBlockOutput{}, err
	}

	note := "team " + team.ID + " may start conversations with every team"
	if len(team.Block) > 0 {
		note = "team " + team.ID + " may no longer start conversations with: " + strings.Join(team.Block, ", ")
	}

	return nil, TeamBlockOutput{Team: team.ID, Block: append([]string{}, team.Block...), Note: note}, nil
}

func (t tools) agentGet(_ context.Context, _ *mcp.CallToolRequest, in AddressInput) (*mcp.CallToolResult, catalog.Definition, error) {
	d, err := t.director.Definition(in.Address)

	return nil, d, err
}

func (t tools) agentRecruit(ctx context.Context, _ *mcp.CallToolRequest, in RecruitInput) (*mcp.CallToolResult, MemberOutput, error) {
	m, err := t.director.Recruit(ctx, in.Team, in.Name, in.Template)
	if err != nil {
		return nil, MemberOutput{}, err
	}

	return nil, MemberOutput{Address: m.Address, Note: "recruited from " + in.Template + "; it is starting"}, nil
}

func (t tools) agentCreate(ctx context.Context, _ *mcp.CallToolRequest, in CreateInput) (*mcp.CallToolResult, MemberOutput, error) {
	m, err := t.director.Create(ctx, in.Team, in.Name, in.Definition)
	if err != nil {
		return nil, MemberOutput{}, err
	}

	return nil, MemberOutput{Address: m.Address, Note: "created; it is starting"}, nil
}

func (t tools) agentEdit(ctx context.Context, _ *mcp.CallToolRequest, in EditInput) (*mcp.CallToolResult, DoneOutput, error) {
	if in.Address == t.self {
		return nil, DoneOutput{}, errSelf
	}

	if _, err := t.director.Edit(ctx, in.Address, in.Definition, in.Fresh); err != nil {
		return nil, DoneOutput{}, err
	}

	return nil, DoneOutput{Note: in.Address + " restarts with its new definition once its current turn is over"}, nil
}

func (t tools) agentLeave(ctx context.Context, _ *mcp.CallToolRequest, in AddressInput) (*mcp.CallToolResult, DoneOutput, error) {
	return t.change(ctx, in.Address, t.director.Leave, "goes on leave once its current turn is over")
}

func (t tools) agentReturn(ctx context.Context, _ *mcp.CallToolRequest, in AddressInput) (*mcp.CallToolResult, DoneOutput, error) {
	return t.change(ctx, in.Address, t.director.Return, "is back and starting")
}

func (t tools) agentDismiss(ctx context.Context, _ *mcp.CallToolRequest, in AddressInput) (*mcp.CallToolResult, DoneOutput, error) {
	return t.change(ctx, in.Address, t.director.Dismiss, "is dismissed and stops once its current turn is over")
}

// change runs a change that only names the agent, never on the director
// itself.
func (t tools) change(ctx context.Context, address string, do func(context.Context, string) error, done string) (
	*mcp.CallToolResult, DoneOutput, error,
) {
	if address == t.self {
		return nil, DoneOutput{}, errSelf
	}

	if err := do(ctx, address); err != nil {
		return nil, DoneOutput{}, fmt.Errorf("%s: %w", address, err)
	}

	return nil, DoneOutput{Note: address + " " + done}, nil
}
