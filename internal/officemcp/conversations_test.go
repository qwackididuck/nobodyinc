package officemcp

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

type fakeConversations struct{ closed int }

func (f *fakeConversations) CloseOpen(context.Context) ([]string, error) {
	f.closed++

	return []string{"m1", "m2"}, nil
}

func connectConversations(t *testing.T, address string, c Conversations) *mcp.ClientSession {
	t.Helper()

	o := testOrg()
	o.God = &org.Agent{Name: "god", Role: "Director"}
	sender := &messaging.Service{Store: memStore{}, Policy: messaging.Policy{Org: o}}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := NewServer(o, Office{Sender: sender, Conversations: c, Version: "test"}, address).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session
}

func TestOnlyTheDirectorClosesConversations(t *testing.T) {
	t.Parallel()

	fake := &fakeConversations{}

	if slices.Contains(toolNames(t, connectConversations(t, "dev1@dev", fake)), "conversations_close") {
		t.Fatal("a team agent got conversations_close")
	}

	god := connectConversations(t, "god", fake)
	if !slices.Contains(toolNames(t, god), "conversations_close") {
		t.Fatal("the director misses conversations_close")
	}

	if !slices.Contains(ToolNames(), "conversations_close") {
		t.Fatal("conversations_close is not allowed without asking (ToolNames)")
	}

	res, err := god.CallTool(context.Background(), &mcp.CallToolParams{Name: "conversations_close"})
	if err != nil || res.IsError {
		t.Fatalf("call: %v %+v", err, res)
	}

	if fake.closed != 1 {
		t.Fatalf("closed %d times, want 1", fake.closed)
	}
}
