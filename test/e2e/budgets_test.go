package e2e

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// Costs and budgets (ADR-0013) through the real daemon, hooks, statusLine
// relay and PTYs. fakeclaude reports its running cost to its statusLine
// and spends only when told ("SPEND <usd>" in a request, or in a ticket's
// title, which then stays in progress); it answers a Stop refused for a
// budget with budget_request, and, as the director, passes requests on with
// budget_submit. The daemon's clock is manual, for the monthly reset.

// budgetOrg: team dev (dev1, dev2) and the director god.
func budgetOrg(t *testing.T) *org.Org {
	t.Helper()

	return &org.Org{
		ProjectDir: t.TempDir(),
		Teams:      []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}, {Name: "dev2", Role: "Developer"}}}},
		God:        &org.Agent{Name: "god", Role: "Director"},
	}
}

// startBudgetDaemon is startDaemon with a manual clock.
func startBudgetDaemon(t *testing.T, o *org.Org) (*daemon.Daemon, *schedule.ManualClock) {
	t.Helper()

	clock := schedule.NewManualClock(time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local))
	d := startDaemonWithOptions(t, o, ":memory:", func(opts *daemon.Options) { opts.Clock = clock })

	return d, clock
}

func spendSummary(t *testing.T, d *daemon.Daemon) budget.Summary {
	t.Helper()

	var s budget.Summary
	decodeBody(t, call(t, d, http.MethodGet, "/api/spend?month=2026-11", "", http.StatusOK), &s)

	return s
}

// requestOf returns the latest request of a budget, if any.
func requestOf(t *testing.T, d *daemon.Daemon, scope budget.Scope) (budget.Request, bool) {
	t.Helper()

	var requests []budget.Request
	decodeBody(t, call(t, d, http.MethodGet, "/api/budget-requests", "", http.StatusOK), &requests)

	for _, r := range requests {
		if r.Scope == scope {
			return r, true // newest first
		}
	}

	return budget.Request{}, false
}

// waitForHuman waits until the request of a budget waits for the human: it
// was justified and passed on by the director.
func waitForHuman(t *testing.T, d *daemon.Daemon, scope budget.Scope) budget.Request {
	t.Helper()

	var r budget.Request

	eventually(t, fmt.Sprintf("the request of %s with the human", scope), func() bool {
		var ok bool

		r, ok = requestOf(t, d, scope)

		return ok && r.Status == budget.RequestAwaitingHuman
	})

	return r
}

func waitForStatus(t *testing.T, d *daemon.Daemon, agent string, status runtime.Status) {
	t.Helper()

	eventually(t, fmt.Sprintf("%s %s", agent, status), func() bool { return d.Statuses.Get(agent) == status })
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// What an agent spends on its ticket shows for the agent, its team, the
// ticket and the month, read from the statusLine through the real relay.
func TestSpendIsMeasuredFromTheStatusLine(t *testing.T) {
	t.Parallel()

	d, _ := startBudgetDaemon(t, budgetOrg(t))

	tk, err := d.Tickets.Create(context.Background(), org.HumanAddress, work.Draft{Title: "Build SPEND 0.30", Team: "dev", Assignee: "dev1@dev"})
	if err != nil {
		t.Fatal(err)
	}

	eventually(t, "the ticket's spend", func() bool {
		got, err := d.Store.GetTicket(context.Background(), tk.ID)

		return err == nil && near(got.SpentUSD, 0.30)
	})

	s := spendSummary(t, d)
	if !near(s.TotalUSD, 0.30) || len(s.Agents) != 1 || s.Agents[0].Key != "dev1@dev" || len(s.Teams) != 1 || s.Teams[0].Key != "dev" {
		t.Fatalf("spend = %+v, want $0.30 by dev1@dev of dev", s)
	}
}

// A fork answering the human's urgent question costs the staff, never the
// agent forked.
func TestForkCostIsChargedToTheStaffForTheHuman(t *testing.T) {
	t.Parallel()

	d, _ := startBudgetDaemon(t, budgetOrg(t))
	sendAsHuman(t, d, "dev1@dev", "Busy", "WORK 3")
	eventually(t, "dev1 busy", func() bool { return d.Statuses.Busy("dev1@dev") })

	if _, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: "dev1@dev", Act: messaging.ActQuery, Priority: messaging.Urgent, Subject: "DB?", Body: "Which database?",
	}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the fork's cost", func() bool { return near(spendSummary(t, d).TotalUSD, 0.01) })

	if s := spendSummary(t, d); len(s.Agents) != 0 || len(s.Teams) != 0 {
		t.Fatalf("spend = %+v, want the fork on the staff only", s)
	}
}

