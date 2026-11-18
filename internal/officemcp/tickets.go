package officemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/work"
)

// Tickets is the ticket service as seen by agents (implemented by work.Service).
type Tickets interface {
	Create(ctx context.Context, by string, d work.Draft) (work.Ticket, error)
	Claim(ctx context.Context, agent, id string) (work.Ticket, error)
	SetStatus(ctx context.Context, by, id string, status work.Status) (work.Ticket, error)
	Comment(ctx context.Context, by, id, body string) (work.Comment, error)
	AddDeliverable(ctx context.Context, by, id string, d work.Deliverable) (work.Deliverable, error)
	Get(ctx context.Context, id string) (work.Details, error)
	// Mine returns the agent's tickets in progress, then its team's queue.
	Mine(ctx context.Context, agent string) ([]work.Ticket, []work.Ticket, error)
	// ProposeFeature records a feature the director proposes to the human.
	ProposeFeature(ctx context.Context, by string, d work.FeatureDraft) (work.Feature, error)
}

// addTicketTools registers the ticket tools of an agent's office.
func (t tools) addTicketTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_list",
		Description: "List the tickets assigned to you and the tickets waiting in your team's queue.",
	}, t.ticketList)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_get",
		Description: "Read a ticket with its description, acceptance criteria, deliverables and comments.",
	}, t.ticketGet)
	mcp.AddTool(server, &mcp.Tool{
		Name: "ticket_claim",
		Description: "Take a ticket from your team's queue and start working on it. " +
			"Without an id, takes the oldest available ticket.",
	}, t.ticketClaim)
	mcp.AddTool(server, &mcp.Tool{
		Name: "ticket_create",
		Description: "Create a ticket for any team, your own included (a team your team blocks too). " +
			"Without an assignee it waits in that team's queue.",
	}, t.ticketCreate)
	mcp.AddTool(server, &mcp.Tool{
		Name: "ticket_update",
		Description: "Change the status of a ticket assigned to you: done when finished, " +
			"open to give it back to your team's queue, canceled if it is no longer needed.",
	}, t.ticketUpdate)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ticket_comment",
		Description: "Add a comment to a ticket: progress, a question, a decision.",
	}, t.ticketComment)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "deliverable_add",
		Description: "Attach what you produced for a ticket: a file, a commit, a report or a note.",
	}, t.deliverableAdd)
}

// TicketListOutput is what ticket_list returns.
type TicketListOutput struct {
	Assigned []work.Ticket `json:"assigned"`
	Queue    []work.Ticket `json:"queue"`
}

func (t tools) ticketList(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, TicketListOutput, error) {
	assigned, queue, err := t.tickets.Mine(ctx, t.self)

	return nil, TicketListOutput{Assigned: nonNil(assigned), Queue: nonNil(queue)}, err
}

// TicketIDInput identifies a ticket.
type TicketIDInput struct {
	ID string `json:"id" jsonschema:"ticket ID, e.g. T-12"`
}

func (t tools) ticketGet(ctx context.Context, _ *mcp.CallToolRequest, in TicketIDInput) (*mcp.CallToolResult, work.Details, error) {
	d, err := t.tickets.Get(ctx, in.ID)

	return nil, d, err
}

// TicketClaimInput optionally names the ticket to claim.
type TicketClaimInput struct {
	ID string `json:"id,omitempty" jsonschema:"ticket ID to claim; omit to take the oldest available ticket of your team"`
}

func (t tools) ticketClaim(ctx context.Context, _ *mcp.CallToolRequest, in TicketClaimInput) (*mcp.CallToolResult, work.Ticket, error) {
	tk, err := t.tickets.Claim(ctx, t.self, in.ID)

	return nil, tk, err
}

// TicketCreateInput describes a new ticket.
type TicketCreateInput struct {
	Title              string   `json:"title"                         jsonschema:"short title"`
	Team               string   `json:"team"                          jsonschema:"team that owns the ticket, e.g. dev"`
	Description        string   `json:"description,omitempty"         jsonschema:"what has to be done and why"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty" jsonschema:"how to know it is done"`
	BlockedBy          []string `json:"blocked_by,omitempty"          jsonschema:"IDs of tickets that must be done first"`
	Assignee           string   `json:"assignee,omitempty"            jsonschema:"address of a team member to assign it to; omit to queue it"`
	BudgetUSD          *float64 `json:"budget_usd,omitempty"          jsonschema:"USD; required in a feature with a budget"`
}

func (t tools) ticketCreate(ctx context.Context, _ *mcp.CallToolRequest, in TicketCreateInput) (*mcp.CallToolResult, work.Ticket, error) {
	tk, err := t.tickets.Create(ctx, t.self, work.Draft{
		Title: in.Title, Team: in.Team, Description: in.Description,
		AcceptanceCriteria: in.AcceptanceCriteria, BlockedBy: in.BlockedBy, Assignee: in.Assignee,
		Budget: work.Budget{USD: in.BudgetUSD},
	})

	return nil, tk, err
}

// TicketUpdateInput changes a ticket's status.
type TicketUpdateInput struct {
	ID      string `json:"id"                jsonschema:"ticket ID"`
	Status  string `json:"status"            jsonschema:"done, open (give it back to the queue) or canceled"`
	Comment string `json:"comment,omitempty" jsonschema:"optional comment explaining the change"`
}

func (t tools) ticketUpdate(ctx context.Context, _ *mcp.CallToolRequest, in TicketUpdateInput) (*mcp.CallToolResult, work.Ticket, error) {
	if in.Comment != "" {
		if _, err := t.tickets.Comment(ctx, t.self, in.ID, in.Comment); err != nil {
			return nil, work.Ticket{}, err
		}
	}

	tk, err := t.tickets.SetStatus(ctx, t.self, in.ID, work.Status(in.Status))

	return nil, tk, err
}

// TicketCommentInput is a comment on a ticket.
type TicketCommentInput struct {
	ID   string `json:"id"   jsonschema:"ticket ID"`
	Body string `json:"body" jsonschema:"the comment"`
}

func (t tools) ticketComment(
	ctx context.Context, _ *mcp.CallToolRequest, in TicketCommentInput,
) (*mcp.CallToolResult, work.Comment, error) {
	c, err := t.tickets.Comment(ctx, t.self, in.ID, in.Body)

	return nil, c, err
}

// DeliverableAddInput describes something produced for a ticket.
type DeliverableAddInput struct {
	ID        string `json:"id"                  jsonschema:"ticket ID"`
	Kind      string `json:"kind"                jsonschema:"file, commit, report or note"`
	Reference string `json:"reference,omitempty" jsonschema:"file path, commit hash or URL"`
	Summary   string `json:"summary"             jsonschema:"what it is, in one or two sentences"`
}

func (t tools) deliverableAdd(
	ctx context.Context, _ *mcp.CallToolRequest, in DeliverableAddInput,
) (*mcp.CallToolResult, work.Deliverable, error) {
	d, err := t.tickets.AddDeliverable(ctx, t.self, in.ID, work.Deliverable{Kind: in.Kind, Reference: in.Reference, Summary: in.Summary})

	return nil, d, err
}

func nonNil(list []work.Ticket) []work.Ticket {
	if list == nil {
		return []work.Ticket{}
	}

	return list
}
