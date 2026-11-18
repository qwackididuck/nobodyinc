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
	ErrBlocked        = errors.New("your team may not start conversations with this team (ADR-0009): " +
		"you can still answer the messages its members send you; to reach them, ask the human instead")
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

	return fmt.Errorf("%w (team %s blocks team %s)", ErrBlocked, org.TeamOf(msg.From), org.TeamOf(msg.To))
}

// allowed applies the conversation rules (ADR-0009), from the most to the
// least common: flows are open, and only a team's block list stops one of its
// members from starting a conversation.
func (p Policy) allowed(msg Message, parent *Message) bool {
	o := p.Org.Current()

	switch {
	case msg.From == org.HumanAddress || msg.From == org.SystemAddress || o.IsGod(msg.From):
		return true // the human, the system and the director may write to anyone
	case msg.To == org.HumanAddress || o.IsGod(msg.To):
		return true // anyone may escalate to the human or the director
	case isReplyTo(msg, parent):
		return true // anyone may answer a message they received
	default:
		return o.CanStart(org.TeamOf(msg.From), org.TeamOf(msg.To)) // unless blocked; a team always talks to itself
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