// A ticket at its threshold: dev1 finishes its turn, its Stop is refused,
// it asks for an extension, the director passes it on, the human extends,
// dev1 starts again.
func TestTicketBudgetExtended(t *testing.T) {
	t.Parallel()

	d, _ := startBudgetDaemon(t, budgetOrg(t))

	tk, err := d.Tickets.Create(context.Background(), org.HumanAddress, work.Draft{
		Title: "Build SPEND 0.45", Team: "dev", Assignee: "dev1@dev", Budget: work.Budget{USD: new(0.5)},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := waitForHuman(t, d, budget.Scope{Kind: budget.KindTicket, ID: tk.ID})

	if r.RequestedBy != "dev1@dev" || r.Justification == "" || r.DirectorOpinion == "" || r.CardMessage == "" {
		t.Fatalf("request = %+v, want dev1's justification and the director's opinion", r)
	}

	waitForStatus(t, d, "dev1@dev", runtime.StatusBudgetHold)

	call(t, d, http.MethodPost, "/api/budget-requests/"+r.ID+"/extend", `{"amount_usd":1}`, http.StatusOK)
	waitForStatus(t, d, "dev1@dev", runtime.StatusIdle)

	got, err := d.Store.GetTicket(context.Background(), tk.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.USD == nil || !near(*got.USD, 1.5) {
		t.Fatalf("ticket budget = %v, want 1.5", got.USD)
	}
}

// Refused with a commit turn: dev1 works one more turn, then its ticket goes
// back to the queue and it is free.
func TestBudgetRefusedAfterACommitTurn(t *testing.T) {
	t.Parallel()

	d, _ := startBudgetDaemon(t, budgetOrg(t))

	tk, err := d.Tickets.Create(context.Background(), org.HumanAddress, work.Draft{
		Title: "Build SPEND 0.45", Team: "dev", Assignee: "dev1@dev", Budget: work.Budget{USD: new(0.5)},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := waitForHuman(t, d, budget.Scope{Kind: budget.KindTicket, ID: tk.ID})
	waitForStatus(t, d, "dev1@dev", runtime.StatusBudgetHold)

	call(t, d, http.MethodPost, "/api/budget-requests/"+r.ID+"/refuse",
		`{"outcomes":{"dev1@dev":"release"},"commit_first":true,"reason":"Too dear."}`, http.StatusOK)

	eventually(t, "the ticket back in the queue", func() bool {
		got, err := d.Store.GetTicket(context.Background(), tk.ID)

		return err == nil && got.Status == work.StatusOpen && got.Assignee == ""
	})

	waitForStatus(t, d, "dev1@dev", runtime.StatusIdle)
}

// A team cap holds every member, the idle one at once; the monthly reset
// releases them.
func TestTeamCapUntilTheMonthlyReset(t *testing.T) {
	t.Parallel()

	d, clock := startBudgetDaemon(t, budgetOrg(t))

	call(t, d, http.MethodPut, "/api/budgets", `{"enabled":true,"threshold":80,"teams":{"dev":{"limit_usd":0.5,"monthly":true}}}`, http.StatusOK)
	sendAsHuman(t, d, "dev1@dev", "Spend", "SPEND 0.45")

	team := budget.Scope{Kind: budget.KindTeam, ID: "dev"}
	r := waitForHuman(t, d, team)

	if r.RequestedBy != "dev1@dev" {
		t.Fatalf("request = %+v, want it justified by dev1, the first to stop", r)
	}

	waitForStatus(t, d, "dev1@dev", runtime.StatusBudgetHold)
	waitForStatus(t, d, "dev2@dev", runtime.StatusBudgetHold)

	if d.Statuses.Get("god") == runtime.StatusBudgetHold {
		t.Fatal("the director was held")
	}

	clock.WaitForTimers(1) // the budgets' guard waits for the next month
	clock.Advance(14 * 24 * time.Hour)

	waitForStatus(t, d, "dev1@dev", runtime.StatusIdle)
	waitForStatus(t, d, "dev2@dev", runtime.StatusIdle)

	if got, _ := requestOf(t, d, team); got.Status != budget.RequestClosed || got.ClosedReason != "the monthly reset" {
		t.Fatalf("request after the month = %+v, want closed by the monthly reset", got)
	}
}

// The staff's cap is justified by the director; turning budgets off releases
// everyone.
func TestStaffCapAndBudgetsTurnedOff(t *testing.T) {
	t.Parallel()

	d, _ := startBudgetDaemon(t, budgetOrg(t))

	call(t, d, http.MethodPut, "/api/budgets", `{"enabled":true,"threshold":80,"staff":{"limit_usd":0.5}}`, http.StatusOK)
	sendAsHuman(t, d, "dev1@dev", "Spend", "SPEND 0.45")

	staff := budget.Scope{Kind: budget.KindStaff}
	r := waitForHuman(t, d, staff)

	if r.RequestedBy != "god" || r.Justification == "" {
		t.Fatalf("request = %+v, want the director's justification", r)
	}

	waitForStatus(t, d, "dev1@dev", runtime.StatusBudgetHold)
	waitForStatus(t, d, "dev2@dev", runtime.StatusBudgetHold)

	call(t, d, http.MethodPut, "/api/budgets", `{"enabled":false,"threshold":80,"staff":{"limit_usd":0.5}}`, http.StatusOK)

	waitForStatus(t, d, "dev1@dev", runtime.StatusIdle)
	waitForStatus(t, d, "dev2@dev", runtime.StatusIdle)

	if got, _ := requestOf(t, d, staff); got.Status != budget.RequestClosed {
		t.Fatalf("request with budgets off = %+v, want closed", got)
	}
}

// Inside a feature with a budget, a ticket needs one, at most what is left.
func TestFeatureBudgetIsAllocated(t *testing.T) {
	t.Parallel()

	o := budgetOrg(t)
	initGitProject(t, o.ProjectDir)

	d, _ := startBudgetDaemon(t, o)

	// Starting a feature restarts the teams, not the director (ADR-0005).
	teams := &org.Org{ProjectDir: o.ProjectDir, Teams: o.Teams}
	before := outputs(d, teams)

	call(t, d, http.MethodPost, "/api/features", `{"title":"Orders","budget_usd":1}`, http.StatusCreated)
	waitRestarted(t, d, teams, before)

	call(t, d, http.MethodPost, "/api/tickets", `{"title":"Without a budget","team":"dev"}`, http.StatusBadRequest)
	call(t, d, http.MethodPost, "/api/tickets", `{"title":"Spec","team":"dev","budget_usd":0.8}`, http.StatusCreated)
	call(t, d, http.MethodPost, "/api/tickets", `{"title":"Code","team":"dev","budget_usd":0.5}`, http.StatusConflict)
}
