// Package messaging is the post office of Nobody Inc: it decides whether a
// message may be sent, threads it, and files it in the recipient's tray.
//
// The message format follows the hive protocol (FIPA-inspired): every message
// has an act saying what the sender expects, belongs to a conversation, and
// counts its hops so that two agents cannot ping-pong forever.
//
// Delivery to the agent itself is not done here: the hooks pick messages up
// from the trays (see internal/hook and docs/hooks.md).
package messaging

import (
	"errors"
	"fmt"
	"time"
)

// Act says what the sender expects from the recipient.
type Act string

// The acts an agent can use.
const (
	ActRequest Act = "request" // please do something
	ActQuery   Act = "query"   // please answer a question
	ActPropose Act = "propose" // here is a proposal, agree or refuse
	ActInform  Act = "inform"  // for your information
	ActAgree   Act = "agree"   // I accept your request or proposal
	ActRefuse  Act = "refuse"  // I decline, with a reason
	ActDone    Act = "done"    // the requested work is finished
)

// RequiresReply reports whether the recipient is expected to answer.
func (a Act) RequiresReply() bool {
	return a == ActRequest || a == ActQuery || a == ActPropose
}

func (a Act) valid() bool {
	switch a {
	case ActRequest, ActQuery, ActPropose, ActInform, ActAgree, ActRefuse, ActDone:
		return true
	}

	return false
}

// Priority says which tray of the recipient the message goes to.
type Priority string

// Normal mail is handed over when the recipient ends its turn; urgent mail as
// soon as it uses a tool.
const (
	Normal Priority = "normal"
	Urgent Priority = "urgent"
)

// Message is a piece of mail between two addresses ("<agent>@<team>" or "human").
type Message struct {
	ID           string   `json:"id"`
	Conversation string   `json:"conversation"`
	InReplyTo    string   `json:"in_reply_to,omitempty"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Act          Act      `json:"act"`
	Priority     Priority `json:"priority"`
	Subject      string   `json:"subject"`
	Body         string   `json:"body"`
	Hops         int      `json:"hops"`
	// Ticket is the ticket the message is about ("T-12"), or "".
	Ticket      string     `json:"ticket,omitempty"`
	SentAt      time.Time  `json:"sent_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

// Draft is what a sender provides; the post office fills in the rest.
type Draft struct {
	From      string
	To        string
	Act       Act
	Priority  Priority
	Subject   string
	Body      string
	InReplyTo string
	// Ticket links the message to a ticket; an answer inherits the ticket of
	// the message it answers.
	Ticket string
}

func (d Draft) validate() error {
	if !d.Act.valid() {
		return fmt.Errorf("unknown act %q", d.Act)
	}

	if d.Priority != Normal && d.Priority != Urgent {
		return fmt.Errorf("unknown priority %q", d.Priority)
	}

	if d.Subject == "" && d.Body == "" {
		return errors.New("a message needs a subject or a body")
	}

	return nil
}
