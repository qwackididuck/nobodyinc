package officemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The director's decision tool (docs/automation.md): in Nobody mode it
// answers, in the human's place, the questions the agents ask them, and
// reports each answer so that the human can review it when they come back.

// decisionToolNames are the director's decision tools.
var decisionToolNames = []string{"decision_log"}

// Decisions is what backs decision_log (implemented in internal/daemon).
type Decisions interface {
	// Log records the director's decision on a question addressed to the
	// human and sends it to the agent that asked.
	Log(ctx context.Context, by string, in DecisionLogInput) (DecisionLogOutput, error)
}

// DecisionLogInput is a decision the director took in the human's place.
type DecisionLogInput struct {
	MessageID string `json:"message_id" jsonschema:"the ID of the question you are answering, as given in the notice"`
	Decision  string `json:"decision"   jsonschema:"what you decided, as the agent that asked must read it: clear and actionable"`
	Rationale string `json:"rationale"  jsonschema:"why: the options you saw and what tipped the choice, so the human can judge it later"`
	//nolint:lll // a struct tag cannot be split
	Impact string `json:"impact" jsonschema:"low, medium or high; high for anything hard to undo, costly, changing the scope or touching security"`
}

// DecisionLogOutput confirms the decision.
type DecisionLogOutput struct {
	ID   int64  `json:"id"`
	Note string `json:"note"`
}

// addScheduleAndDecisionTools registers the director's scheduled-task and
// decision tools, those that are backed.
func (t tools) addScheduleAndDecisionTools(server *mcp.Server) {
	if t.schedules != nil {
		t.addScheduleTools(server)
	}

	if t.decisions != nil {
		t.addDecisionTools(server)
	}
}

// addDecisionTools registers decision_log.
func (t tools) addDecisionTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "decision_log",
		Description: "In Nobody mode, record the decision you took in the human's place on an agent's question. " +
			"It is sent to the agent that asked (do not also send it yourself) and kept in the report the human reads " +
			"when they come back, with its impact highlighted. Call it once per question, only for a question a notice " +
			"asked you to decide.",
	}, t.decisionLog)
}

func (t tools) decisionLog(ctx context.Context, _ *mcp.CallToolRequest, in DecisionLogInput) (
	*mcp.CallToolResult, DecisionLogOutput, error,
) {
	out, err := t.decisions.Log(ctx, t.self, in)

	return nil, out, err
}
