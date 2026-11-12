package messaging

import (
	"errors"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// DefaultMaxHops caps the length of a conversation. Past it, the conversation
// must be escalated instead of continued.
const DefaultMaxHops = 10

// Errors returned when a message is refused. They are meant to be shown to the
// sending agent, so they explain what to do instead.
var (
	ErrUnknownAddress = errors.New("unknown address")
	ErrNoChannel      = errors.New("no channel allows this conversation: you can only start conversations " +
		"with your own team or teams your team has a channel to, and answer messages you received")
	ErrTooManyHops    = errors.New("this conversation is too long: stop exchanging and escalate to the human")
	ErrNothingToReply = errors.New("the message you are answering did not ask for an answer: do not reply to it; " +
		"send a request, query or propose only if you have something new to ask")
)

// Policy decides whether a message may be sent, from the organization chart.
type Policy struct {
	Org     org.Source
	MaxHops int
}

// Check returns nil if msg may be sent. parent is the message it answers, or
// nil when msg starts a new conversation.
func (p Policy) Check(msg Message, parent *Message) error {
	if err := p.checkAddress(msg.From); err != nil {
		return err
	}

	if err := p.checkAddress(msg.To); err != nil {
		return err
	}

	if msg.To == org.SystemAddress {
		return fmt.Errorf("%w: %q only sends notices", ErrUnknownAddress, org.SystemAddress)
	}

	if msg.Hops > p.maxHops() {
		return ErrTooManyHops
	}

	if isAcknowledgementLoop(msg, parent) {
		return ErrNothingToReply
	}

	if p.allowed(msg, parent) {
		return nil
	}

	return ErrNoChannel
}

// allowed applies the conversation rules, from the most to the least common.
func (p Policy) allowed(msg Message, parent *Message) bool {
	switch {
	case msg.From == org.HumanAddress || msg.From == org.SystemAddress:
		return true // the human and the system may write to anyone
	case msg.To == org.HumanAddress:
		return true // anyone may escalate to the human
	case isReplyTo(msg, parent):
		return true // anyone may answer a message they received
	case org.TeamOf(msg.From) == org.TeamOf(msg.To):
		return true // a team always talks to itself
	default:
		return p.Org.Current().HasChannel(org.TeamOf(msg.From), org.TeamOf(msg.To))
	}
}

// isAcknowledgementLoop reports whether msg merely acknowledges a message
// that expected no answer ("thanks" after "done"). Left alone, polite agents
// answer acknowledgements with acknowledgements forever.
// The human is exempt: the operator may comment on anything.
func isAcknowledgementLoop(msg Message, parent *Message) bool {
	return msg.From != org.HumanAddress && parent != nil &&
		!parent.Act.RequiresReply() && !msg.Act.RequiresReply()
}

// isReplyTo reports whether msg answers parent, i.e. goes back to its sender.
func isReplyTo(msg Message, parent *Message) bool {
	return parent != nil && parent.To == msg.From && parent.From == msg.To
}

func (p Policy) checkAddress(address string) error {
	if address == org.HumanAddress || address == org.SystemAddress {
		return nil
	}

	if _, ok := p.Org.Current().Member(address); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownAddress, address)
	}

	return nil
}

func (p Policy) maxHops() int {
	if p.MaxHops > 0 {
		return p.MaxHops
	}

	return DefaultMaxHops
}
