// Package officemcp is the MCP server every agent gets: its window onto the
// organization. It exposes:
//
//   - send_message: write to a colleague (or answer the human);
//   - list_contacts: who the agent may start a conversation with;
//   - the ticket tools (tickets.go): ticket_list, ticket_get, ticket_claim,
//     ticket_create, ticket_update, ticket_comment, deliverable_add.
//
// The daemon serves it over HTTP at /mcp/{address}. The address in the URL is
// the identity of the calling agent: it is set by the daemon in the agent's MCP
// config at launch, so an agent always sends mail under its own name.
//
// Receiving mail does not go through MCP: it is handed over by the hooks (see
// docs/hooks.md).
package officemcp

import (
	"context"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Sender sends messages on behalf of agents (implemented by messaging.Service).
type Sender interface {
	Send(ctx context.Context, d messaging.Draft) (messaging.Message, error)
}

// Handler returns the HTTP handler serving one MCP server per agent. It must be
// mounted on a pattern with an {address} wildcard, e.g. "/mcp/{address}".
func Handler(o *org.Org, sender Sender, tickets Tickets, version string) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		member, ok := o.Member(r.PathValue("address"))
		if !ok {
			return nil // the SDK answers 400 Bad Request
		}

		return NewServer(o, sender, tickets, member.Address, version)
	}, &mcp.StreamableHTTPOptions{Stateless: true})
}

// ToolNames lists the office tools, e.g. to allow them without asking.
func ToolNames() []string {
	return []string{
		"send_message", "list_contacts",
		"ticket_list", "ticket_get", "ticket_claim", "ticket_create", "ticket_update", "ticket_comment", "deliverable_add",
	}
}

// NewServer builds the MCP server of one agent.
func NewServer(o *org.Org, sender Sender, tickets Tickets, address, version string) *mcp.Server {
	t := tools{org: o, sender: sender, tickets: tickets, self: address}
	server := mcp.NewServer(&mcp.Implementation{Name: "nobody-inc", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "send_message",
		Description: "Send a message to a colleague, or answer a message you received. " +
			"You can start conversations only with your own team and the teams listed by list_contacts; " +
			"you can answer a message that expects an answer (request, query, propose) by setting in_reply_to to its ID. " +
			"Never reply to inform, agree, refuse or done.",
	}, t.sendMessage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_contacts",
		Description: "List the colleagues you can start a conversation with, and why.",
	}, t.listContacts)

	t.addTicketTools(server)

	return server
}

type tools struct {
	org     *org.Org
	sender  Sender
	tickets Tickets
	self    string
}

// SendMessageInput is what an agent provides to send_message.
type SendMessageInput struct {
	To        string `json:"to"                    jsonschema:"recipient: an agent (archi@specs), a team (@specs) or human"`
	Act       string `json:"act"                   jsonschema:"request, query, propose (answer expected) or inform, agree, refuse, done"`
	Subject   string `json:"subject"               jsonschema:"one-line summary"`
	Body      string `json:"body,omitempty"        jsonschema:"the message itself"`
	Urgent    bool   `json:"urgent,omitempty"      jsonschema:"true only if the recipient must see it before finishing its current turn"`
	InReplyTo string `json:"in_reply_to,omitempty" jsonschema:"ID of the message you are answering"`
	Ticket    string `json:"ticket,omitempty"      jsonschema:"ID of the ticket this message is about, e.g. T-12"`
}

// SendMessageOutput confirms what was sent.
type SendMessageOutput struct {
	ID           string `json:"id"`
	Conversation string `json:"conversation"`
}

func (t tools) sendMessage(
	ctx context.Context, _ *mcp.CallToolRequest, in SendMessageInput,
) (*mcp.CallToolResult, SendMessageOutput, error) {
	priority := messaging.Normal
	if in.Urgent {
		priority = messaging.Urgent
	}

	msg, err := t.sender.Send(ctx, messaging.Draft{
		From:      t.self,
		To:        in.To,
		Act:       messaging.Act(in.Act),
		Priority:  priority,
		Subject:   in.Subject,
		Body:      in.Body,
		InReplyTo: in.InReplyTo,
		Ticket:    in.Ticket,
	})
	if err != nil {
		// Returned to the agent as a tool error it can read and act on.
		return nil, SendMessageOutput{}, err
	}

	return nil, SendMessageOutput{ID: msg.ID, Conversation: msg.Conversation}, nil
}

// Contact is someone an agent may start a conversation with.
type Contact struct {
	Address string `json:"address"`
	Role    string `json:"role"`
	Team    string `json:"team"`
	// Why says what allows the conversation: "teammate" or "channel".
	Why string `json:"why"`
}

// ListContactsOutput lists an agent's contacts.
type ListContactsOutput struct {
	Contacts []Contact `json:"contacts"`
	// Teams can be written to as "@<team>": an available member answers.
	Teams []string `json:"teams"`
	Note  string   `json:"note"`
}

func (t tools) listContacts(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, ListContactsOutput, error) {
	return nil, ListContactsOutput{
		Contacts: Contacts(t.org, t.self),
		Teams:    teamAddresses(Contacts(t.org, t.self)),
		Note: "Write to a team as @<team> when any member can answer. " +
			"You can also answer any message you received, whoever sent it, by setting in_reply_to.",
	}, nil
}

// teamAddresses lists the "@team" addresses of the teams among contacts.
func teamAddresses(contacts []Contact) []string {
	teams := []string{}

	for _, c := range contacts {
		address := messaging.TeamAddressPrefix + c.Team
		if !slices.Contains(teams, address) {
			teams = append(teams, address)
		}
	}

	return teams
}

// Contacts lists who address may start a conversation with: its teammates,
// then the members of the teams its team has a channel to.
func Contacts(o *org.Org, address string) []Contact {
	myTeam := org.TeamOf(address)
	contacts := []Contact{}

	for _, m := range o.Members() {
		switch {
		case m.Address == address:
			continue
		case m.Team.ID == myTeam:
			contacts = append(contacts, Contact{Address: m.Address, Role: m.Agent.Role, Team: m.Team.ID, Why: "teammate"})
		case o.HasChannel(myTeam, m.Team.ID):
			contacts = append(contacts, Contact{Address: m.Address, Role: m.Agent.Role, Team: m.Team.ID, Why: "channel"})
		}
	}

	return contacts
}
