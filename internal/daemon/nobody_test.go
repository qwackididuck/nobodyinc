package daemon

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/automation"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

type nobodyEnv struct {
	office *nobodyOffice
	mode   *automation.Mode
	store  *store.Store
	sent   *[]messaging.Message
}

func newNobodyEnv(t *testing.T, level automation.Level) nobodyEnv {
	t.Helper()

	ctx := context.Background()

	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	o := &org.Org{
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}}}},
		God:   &org.Agent{Name: "god", Role: "Director"},
	}
	messages := &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}}
	mode := automation.New()

	if _, err := mode.Set(level); err != nil {
		t.Fatal(err)
	}

	var sent []messaging.Message

	office := newNobodyOffice(ctx, func(m messaging.Message) { sent = append(sent, m) },
		mode, o, st, messages, events.NewBus(), slog.New(slog.DiscardHandler))

	return nobodyEnv{office: office, mode: mode, store: st, sent: &sent}
}

func (e nobodyEnv) ask(t *testing.T, from string, act messaging.Act) messaging.Message {
	t.Helper()

	m, err := e.office.messages.Send(context.Background(), messaging.Draft{
		From: from, To: org.HumanAddress, Act: act, Priority: messaging.Normal, Subject: "Which database?", Body: "SQLite or Postgres?",
	})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// toDirector counts the notices the director got, from the system.
func (e nobodyEnv) notices() []messaging.Message {
	var out []messaging.Message

	for _, m := range *e.sent {
		if m.To == "god" && m.From == org.SystemAddress {
			out = append(out, m)
		}
	}

	return out
}

func TestNobodyRoutesQueriesToTheDirector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		level       automation.Level
		from        string
		act         messaging.Act
		wantNotices int
	}{
		{"query in Nobody mode", automation.Nobody, "dev1@dev", messaging.ActQuery, 1},
		{"the director's own query", automation.Nobody, "god", messaging.ActQuery, 1},
		{"auto mode leaves it to the human", automation.Auto, "dev1@dev", messaging.ActQuery, 0},
		{"off", automation.Off, "dev1@dev", messaging.ActQuery, 0},
		{"a proposal stays for the human", automation.Nobody, "god", messaging.ActPropose, 0},
		{"an inform needs no decision", automation.Nobody, "dev1@dev", messaging.ActInform, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newNobodyEnv(t, tt.level)
			q := env.ask(t, tt.from, tt.act)

			notices := env.notices()
			if len(notices) != tt.wantNotices {
				t.Fatalf("%d notices to the director, want %d", len(notices), tt.wantNotices)
			}

			if tt.wantNotices == 1 && (!strings.Contains(notices[0].Body, q.ID) || !strings.Contains(notices[0].Body, "decision_log")) {
				t.Fatalf("the notice must name the question and the tool: %s", notices[0].Body)
			}
		})
	}
}

func TestNobodyLog(t *testing.T) {
	t.Parallel()

	env := newNobodyEnv(t, automation.Nobody)
	ctx := context.Background()
	q := env.ask(t, "dev1@dev", messaging.ActQuery)

	for name, in := range map[string]officemcp.DecisionLogInput{
		"bad impact":      {MessageID: q.ID, Decision: "SQLite", Rationale: "simple", Impact: "huge"},
		"no rationale":    {MessageID: q.ID, Decision: "SQLite", Impact: "low"},
		"unknown message": {MessageID: "nope", Decision: "SQLite", Rationale: "simple", Impact: "low"},
	} {
		if _, err := env.office.Log(ctx, "god", in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	in := officemcp.DecisionLogInput{MessageID: q.ID, Decision: "SQLite", Rationale: "simple", Impact: "high"}
	if _, err := env.office.Log(ctx, "god", in); err != nil {
		t.Fatal(err)
	}

	if _, err := env.office.Log(ctx, "god", in); err == nil {
		t.Error("a question must be decided once")
	}

	decisions, err := env.store.ListDecisions(ctx)
	if err != nil || len(decisions) != 1 || decisions[0].AskedBy != "dev1@dev" || decisions[0].Impact != automation.ImpactHigh {
		t.Fatalf("decisions = %+v, err %v", decisions, err)
	}

	var toAsker, toHuman *messaging.Message

	for i, m := range *env.sent {
		switch {
		case m.To == "dev1@dev" && m.From == "god":
			toAsker = &(*env.sent)[i]
		case m.To == org.HumanAddress && m.From == org.SystemAddress:
			toHuman = &(*env.sent)[i]
		}
	}

	if toAsker == nil || toAsker.InReplyTo != q.ID || !strings.Contains(toAsker.Body, "SQLite") {
		t.Fatalf("the asker must get the decision as an answer: %+v", toAsker)
	}

	if toHuman == nil || toHuman.InReplyTo != q.ID || !strings.Contains(toHuman.Subject, "high") {
		t.Fatalf("the human's card must be closed with the impact: %+v", toHuman)
	}
}

// The director deciding on its own question tells nobody but the human's card.
func TestNobodyLogOwnQuestion(t *testing.T) {
	t.Parallel()

	env := newNobodyEnv(t, automation.Nobody)
	q := env.ask(t, "god", messaging.ActQuery)

	if _, err := env.office.Log(context.Background(), "god", officemcp.DecisionLogInput{
		MessageID: q.ID, Decision: "Postgres", Rationale: "scale", Impact: "medium",
	}); err != nil {
		t.Fatal(err)
	}

	for _, m := range *env.sent {
		if m.From == "god" && m.To != org.HumanAddress {
			t.Fatalf("the director must not write to anyone: %+v", m)
		}
	}
}
