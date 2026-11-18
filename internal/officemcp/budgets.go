package officemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/budget"
)

// The budget tools (ADR-0013). A team agent held by a budget justifies an
// extension with budget_request; the director reads the budgets, passes
// requests on to the human with its opinion, and proposes caps. Only the
// human extends, refuses or accepts, which the code enforces: no tool
// changes a budget.

// budgetAgentToolNames are the team agents' budget tools; budgetDirectorToolNames
// the director's.
var (
	budgetAgentToolNames    = []string{"budget_request"}
	budgetDirectorToolNames = []string{"budget_list", "budget_submit", "budget_propose"}
)

// Budgets is what backs the budget tools (implemented in internal/daemon).
type Budgets interface {
	RequestExtension(ctx context.Context, agent, id string, usd float64, justification string) (budget.Request, error)
	Submit(ctx context.Context, director, id string, s budget.Submission) (budget.Request, error)
	Overview(ctx context.Context) (budget.Overview, error)
	Propose(ctx context.Context, director string, p budget.Proposal) (budget.Proposal, error)
}

// BudgetRequestInput is an agent's justification for an extension.
type BudgetRequestInput struct {
	Request       string  `json:"request,omitempty" jsonschema:"the request ID given when your turn was stopped, e.g. BR-4"`
	AmountUSD     float64 `json:"amount_usd"        jsonschema:"how many more USD (list price) you need to finish"`
	Justification string  `json:"justification"     jsonschema:"where you are, what remains, why it costs more than planned"`
}

// BudgetSubmitInput is the director passing a request on to the human.
type BudgetSubmitInput struct {
	Request        string   `json:"request"                   jsonschema:"the request ID, e.g. BR-4"`
	Opinion        string   `json:"opinion"                   jsonschema:"your opinion for the human: extend or not, and why"`
	RecommendedUSD *float64 `json:"recommended_usd,omitempty" jsonschema:"the extension you recommend, in USD"`
	//nolint:lll // a struct tag cannot be split
	Justification string   `json:"justification,omitempty" jsonschema:"required when nobody justified it (staff or feature budget, or an agent that stopped without answering)"`
	AmountUSD     *float64 `json:"amount_usd,omitempty"    jsonschema:"the amount to ask for, required with your justification"`
}

// BudgetProposeInput is a cap the director proposes.
type BudgetProposeInput struct {
	Kind      string  `json:"kind"                jsonschema:"agent, team or staff"`
	ID        string  `json:"id,omitempty"        jsonschema:"the agent's address or the team's id; omit for the staff"`
	LimitUSD  float64 `json:"limit_usd"           jsonschema:"the cap, in USD (list price)"`
	Monthly   bool    `json:"monthly,omitempty"   jsonschema:"true: it starts again from 0 every month; false: cumulative"`
	Threshold int     `json:"threshold,omitempty" jsonschema:"percent at which its agents stop; omit for the project's"`
	Reason    string  `json:"reason"              jsonschema:"why you propose it"`
}

// BudgetAnswer confirms a budget tool's action.
type BudgetAnswer struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

// addBudgetAgentTools registers budget_request.
func (t tools) addBudgetAgentTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "budget_request",
		Description: "When a budget stopped your turn, ask for an extension: the amount you need to finish and a short " +
			"justification. It goes to the director, then to the human, who decides; you stay on hold, your conversation kept.",
	}, t.budgetRequest)
}

// addBudgetDirectorTools registers the director's budget tools.
func (t tools) addBudgetDirectorTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "budget_list",
		Description: "The budgets: settings and caps, this month's spend per agent, team, feature and schedule, where every " +
			"budget stands, and the extension requests still open. Amounts are Claude Code's estimate at list price.",
	}, t.budgetList)
	mcp.AddTool(server, &mcp.Tool{
		Name: "budget_submit",
		Description: "Pass a budget extension request on to the human, with your opinion and the amount you recommend. " +
			"For a staff or feature budget, or a request no agent justified, write the justification and the amount to ask for " +
			"yourself. You grant nothing: the human extends or refuses, and you get their answer.",
	}, t.budgetSubmit)
	mcp.AddTool(server, &mcp.Tool{
		Name: "budget_propose",
		Description: "Propose a cap on an agent, a team or the whole staff. Nothing changes until the human accepts it, " +
			"possibly edited: you then get an \"agree\" answer, or a \"refuse\" answer with their reason.",
	}, t.budgetPropose)
}

func (t tools) budgetRequest(ctx context.Context, _ *mcp.CallToolRequest, in BudgetRequestInput) (
	*mcp.CallToolResult, BudgetAnswer, error,
) {
	r, err := t.budgets.RequestExtension(ctx, t.self, in.Request, in.AmountUSD, in.Justification)
	if err != nil {
		return nil, BudgetAnswer{}, err
	}

	return nil, BudgetAnswer{ID: r.ID, Note: "sent to the director; end your turn now and wait for the human's decision"}, nil
}

func (t tools) budgetList(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, budget.Overview, error) {
	overview, err := t.budgets.Overview(ctx)

	return nil, overview, err
}

func (t tools) budgetSubmit(ctx context.Context, _ *mcp.CallToolRequest, in BudgetSubmitInput) (
	*mcp.CallToolResult, BudgetAnswer, error,
) {
	r, err := t.budgets.Submit(ctx, t.self, in.Request, budget.Submission{
		Opinion: in.Opinion, RecommendedUSD: in.RecommendedUSD, Justification: in.Justification, AmountUSD: in.AmountUSD,
	})
	if err != nil {
		return nil, BudgetAnswer{}, err
	}

	return nil, BudgetAnswer{ID: r.ID, Note: "passed on to the human; wait for their answer"}, nil
}

func (t tools) budgetPropose(ctx context.Context, _ *mcp.CallToolRequest, in BudgetProposeInput) (
	*mcp.CallToolResult, BudgetAnswer, error,
) {
	p, err := t.budgets.Propose(ctx, t.self, budget.Proposal{
		Scope:  budget.Scope{Kind: budget.Kind(in.Kind), ID: in.ID},
		Cap:    budget.Cap{LimitUSD: in.LimitUSD, Monthly: in.Monthly, Threshold: in.Threshold},
		Reason: in.Reason,
	})
	if err != nil {
		return nil, BudgetAnswer{}, err
	}

	return nil, BudgetAnswer{ID: p.ID, Note: "proposed to the human; wait for their answer"}, nil
}
