package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

type memStore struct {
	messages  map[string]messaging.Message
	delivered []string
	requeued  []string
}

func (m *memStore) InsertMessage(_ context.Context, msg messaging.Message) error {
	m.messages[msg.ID] = msg

	return nil
}

func (m *memStore) GetMessage(_ context.Context, id string) (messaging.Message, error) {
	msg, ok := m.messages[id]
	if !ok {
		return messaging.Message{}, errors.New("not found")
	}

	return msg, nil
}

func (m *memStore) requeue(_ context.Context, _ string, ids []string) error {
	m.requeued = append(m.requeued, ids...)

	return nil
}

func (m *memStore) markDelivered(_ context.Context, _ string, ids []string, _ time.Time) error {
	m.delivered = append(m.delivered, ids...)

	return nil
}

// newForkEnv: archi@specs is busy in session "s-archi"; the fake claude
// answers with forkAnswer.
func newForkEnv(t *testing.T, forkAnswer string) (*Forker, *messaging.Service, *memStore, *[]string) {
	t.Helper()

	o := testOrg()
	st := &memStore{messages: map[string]messaging.Message{}}
	svc := &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}}

	statuses := NewStatuses()
	statuses.ObserveHook("archi@specs", hook.Event{Name: hook.EventPostToolUse, SessionID: "s-archi"}, 0)

	var calls []string

	f := &Forker{
		Org:           o,
		Statuses:      statuses,
		Logger:        slog.New(slog.DiscardHandler),
		WorkDir:       func(string) string { return "/tmp/project" },
		Send:          svc.Send,
		MarkDelivered: st.markDelivered,
		Requeue:       st.requeue,
		Run: func(_ context.Context, _ string, args []string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))

			return []byte(`{"result":` + quote(forkAnswer) + `,"is_error":false,"session_id":"s-fork","total_cost_usd":0.012}`), nil
		},
	}

	return f, svc, st, &calls
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func urgentQuery(t *testing.T, svc *messaging.Service) messaging.Message {
	t.Helper()

	q, err := svc.Send(context.Background(), messaging.Draft{
		From: "dev1@dev", To: "archi@specs", Act: messaging.ActQuery, Priority: messaging.Urgent,
		Subject: "Which DB?", Body: "Which database did we pick?",
	})
	if err != nil {
		t.Fatal(err)
	}

	return q
}

func TestForkAnswersOnBehalfOfBusyAgent(t *testing.T) {
	t.Parallel()

	f, svc, st, calls := newForkEnv(t, "PostgreSQL 16, decided this morning.")

	q := urgentQuery(t, svc)
	if !f.ShouldAnswer(q) {
		t.Fatal("an urgent query to a busy agent should be answered by a fork")
	}

	f.Answer(context.Background(), q)

	call := (*calls)[0]
	for _, want := range []string{"--resume s-archi", "--fork-session", `--tools `, "--setting-sources project,local", "--model sonnet", "ESCALATE"} {
		if !strings.Contains(call, want) {
			t.Fatalf("fork command misses %q:\n%s", want, call)
		}
	}

	var reply, notice *messaging.Message

	for _, m := range st.messages {
		switch {
		case m.From == "archi@specs" && m.To == "dev1@dev":
			reply = &m
		case m.From == org.SystemAddress && m.To == "archi@specs":
			notice = &m
		}
	}

	if reply == nil || reply.InReplyTo != q.ID || !strings.Contains(reply.Body, "PostgreSQL 16") {
		t.Fatalf("the asker should get the fork's answer, got %+v", reply)
	}

	if notice == nil || !strings.Contains(notice.Body, "PostgreSQL 16") || !strings.Contains(notice.Body, q.ID) {
		t.Fatalf("the busy agent should get a notice to review the answer, got %+v", notice)
	}

	if len(st.delivered) != 1 || st.delivered[0] != q.ID || len(st.requeued) != 0 {
		t.Fatalf("the answered question should leave the agent's tray: delivered %v, requeued %v", st.delivered, st.requeued)
	}
}

func TestForkEscalationChangesNothing(t *testing.T) {
	t.Parallel()

	f, svc, st, _ := newForkEnv(t, "ESCALATE")
	q := urgentQuery(t, svc)

	f.Answer(context.Background(), q)

	if len(st.messages) != 1 || len(st.requeued) != 1 || st.requeued[0] != q.ID {
		t.Fatalf("an escalated question must go back to the tray: %d messages, requeued %v", len(st.messages), st.requeued)
	}
}

// The fork's cost goes to the agent that asked, answered or escalated alike
// (ADR-0013).
func TestForkCostIsChargedToTheAsker(t *testing.T) {
	t.Parallel()

	for _, answer := range []string{"PostgreSQL 16.", "ESCALATE"} {
		t.Run(answer, func(t *testing.T) {
			t.Parallel()

			f, svc, _, _ := newForkEnv(t, answer)

			var charged []string

			f.RecordCost = func(_ context.Context, asker, session string, cost float64) {
				charged = append(charged, fmt.Sprintf("%s %s %.3f", asker, session, cost))
			}

			f.Answer(context.Background(), urgentQuery(t, svc))

			if len(charged) != 1 || charged[0] != "dev1@dev s-fork 0.012" {
				t.Fatalf("charged %v, want [dev1@dev s-fork 0.012]", charged)
			}
		})
	}
}

func TestShouldAnswer(t *testing.T) { //nolint:tparallel // subtests share one service and run in order
	t.Parallel()

	f, svc, _, _ := newForkEnv(t, "")
	q := urgentQuery(t, svc)

	tests := []struct {
		name   string
		mutate func(m *messaging.Message)
		idle   bool
		want   bool
	}{
		{"urgent query to a busy agent", func(*messaging.Message) {}, false, true},
		{"normal priority", func(m *messaging.Message) { m.Priority = messaging.Normal }, false, false},
		{"not a question", func(m *messaging.Message) { m.Act = messaging.ActRequest }, false, false},
		{"idle agent: woken up instead", func(*messaging.Message) {}, true, false},
		{"unknown session", func(m *messaging.Message) { m.To = "dev1@dev" }, false, false},
	}
	for _, tt := range tests { //nolint:paralleltest // subtests share one service and run in order
		t.Run(tt.name, func(t *testing.T) {
			m := q
			tt.mutate(&m)

			if tt.idle {
				f.Statuses.Set("archi@specs", StatusIdle)
				defer f.Statuses.Set("archi@specs", StatusWorking)
			}

			if got := f.ShouldAnswer(m); got != tt.want {
				t.Fatalf("ShouldAnswer = %v, want %v", got, tt.want)
			}
		})
	}
}
