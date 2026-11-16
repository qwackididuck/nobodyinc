package officemcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

func testOrg() *org.Org {
	return &org.Org{
		Teams: []org.Team{
			{ID: "specs", Agents: []org.Agent{{Name: "archi", Role: "Architect"}, {Name: "secu", Role: "Security"}}},
			{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}}},
			{ID: "qa", Agents: []org.Agent{{Name: "tester", Role: "Tester"}}},
		},
		Channels: []org.Channel{{From: "dev", To: "specs"}},
	}
}

type memStore map[string]messaging.Message

func (m memStore) InsertMessage(_ context.Context, msg messaging.Message) error {
	m[msg.ID] = msg

	return nil
}

func (m memStore) GetMessage(_ context.Context, id string) (messaging.Message, error) {
	return m[id], nil
}

// connect starts the MCP server of address and returns a client session to it.
func connect(t *testing.T, address string) (*mcp.ClientSession, memStore) {
	t.Helper()

	o := testOrg()
	st := memStore{}
	sender := &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := NewServer(o, Office{Sender: sender, Version: "test"}, address).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session, st
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		from      string
		args      map[string]any
		wantError string
	}{
		{
			name: "dev questions specs through the channel",
			from: "dev1@dev",
			args: map[string]any{"to": "archi@specs", "act": "query", "subject": "Which DB?", "urgent": true},
		},
		{
			name:      "specs cannot start a conversation with dev",
			from:      "archi@specs",
			args:      map[string]any{"to": "dev1@dev", "act": "inform", "subject": "FYI"},
			wantError: "no channel",
		},
		{
			name:      "unknown act is refused",
			from:      "dev1@dev",
			args:      map[string]any{"to": "archi@specs", "act": "shout", "subject": "x"},
			wantError: "unknown act",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session, st := connect(t, tt.from)

			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "send_message", Arguments: tt.args})
			if err != nil {
				t.Fatal(err)
			}

			if tt.wantError != "" {
				if !res.IsError || !strings.Contains(textOf(res), tt.wantError) {
					t.Fatalf("expected tool error containing %q, got %q", tt.wantError, textOf(res))
				}

				return
			}

			if res.IsError {
				t.Fatalf("unexpected tool error: %s", textOf(res))
			}

			if len(st) != 1 {
				t.Fatalf("stored %d messages, want 1", len(st))
			}

			for _, m := range st {
				if m.From != tt.from {
					t.Fatalf("message sent as %q, want the calling agent %q", m.From, tt.from)
				}

				if m.Priority != messaging.Urgent {
					t.Fatalf("urgent flag lost: priority = %s", m.Priority)
				}
			}
		})
	}
}

func TestListContacts(t *testing.T) {
	t.Parallel()

	session, _ := connect(t, "dev1@dev")

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_contacts"})
	if err != nil {
		t.Fatal(err)
	}

	var out ListContactsOutput
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil {
		t.Fatalf("decode %s: %v", textOf(res), err)
	}

	got := map[string]string{}
	for _, c := range out.Contacts {
		got[c.Address] = c.Why
	}

	want := map[string]string{"archi@specs": "channel", "secu@specs": "channel"}
	if len(got) != len(want) || got["archi@specs"] != "channel" || got["secu@specs"] != "channel" {
		t.Fatalf("contacts = %v, want %v (no qa: no channel)", got, want)
	}

	if len(out.Organization) != 3 || out.Organization[2].Team != "qa" || out.Organization[2].Reach != ReachAnswerOnly {
		t.Fatalf("organization = %+v, want every team, qa answer-only", out.Organization)
	}
}

func TestContactsIncludeTeammates(t *testing.T) {
	t.Parallel()

	contacts := Contacts(testOrg(), "archi@specs")
	if len(contacts) != 1 || contacts[0].Address != "secu@specs" || contacts[0].Why != "teammate" {
		t.Fatalf("contacts = %+v", contacts)
	}
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder

	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}

	return b.String()
}

// connectWithTickets is connect with a real ticket service on an in-memory store.
func connectWithTickets(t *testing.T, address string) *mcp.ClientSession {
	t.Helper()

	o := testOrg()

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	sender := &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}}
	tickets := &work.Service{Store: st, Org: o}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := NewServer(o, Office{Sender: sender, Tickets: tickets, Version: "test"}, address).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session
}

func call(t *testing.T, s *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()

	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}

	return textOf(res), res.IsError
}

func TestTicketTools(t *testing.T) {
	t.Parallel()

	dev := connectWithTickets(t, "dev1@dev")

	if text, isErr := call(t, dev, "ticket_create", map[string]any{"title": "Build the API", "team": "dev"}); isErr || !strings.Contains(text, `"id":"T-1"`) {
		t.Fatalf("ticket_create = %s (error %v)", text, isErr)
	}

	if text, _ := call(t, dev, "ticket_list", nil); !strings.Contains(text, `"assigned":[]`) || !strings.Contains(text, `"id":"T-1"`) {
		t.Fatalf("ticket_list should show T-1 in the queue: %s", text)
	}

	if text, isErr := call(t, dev, "ticket_claim", nil); isErr || !strings.Contains(text, `"assignee":"dev1@dev"`) {
		t.Fatalf("ticket_claim = %s (error %v)", text, isErr)
	}

	if _, isErr := call(t, dev, "deliverable_add", map[string]any{"id": "T-1", "kind": "commit", "reference": "abc", "summary": "API done"}); isErr {
		t.Fatal("deliverable_add failed")
	}

	if text, isErr := call(t, dev, "ticket_update", map[string]any{"id": "T-1", "status": "done", "comment": "All green"}); isErr || !strings.Contains(text, `"status":"done"`) {
		t.Fatalf("ticket_update = %s (error %v)", text, isErr)
	}

	if text, _ := call(t, dev, "ticket_get", map[string]any{"id": "T-1"}); !strings.Contains(text, "API done") || !strings.Contains(text, "All green") {
		t.Fatalf("ticket_get should show the deliverable and the comment: %s", text)
	}

	if text, isErr := call(t, dev, "ticket_create", map[string]any{"title": "x", "team": "qa"}); !isErr || !strings.Contains(text, "not allowed") {
		t.Fatalf("creating a ticket for a team without channel should be refused: %s", text)
	}
}

func TestChart(t *testing.T) {
	t.Parallel()

	o := testOrg()
	o.Teams[0].Agents[1].OnLeave = true // secu@specs

	chart := Chart(o, "dev1@dev")

	var got []string
	for _, team := range chart {
		got = append(got, team.Team+":"+team.Reach)
	}

	if want := "dev:own_team specs:channel qa:answer_only"; strings.Join(got, " ") != want {
		t.Fatalf("chart = %q, want %q (own team, then channels, then the others)", strings.Join(got, " "), want)
	}

	if len(chart[0].Members) != 0 {
		t.Fatalf("the agent itself must not be listed: %+v", chart[0].Members)
	}

	if specs := chart[1].Members; len(specs) != 2 || specs[0].Address != "archi@specs" || specs[1].OnLeave != true {
		t.Fatalf("specs members = %+v, want archi then secu on leave", specs)
	}
}
