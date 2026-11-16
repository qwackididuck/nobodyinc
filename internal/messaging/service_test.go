package messaging

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// testOrg: specs (archi, secu), dev (dev1), qa (tester); channel dev -> specs.
func testOrg() *org.Org {
	return &org.Org{
		Teams: []org.Team{
			{ID: "specs", Agents: []org.Agent{{Name: "archi"}, {Name: "secu"}}},
			{ID: "dev", Agents: []org.Agent{{Name: "dev1"}}},
			{ID: "qa", Agents: []org.Agent{{Name: "tester"}}},
		},
		Channels: []org.Channel{{From: "dev", To: "specs"}},
	}
}

type memStore map[string]Message

func (m memStore) InsertMessage(_ context.Context, msg Message) error {
	m[msg.ID] = msg

	return nil
}

func (m memStore) GetMessage(_ context.Context, id string) (Message, error) {
	msg, ok := m[id]
	if !ok {
		return Message{}, errors.New("not found")
	}

	return msg, nil
}

func newService(maxHops int) *Service {
	n := 0

	return &Service{
		Store:  memStore{},
		Policy: Policy{Org: testOrg(), MaxHops: maxHops},
		Now:    func() time.Time { return time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC) },
		NewID: func() string {
			n++

			return fmt.Sprintf("m%d", n)
		},
	}
}

func draft(from, to string) Draft {
	return Draft{From: from, To: to, Act: ActQuery, Priority: Normal, Subject: "question"}
}

func TestSendChannelRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr error
	}{
		{"same team", "archi@specs", "secu@specs", nil},
		{"declared channel dev -> specs", "dev1@dev", "archi@specs", nil},
		{"channels are one-way: specs cannot start with dev", "archi@specs", "dev1@dev", ErrNoChannel},
		{"no channel at all", "tester@qa", "dev1@dev", ErrNoChannel},
		{"human writes to anyone", org.HumanAddress, "tester@qa", nil},
		{"agents may escalate to the human", "tester@qa", org.HumanAddress, nil},
		{"unknown recipient", "dev1@dev", "ghost@dev", ErrUnknownAddress},
		{"unknown sender", "ghost@dev", "dev1@dev", ErrUnknownAddress},
		{"the system writes to anyone", org.SystemAddress, "tester@qa", nil},
		{"nobody writes to the system", "tester@qa", org.SystemAddress, ErrUnknownAddress},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := newService(0).Send(context.Background(), draft(tt.from, tt.to))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Send error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDirectorWritesToAnyoneAndAnyoneWritesToIt(t *testing.T) {
	t.Parallel()

	o := testOrg()
	o.God = &org.Agent{Name: "god", Role: "Director"}
	svc := &Service{Store: memStore{}, Policy: Policy{Org: o}}

	for _, route := range [][2]string{{"god", "tester@qa"}, {"tester@qa", "god"}, {"god", "human"}, {"archi@specs", "god"}} {
		_, err := svc.Send(context.Background(), Draft{From: route[0], To: route[1], Act: ActRequest, Priority: Normal, Subject: "s"})
		if err != nil {
			t.Errorf("%s -> %s refused: %v", route[0], route[1], err)
		}
	}
}

func TestReplyIsAllowedWithoutChannel(t *testing.T) {
	t.Parallel()

	svc := newService(0)
	ctx := context.Background()

	question, err := svc.Send(ctx, draft("dev1@dev", "archi@specs"))
	if err != nil {
		t.Fatal(err)
	}

	answer := draft("archi@specs", "dev1@dev")
	answer.Act = ActInform
	answer.InReplyTo = question.ID

	reply, err := svc.Send(ctx, answer)
	if err != nil {
		t.Fatalf("specs must be able to answer dev: %v", err)
	}

	if reply.Conversation != question.ID || reply.Hops != 1 {
		t.Fatalf("reply threading = (%s, %d), want (%s, 1)", reply.Conversation, reply.Hops, question.ID)
	}

	// Replying to a message addressed to someone else is not a reply.
	hijack := draft("archi@specs", "dev1@dev")

	hijack.InReplyTo = reply.ID // reply went from archi to dev1, not to archi
	if _, err := svc.Send(ctx, hijack); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("answering your own message must not open a channel, got %v", err)
	}
}

