package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Store keeps messages. It is implemented by internal/store (SQLite).
type Store interface {
	InsertMessage(ctx context.Context, m Message) error
	GetMessage(ctx context.Context, id string) (Message, error)
}

// Service sends messages: it validates, threads, checks the policy and files
// them in the store.
type Service struct {
	Store  Store
	Policy Policy
	// Now and NewID are injectable for tests; they default to the real clock
	// and random IDs.
	Now   func() time.Time
	NewID func() string
	// OnSent, when set, is called after a message is stored, e.g. to wake up
	// an idle recipient.
	OnSent func(Message)
	// PickMember chooses which member of a team receives a message sent to
	// "@<team>"; it returns "" for an unknown or empty team. Nil disables
	// team addresses.
	PickMember func(team string) string
}

// TeamAddressPrefix marks a team address: "@dev" lets the dev team pick an
// available member.
const TeamAddressPrefix = "@"

// Send turns a draft into a message and files it, or explains why it cannot
// be sent.
func (s *Service) Send(ctx context.Context, d Draft) (Message, error) {
	if err := d.validate(); err != nil {
		return Message{}, err
	}

	if err := s.resolveTeamAddress(&d); err != nil {
		return Message{}, err
	}

	parent, err := s.parentOf(ctx, d)
	if err != nil {
		return Message{}, err
	}

	msg := Message{
		ID:        s.newID(),
		InReplyTo: d.InReplyTo,
		From:      d.From,
		To:        d.To,
		Act:       d.Act,
		Priority:  d.Priority,
		Subject:   d.Subject,
		Body:      d.Body,
		Ticket:    d.Ticket,
		SentAt:    s.now(),
	}

	msg.Conversation, msg.Hops = threadPosition(msg.ID, parent)
	if msg.Ticket == "" && parent != nil {
		msg.Ticket = parent.Ticket
	}

	if err := s.Policy.Check(msg, parent); err != nil {
		return Message{}, err
	}

	if err := s.Store.InsertMessage(ctx, msg); err != nil {
		return Message{}, fmt.Errorf("store message: %w", err)
	}

	if s.OnSent != nil {
		s.OnSent(msg)
	}

	return msg, nil
}

// resolveTeamAddress replaces "@<team>" by the member chosen to answer.
func (s *Service) resolveTeamAddress(d *Draft) error {
	team, isTeam := strings.CutPrefix(d.To, TeamAddressPrefix)
	if !isTeam {
		return nil
	}

	var member string
	if s.PickMember != nil {
		member = s.PickMember(team)
	}

	if member == "" {
		return fmt.Errorf("%w: no team %q to write to", ErrUnknownAddress, team)
	}

	d.To = member

	return nil
}

// parentOf loads the message being answered, or returns nil when the draft
// starts a new conversation.
func (s *Service) parentOf(ctx context.Context, d Draft) (*Message, error) {
	if d.InReplyTo == "" {
		return nil, nil //nolint:nilnil // no parent is the normal case of a new conversation
	}

	parent, err := s.Store.GetMessage(ctx, d.InReplyTo)
	if err != nil {
		return nil, fmt.Errorf("message %q being answered: %w", d.InReplyTo, err)
	}

	return &parent, nil
}

// threadPosition returns the conversation a message belongs to, then its hop
// count: a new conversation starts at hop 0, each answer adds one.
func threadPosition(id string, parent *Message) (string, int) {
	if parent == nil {
		return id, 0
	}

	return parent.Conversation, parent.Hops + 1
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now().UTC()
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}

	return RandomID()
}

// RandomID returns a time-sortable unique ID such as "20261111T153000-a1b2c3d4".
func RandomID() string {
	var b [4]byte

	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error

	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}
