package officemcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The director's tool to close, by force, the conversations between agents
// that nobody answers (messaging.Closer, docs/god.md).

// conversationToolNames are the director's conversation tools.
var conversationToolNames = []string{"conversations_close"}

// Conversations is what backs conversations_close (implemented in
// internal/daemon).
type Conversations interface {
	// CloseOpen closes every open conversation between agents, telling the
	// agents that were waiting, and returns the IDs of the deleted messages.
	CloseOpen(ctx context.Context) ([]string, error)
}

// ConversationsCloseOutput reports what was closed.
type ConversationsCloseOutput struct {
	Deleted []string `json:"deleted"`
	Note    string   `json:"note"`
}

// addConversationTools registers conversations_close.
func (t tools) addConversationTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "conversations_close",
		Description: "Close for good every open conversation between agents (a request, query or proposal nobody " +
			"answered): its messages are deleted and the agent that was waiting is told it is closed. Conversations " +
			"with the human are never touched. Only with the human's explicit agreement: ask them first.",
	}, t.conversationsClose)
}

func (t tools) conversationsClose(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (
	*mcp.CallToolResult, ConversationsCloseOutput, error,
) {
	ids, err := t.conversations.CloseOpen(ctx)
	if err != nil {
		return nil, ConversationsCloseOutput{}, err
	}

	if ids == nil {
		ids = []string{}
	}

	return nil, ConversationsCloseOutput{Deleted: ids, Note: fmt.Sprintf("%d message(s) deleted", len(ids))}, nil
}