func TestAcknowledgementLoopIsRefused(t *testing.T) { //nolint:tparallel // subtests share one service and run in order
	t.Parallel()

	svc := newService(0)
	ctx := context.Background()

	question, err := svc.Send(ctx, draft("dev1@dev", "archi@specs")) // query
	if err != nil {
		t.Fatal(err)
	}

	answer := Draft{From: "archi@specs", To: "dev1@dev", Act: ActInform, Priority: Normal, Subject: "PostgreSQL", InReplyTo: question.ID}

	done, err := svc.Send(ctx, answer)
	if err != nil {
		t.Fatalf("answering a query must be allowed: %v", err)
	}

	tests := []struct {
		name    string
		act     Act
		wantErr error
	}{
		{"thanks after an answer is refused", ActAgree, ErrNothingToReply},
		{"done after an answer is refused", ActDone, ErrNothingToReply},
		{"a follow-up question is allowed", ActQuery, nil},
	}

	t.Run("the human may comment on anything", func(t *testing.T) {
		toHuman, err := svc.Send(ctx, Draft{From: "archi@specs", To: org.HumanAddress, Act: ActDone, Priority: Normal, Subject: "finished"})
		if err != nil {
			t.Fatal(err)
		}

		comment := Draft{From: org.HumanAddress, To: "archi@specs", Act: ActInform, Priority: Normal, Subject: "great", InReplyTo: toHuman.ID}
		if _, err := svc.Send(ctx, comment); err != nil {
			t.Fatalf("the human must not be limited: %v", err)
		}
	})

	for _, tt := range tests { //nolint:paralleltest // subtests share one service and run in order
		t.Run(tt.name, func(t *testing.T) {
			d := Draft{From: "dev1@dev", To: "archi@specs", Act: tt.act, Priority: Normal, Subject: "re", InReplyTo: done.ID}
			if _, err := svc.Send(ctx, d); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAnswerInheritsTicket(t *testing.T) {
	t.Parallel()

	svc := newService(0)
	ctx := context.Background()
	q := draft("dev1@dev", "archi@specs")
	q.Ticket = "T-3"

	question, err := svc.Send(ctx, q)
	if err != nil {
		t.Fatal(err)
	}

	a := Draft{From: "archi@specs", To: "dev1@dev", Act: ActInform, Priority: Normal, Subject: "re", InReplyTo: question.ID}

	answer, err := svc.Send(ctx, a)
	if err != nil {
		t.Fatal(err)
	}

	if answer.Ticket != "T-3" {
		t.Fatalf("answer ticket = %q, want T-3", answer.Ticket)
	}
}

func TestHumanCanBeAnswered(t *testing.T) {
	t.Parallel()

	svc := newService(0)
	ctx := context.Background()

	order, err := svc.Send(ctx, draft(org.HumanAddress, "tester@qa"))
	if err != nil {
		t.Fatal(err)
	}

	answer := draft("tester@qa", org.HumanAddress)

	answer.InReplyTo = order.ID
	if _, err := svc.Send(ctx, answer); err != nil {
		t.Fatalf("an agent must be able to answer the human: %v", err)
	}
}

func TestConversationHopCap(t *testing.T) {
	t.Parallel()

	svc := newService(2)
	ctx := context.Background()

	msg, err := svc.Send(ctx, draft("archi@specs", "secu@specs")) // hop 0
	if err != nil {
		t.Fatal(err)
	}

	pair := [2]string{"secu@specs", "archi@specs"}
	for hop := 1; hop <= 3; hop++ {
		d := draft(pair[(hop+1)%2], pair[hop%2])
		d.InReplyTo = msg.ID

		next, err := svc.Send(ctx, d)
		if hop <= 2 {
			if err != nil {
				t.Fatalf("hop %d refused: %v", hop, err)
			}

			msg = next

			continue
		}

		if !errors.Is(err, ErrTooManyHops) {
			t.Fatalf("hop %d: error = %v, want ErrTooManyHops", hop, err)
		}
	}
}

func TestSendValidatesDraft(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		draft Draft
	}{
		{"unknown act", Draft{From: "archi@specs", To: "secu@specs", Act: "shout", Priority: Normal, Subject: "x"}},
		{"unknown priority", Draft{From: "archi@specs", To: "secu@specs", Act: ActInform, Priority: "asap", Subject: "x"}},
		{"empty message", Draft{From: "archi@specs", To: "secu@specs", Act: ActInform, Priority: Normal}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := newService(0).Send(context.Background(), tt.draft); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestTeamAddress(t *testing.T) {
	t.Parallel()

	svc := newService(0)
	svc.PickMember = func(team string) string {
		if team == "specs" {
			return "secu@specs"
		}

		return ""
	}
	ctx := context.Background()

	msg, err := svc.Send(ctx, draft("dev1@dev", "@specs"))
	if err != nil {
		t.Fatal(err)
	}

	if msg.To != "secu@specs" {
		t.Fatalf("message to @specs delivered to %q, want the picked member", msg.To)
	}

	if _, err := svc.Send(ctx, draft("dev1@dev", "@ghosts")); !errors.Is(err, ErrUnknownAddress) {
		t.Fatalf("unknown team: error = %v", err)
	}

	if _, err := svc.Send(ctx, draft("archi@specs", "@qa")); !errors.Is(err, ErrUnknownAddress) {
		t.Fatalf("PickMember returned nobody for qa: error = %v", err)
	}
}

func TestOnSentIsCalled(t *testing.T) {
	t.Parallel()

	svc := newService(0)

	var got []string

	svc.OnSent = func(m Message) { got = append(got, m.To) }

	if _, err := svc.Send(context.Background(), draft("dev1@dev", "archi@specs")); err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != "archi@specs" {
		t.Fatalf("OnSent calls = %v", got)
	}
}
