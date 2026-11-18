package server

import (
	"context"
	"math"
	"net/http"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// ticketInProgress attributes an agent's spend to its team and its ticket in
// progress (the daemon also adds the schedule occurrence).
func ticketInProgress(st *store.Store) func(context.Context, string) (budget.Attribution, error) {
	return func(ctx context.Context, address string) (budget.Attribution, error) {
		a := budget.Attribution{Agent: address, Team: org.TeamOf(address)}

		tickets, err := st.ListTickets(ctx, work.Filter{Assignee: address, Status: work.StatusInProgress})
		if len(tickets) > 0 {
			a.Ticket, a.Feature = tickets[0].ID, tickets[0].Feature
		}

		return a, err
	}
}

// The statusLine's running totals become spend, on the agent's ticket in
// progress, in GET /api/spend and the ticket's spent_usd.
func TestStatusLineReportsBecomeSpend(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	rec := env.do(t, http.MethodPost, "/api/tickets", `{"title":"Build it","team":"dev","assignee":"dev1@dev"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ticket: %d %s", rec.Code, rec.Body)
	}

	var ticket work.Ticket
	decode(t, rec, &ticket)

	if ticket.Status != work.StatusInProgress {
		t.Fatalf("a ticket created with an assignee is %s, want in_progress", ticket.Status)
	}

	for _, body := range []string{
		`{"session_id":"s1","cost":{"total_cost_usd":0},"model":{"id":"haiku"}}`,
		`{"session_id":"s1","cost":{"total_cost_usd":0.25}}`,
		`{"session_id":"s1","cost":{"total_cost_usd":0.40}}`,
	} {
		if rec := env.do(t, http.MethodPost, "/internal/agents/dev1@dev/statusline", body); rec.Code != http.StatusNoContent {
			t.Fatalf("statusline: %d %s", rec.Code, rec.Body)
		}
	}

	var summary budget.Summary
	decode(t, env.do(t, http.MethodGet, "/api/spend", ""), &summary)

	if math.Abs(summary.TotalUSD-0.40) > 1e-9 || len(summary.Agents) != 1 || summary.Agents[0].Key != "dev1@dev" ||
		len(summary.Teams) != 1 || summary.Teams[0].Key != "dev" {
		t.Fatalf("spend = %+v, want 0.40 by dev1@dev of dev", summary)
	}

	var details work.Details
	decode(t, env.do(t, http.MethodGet, "/api/tickets/"+ticket.ID, ""), &details)

	if math.Abs(details.Ticket.SpentUSD-0.40) > 1e-9 {
		t.Fatalf("ticket spent_usd = %v, want 0.40", details.Ticket.SpentUSD)
	}
}

func TestStatusLineErrors(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	tests := []struct {
		name, path, body string
		want             int
	}{
		{"unknown agent", "/internal/agents/ghost@dev/statusline", `{"session_id":"s","cost":{"total_cost_usd":1}}`, http.StatusNotFound},
		{"no cost", "/internal/agents/dev1@dev/statusline", `{"session_id":"s"}`, http.StatusBadRequest},
		{"no session", "/internal/agents/dev1@dev/statusline", `{"cost":{"total_cost_usd":1}}`, http.StatusBadRequest},
		{"bad month", "/api/spend?month=2026-13", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := http.MethodPost
			if tt.body == "" {
				method = http.MethodGet
			}

			if rec := env.do(t, method, tt.path, tt.body); rec.Code != tt.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}

// The budget settings are read and replaced through /api/budgets, and every
// cap shows as a gauge in /api/spend.
func TestBudgetsAPI(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	var settings budget.Settings
	decode(t, env.do(t, http.MethodGet, "/api/budgets", ""), &settings)

	if !settings.Enabled || settings.Threshold != budget.DefaultThreshold {
		t.Fatalf("defaults = %+v", settings)
	}

	if rec := env.do(t, http.MethodPut, "/api/budgets", `{"enabled":true,"threshold":80,"teams":{"ops":{"limit_usd":5}}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a cap on an unknown team: %d %s, want 400", rec.Code, rec.Body)
	}

	rec := env.do(t, http.MethodPut, "/api/budgets", `{"enabled":true,"threshold":75,"teams":{"dev":{"limit_usd":5,"monthly":true}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/budgets: %d %s", rec.Code, rec.Body)
	}

	if rec := env.do(t, http.MethodPost, "/internal/agents/dev1@dev/statusline", `{"session_id":"s1","cost":{"total_cost_usd":4}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("statusline: %d %s", rec.Code, rec.Body)
	}

	var summary budget.Summary
	decode(t, env.do(t, http.MethodGet, "/api/spend", ""), &summary)

	if len(summary.Gauges) != 1 {
		t.Fatalf("gauges = %+v, want the dev team's", summary.Gauges)
	}

	g := summary.Gauges[0]
	if g.Scope != (budget.Scope{Kind: budget.KindTeam, ID: "dev"}) || g.SpentUSD != 4 || g.Threshold != 75 || !g.Reached() {
		t.Fatalf("dev's gauge = %+v, want 4 of 5 at 75 %%, reached", g)
	}
}

func TestUpdateTicketBudget(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	var ticket work.Ticket
	decode(t, env.do(t, http.MethodPost, "/api/tickets", `{"title":"Build it","team":"dev","budget_usd":2}`), &ticket)

	if ticket.USD == nil || *ticket.USD != 2 {
		t.Fatalf("created ticket budget = %v, want 2", ticket.USD)
	}

	rec := env.do(t, http.MethodPatch, "/api/tickets/"+ticket.ID, `{"budget_usd":3,"threshold":50}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}

	decode(t, rec, &ticket)

	if ticket.USD == nil || *ticket.USD != 3 || ticket.Threshold == nil || *ticket.Threshold != 50 {
		t.Fatalf("ticket after PATCH = %+v", ticket)
	}

	if rec := env.do(t, http.MethodPatch, "/api/tickets/"+ticket.ID, `{"budget":3}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d, want 400", rec.Code)
	}
}
