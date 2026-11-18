package daemon

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// budgetTest is the budgets' guard on a real store, without agents: the test
// sets their statuses and records what they spend.
type budgetTest struct {
	t        *testing.T
	store    *store.Store
	statuses *runtime.Statuses
	book     *budget.Book
	guard    *budgetGuard
	tickets  *work.Service
	clock    *schedule.ManualClock
	office   *budgetOffice
}

// newBudgetTest: team dev (dev1, dev2), the director god.
func newBudgetTest(t *testing.T) *budgetTest {
	t.Helper()

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	o := &org.Org{
		ProjectDir: t.TempDir(),
		Teams:      []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1"}, {Name: "dev2"}}}},
		God:        &org.Agent{Name: "god"},
	}

	book, err := budget.OpenBook(o.ProjectDir, o, nil)
	if err != nil {
		t.Fatal(err)
	}

	statuses := runtime.NewStatuses()
	clock := schedule.NewManualClock(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))
	guard := newBudgetGuard(book, st, o, statuses, clock, events.NewBus(), slog.New(slog.DiscardHandler))

	for _, a := range []string{"dev1@dev", "dev2@dev", "god"} {
		statuses.Set(a, runtime.StatusIdle)
	}

	tickets := &work.Service{Store: st, Org: o, CheckClaim: guard.checkClaim}
	messages := &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}}
	office := newBudgetOffice(&budgets{book: book, guard: guard}, st, tickets, messages,
		Options{Org: o, Clock: clock, Logger: slog.New(slog.DiscardHandler)}, events.NewBus())

	return &budgetTest{
		t: t, store: st, statuses: statuses, book: book, guard: guard, clock: clock, tickets: tickets, office: office,
	}
}

// mail returns the subjects of the mail waiting for an agent.
func (b *budgetTest) mail(recipient string) []string {
	b.t.Helper()

	pending, err := b.store.PendingMessages(context.Background(), recipient, messaging.Normal)
	if err != nil {
		b.t.Fatal(err)
	}

	var subjects []string
	for _, m := range pending {
		subjects = append(subjects, m.Subject)
	}

	return subjects
}

func hasSubject(subjects []string, part string) bool {
	return slices.ContainsFunc(subjects, func(s string) bool { return strings.Contains(s, part) })
}

func (b *budgetTest) spend(agent, ticket string, usd float64) {
	b.t.Helper()

	_, err := b.store.InsertSpend(context.Background(), budget.Record{
		At: b.clock.Now(), Month: budget.Month(b.clock.Now()), Source: budget.SourceStatusLine, AmountUSD: usd,
		Agent: agent, Team: org.TeamOf(agent), Ticket: ticket,
	})
	if err != nil {
		b.t.Fatal(err)
	}

	b.reconcile()
}

func (b *budgetTest) reconcile() {
	b.t.Helper()

	if err := b.guard.reconcile(context.Background()); err != nil {
		b.t.Fatal(err)
	}
}

func (b *budgetTest) request(scope budget.Scope) budget.Request {
	b.t.Helper()

	requests, err := b.store.ListBudgetRequests(context.Background(), store.BudgetRequestFilter{})
	if err != nil {
		b.t.Fatal(err)
	}

	for _, r := range requests {
		if r.Scope == scope {
			return r // newest first
		}
	}

	b.t.Fatalf("no request for %s", scope)

	return budget.Request{}
}

// stop plays the agent's Stop hook, as the server does.
func (b *budgetTest) stop(agent string) (*hook.Response, bool) { //nolint:unparam // any agent may stop; the tests use dev1
	b.t.Helper()

	response, held, err := b.guard.atStop(context.Background(), agent)
	if err != nil {
		b.t.Fatal(err)
	}

	if held && response == nil {
		b.statuses.ObserveHeldStop(agent, hook.Event{Name: hook.EventStop})
	}

	return response, held
}

// A ticket at its threshold: its agent finishes its turn, is asked once to
// justify an extension, then holds; raising the budget releases it.
func TestTicketBudgetHoldsItsAgent(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)
	ctx := context.Background()

	tk, err := b.tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Build", Team: "dev", Assignee: "dev1@dev", Budget: work.Budget{USD: new(1.0)}})
	if err != nil {
		t.Fatal(err)
	}

	b.statuses.Set("dev1@dev", runtime.StatusWorking)
	b.spend("dev1@dev", tk.ID, 0.5)

	if b.guard.isHeld("dev1@dev") {
		t.Fatal("held at 50 %")
	}

	b.spend("dev1@dev", tk.ID, 0.3) // 80 %

	scope := budget.Scope{Kind: budget.KindTicket, ID: tk.ID}
	if r := b.request(scope); r.Status != budget.RequestAwaitingAgent || !r.Active {
		t.Fatalf("request = %+v, want active, awaiting its agent", r)
	}

	if st := b.statuses.Get("dev1@dev"); st != runtime.StatusWorking {
		t.Fatalf("an agent in its turn finishes it: status %s", st)
	}

	response, held := b.stop("dev1@dev")
	if !held || response == nil || response.Decision != "block" || !strings.Contains(response.Reason, "budget_request") {
		t.Fatalf("first Stop: held %v, response %+v; want the end refused, asking for budget_request", held, response)
	}

	if response, held := b.stop("dev1@dev"); !held || response != nil {
		t.Fatalf("second Stop: held %v, response %+v; want a plain hold", held, response)
	}

	if st := b.statuses.Get("dev1@dev"); st != runtime.StatusBudgetHold {
		t.Fatalf("status %s, want budget_hold", st)
	}

	if r := b.request(scope); r.Status != budget.RequestAwaitingDirector || r.AskedAgent != "dev1@dev" {
		t.Fatalf("request = %+v, want it with the director, unjustified", r)
	}

	if _, err := b.tickets.Claim(ctx, "dev1@dev", ""); !errors.Is(err, work.ErrConflict) && !errors.Is(err, work.ErrNoTicket) {
		t.Fatalf("a held agent claimed: %v", err)
	}

	// The human raises the budget: the agent is released, the request closed.
	if _, err := b.tickets.SetBudget(ctx, org.HumanAddress, tk.ID, work.Budget{USD: new(5.0)}); err != nil {
		t.Fatal(err)
	}

	b.reconcile()

	if st := b.statuses.Get("dev1@dev"); st != runtime.StatusIdle {
		t.Fatalf("status %s after the budget was raised, want idle", st)
	}

	if r := b.request(scope); r.Active || r.Status != budget.RequestClosed {
		t.Fatalf("request = %+v, want closed and inactive", r)
	}
}

// A team cap holds every member, idle ones at once; nobody is in a turn to
// justify it, so the director does. The director is never held.
func TestTeamAndStaffCaps(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)

	if _, err := b.book.Replace(budget.Settings{
		Enabled: true, Threshold: 80,
		Staff: &budget.Cap{LimitUSD: 100},
		Teams: map[string]budget.Cap{"dev": {LimitUSD: 1, Monthly: true}},
	}); err != nil {
		t.Fatal(err)
	}

	b.spend("dev1@dev", "", 0.9)

	for _, a := range []string{"dev1@dev", "dev2@dev"} {
		if st := b.statuses.Get(a); st != runtime.StatusBudgetHold {
			t.Fatalf("%s is %s, want budget_hold", a, st)
		}
	}

	team := budget.Scope{Kind: budget.KindTeam, ID: "dev"}
	if r := b.request(team); r.Status != budget.RequestAwaitingDirector {
		t.Fatalf("team request = %+v, want it with the director: nobody was in a turn", r)
	}

	// The director spends past the staff's threshold: others stay held, it
	// does not.
	b.spend("god", "", 90)

	if b.guard.isHeld("god") || b.statuses.Get("god") != runtime.StatusIdle {
		t.Fatal("the director was held")
	}

	if r := b.request(budget.Scope{Kind: budget.KindStaff}); r.Status != budget.RequestAwaitingDirector {
		t.Fatalf("staff request = %+v, want it with the director", r)
	}

	// A new month: the monthly team cap starts again, the cumulative staff
	// cap does not.
	b.clock.Advance(14 * 24 * time.Hour)
	b.reconcile()

	if r := b.request(team); r.Active || r.ClosedReason != "the monthly reset" {
		t.Fatalf("team request after the month = %+v, want closed by the monthly reset", r)
	}

	if !b.guard.isHeld("dev1@dev") {
		t.Fatal("the cumulative staff cap no longer holds dev1")
	}

	// Turning budgets off releases everyone.
	s := b.book.Get()
	s.Enabled = false

	if _, err := b.book.Replace(s); err != nil {
		t.Fatal(err)
	}

	b.reconcile()

	for _, a := range []string{"dev1@dev", "dev2@dev"} {
		if st := b.statuses.Get(a); st != runtime.StatusIdle {
			t.Fatalf("%s is %s with budgets off, want idle", a, st)
		}
	}

	if r := b.request(budget.Scope{Kind: budget.KindStaff}); r.Status != budget.RequestClosed || r.ClosedReason != "budgets were turned off" {
		t.Fatalf("staff request = %+v, want closed: budgets turned off", r)
	}
}

// A ticket whose budget is reached cannot be claimed again once given back.
func TestClaimOfAReachedTicket(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)
	ctx := context.Background()

	tk, err := b.tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Build", Team: "dev", Assignee: "dev1@dev", Budget: work.Budget{USD: new(1.0)}})
	if err != nil {
		t.Fatal(err)
	}

	b.spend("dev1@dev", tk.ID, 0.9)

	if _, err := b.tickets.SetStatus(ctx, org.HumanAddress, tk.ID, work.StatusOpen); err != nil {
		t.Fatal(err)
	}

	b.reconcile()

	if _, err := b.tickets.Claim(ctx, "dev2@dev", tk.ID); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("claim of a ticket at its threshold: %v, want ErrConflict", err)
	}
}

func TestUntilNextMonth(t *testing.T) { //nolint:paralleltest // changes time.Local
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tzdata:", err)
	}

	saved := time.Local
	time.Local = paris

	t.Cleanup(func() { time.Local = saved })

	now := time.Date(2026, 11, 30, 22, 0, 0, 0, time.UTC) // 23:00 in Paris
	if got := untilNextMonth(now); got != time.Hour {
		t.Fatalf("untilNextMonth = %v, want 1h (midnight in Paris)", got)
	}
}

// The whole way of a ticket's request: the agent justifies it, the director
// passes it on, the human extends it; too small an extension is refused.
func TestBudgetRequestExtended(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)
	ctx := context.Background()

	tk, err := b.tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Build", Team: "dev", Assignee: "dev1@dev", Budget: work.Budget{USD: new(1.0)}})
	if err != nil {
		t.Fatal(err)
	}

	b.statuses.Set("dev1@dev", runtime.StatusWorking)
	b.spend("dev1@dev", tk.ID, 0.9)

	if response, _ := b.stop("dev1@dev"); response == nil {
		t.Fatal("dev1 was not asked to justify the request")
	}

	r, err := b.office.RequestExtension(ctx, "dev1@dev", "", 2, "Half done: the tests remain.")
	if err != nil {
		t.Fatal(err)
	}

	if _, held := b.stop("dev1@dev"); !held || b.statuses.Get("dev1@dev") != runtime.StatusBudgetHold {
		t.Fatal("dev1 is not on hold after justifying")
	}

	if !hasSubject(b.mail("god"), "Budget request "+r.ID) {
		t.Fatalf("the director's mail %v misses the request", b.mail("god"))
	}

	if _, err := b.office.Extend(ctx, r.ID, 5); !errors.Is(err, budget.ErrConflict) {
		t.Fatalf("an extension before the director passed it on: %v, want ErrConflict", err)
	}

	r, err = b.office.Submit(ctx, "god", r.ID, budget.Submission{Opinion: "Worth it.", RecommendedUSD: new(2.0)})
	if err != nil {
		t.Fatal(err)
	}

	if r.Status != budget.RequestAwaitingHuman || r.CardMessage == "" || !hasSubject(b.mail(org.HumanAddress), "Budget request "+r.ID) {
		t.Fatalf("after budget_submit: %+v; the human's mail %v", r, b.mail(org.HumanAddress))
	}

	// 0.9 spent: under 80 % needs a budget above 1.125.
	if _, err := b.office.Extend(ctx, r.ID, 0.1); !errors.Is(err, budget.ErrConflict) || !strings.Contains(err.Error(), "at least $0.14") {
		t.Fatalf("too small an extension: %v, want ErrConflict naming at least $0.14", err)
	}

	if r, err = b.office.Extend(ctx, r.ID, 2); err != nil {
		t.Fatal(err)
	}

	if r.Status != budget.RequestExtended || b.statuses.Get("dev1@dev") != runtime.StatusIdle {
		t.Fatalf("after the extension: request %s, dev1 %s; want extended, idle", r.Status, b.statuses.Get("dev1@dev"))
	}

	if !hasSubject(b.mail("dev1@dev"), "Budget extended") || !hasSubject(b.mail("god"), "extended by $2.00") {
		t.Fatalf("notices: dev1 %v, director %v", b.mail("dev1@dev"), b.mail("god"))
	}

	got, err := b.store.GetTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}

	if *got.USD != 3 {
		t.Fatalf("ticket budget %v, want 3", *got.USD)
	}
}

// A refusal with a commit turn: the agent works one more turn, then its
// ticket goes back to the queue and it is free.
func TestBudgetRequestRefusedAfterACommitTurn(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)
	ctx := context.Background()

	tk, err := b.tickets.Create(ctx, org.HumanAddress, work.Draft{Title: "Build", Team: "dev", Assignee: "dev1@dev", Budget: work.Budget{USD: new(1.0)}})
	if err != nil {
		t.Fatal(err)
	}

	b.spend("dev1@dev", tk.ID, 0.9) // dev1 is idle: held at once, the director explains it

	r := b.request(budget.Scope{Kind: budget.KindTicket, ID: tk.ID})
	if r.Status != budget.RequestAwaitingDirector {
		t.Fatalf("request %+v, want it with the director", r)
	}

	if _, err := b.office.Submit(ctx, "god", r.ID, budget.Submission{Opinion: "No."}); !errors.Is(err, budget.ErrInvalid) {
		t.Fatalf("an unjustified request passed on without a justification: %v, want ErrInvalid", err)
	}

	if _, err := b.office.Submit(ctx, "god", r.ID, budget.Submission{Opinion: "No.", Justification: "Stuck.", AmountUSD: new(1.0)}); err != nil {
		t.Fatal(err)
	}

	if _, err := b.office.Refuse(ctx, r.ID, budget.Refusal{Outcomes: map[string]budget.Outcome{}}); !errors.Is(err, budget.ErrInvalid) {
		t.Fatalf("a refusal without dev1's outcome: %v, want ErrInvalid", err)
	}

	refusal := budget.Refusal{Outcomes: map[string]budget.Outcome{"dev1@dev": budget.OutcomeRelease}, CommitFirst: true, Reason: "Too dear."}
	if _, err := b.office.Refuse(ctx, r.ID, refusal); err != nil {
		t.Fatal(err)
	}

	if st := b.statuses.Get("dev1@dev"); st != runtime.StatusIdle || !hasSubject(b.mail("dev1@dev"), "commit your work") {
		t.Fatalf("dev1 is %s with mail %v; want it released for its commit turn", st, b.mail("dev1@dev"))
	}

	b.statuses.Set("dev1@dev", runtime.StatusWorking) // the commit turn

	if _, held := b.stop("dev1@dev"); held {
		t.Fatal("dev1 is held after its commit turn, its ticket given back")
	}

	got, err := b.store.GetTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Status != work.StatusOpen || got.Assignee != "" {
		t.Fatalf("ticket %s assigned to %q, want open in the queue", got.Status, got.Assignee)
	}
}

// Extending a cumulative cap writes it; a monthly cap gets an extension for
// the month only.
func TestCapExtensions(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)
	ctx := context.Background()

	if _, err := b.book.Replace(budget.Settings{
		Enabled: true, Threshold: 80,
		Staff: &budget.Cap{LimitUSD: 10},
		Teams: map[string]budget.Cap{"dev": {LimitUSD: 1, Monthly: true}},
	}); err != nil {
		t.Fatal(err)
	}

	b.spend("dev1@dev", "", 0.9)

	team := b.request(budget.Scope{Kind: budget.KindTeam, ID: "dev"})
	submit := budget.Submission{Opinion: "Fine.", Justification: "Busy month.", AmountUSD: new(1.0)}

	if _, err := b.office.Submit(ctx, "god", team.ID, submit); err != nil {
		t.Fatal(err)
	}

	if _, err := b.office.Extend(ctx, team.ID, 1); err != nil {
		t.Fatal(err)
	}

	if b.book.Get().Teams["dev"].LimitUSD != 1 {
		t.Fatal("a monthly extension changed budgets.yaml")
	}

	if b.guard.isHeld("dev1@dev") {
		t.Fatal("dev1 still held after the team's monthly extension")
	}

	b.spend("dev2@dev", "", 7.5) // the staff: 8.4 of 10

	staff := b.request(budget.Scope{Kind: budget.KindStaff})
	if _, err := b.office.Submit(ctx, "god", staff.ID, submit); err != nil {
		t.Fatal(err)
	}

	if _, err := b.office.Extend(ctx, staff.ID, 5); err != nil {
		t.Fatal(err)
	}

	if got := b.book.Get().Staff.LimitUSD; got != 15 {
		t.Fatalf("staff cap %v, want 15 written", got)
	}
}

// The director proposes caps; the human accepts one edited, refuses another.
func TestCapProposals(t *testing.T) {
	t.Parallel()

	b := newBudgetTest(t)
	ctx := context.Background()

	if _, err := b.office.Propose(ctx, "god", budget.Proposal{Scope: budget.Scope{Kind: budget.KindAgent, ID: "god"}, Cap: budget.Cap{LimitUSD: 5}}); !errors.Is(err, budget.ErrInvalid) {
		t.Fatalf("a cap on the director: %v, want ErrInvalid", err)
	}

	if _, err := b.office.Propose(ctx, "dev1@dev", budget.Proposal{Scope: budget.Scope{Kind: budget.KindStaff}, Cap: budget.Cap{LimitUSD: 5}}); err == nil {
		t.Fatal("a team agent proposed a cap")
	}

	p, err := b.office.Propose(ctx, "god", budget.Proposal{
		Scope: budget.Scope{Kind: budget.KindTeam, ID: "dev"}, Cap: budget.Cap{LimitUSD: 20, Monthly: true}, Reason: "A guard.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !hasSubject(b.mail(org.HumanAddress), "Cap proposal "+p.ID) {
		t.Fatalf("the human's mail %v misses the proposal", b.mail(org.HumanAddress))
	}

	if _, err := b.office.AcceptProposal(ctx, p.ID, &budget.Cap{LimitUSD: 15, Monthly: true}); err != nil {
		t.Fatal(err)
	}

	if got := b.book.Get().Teams["dev"]; got.LimitUSD != 15 || !got.Monthly {
		t.Fatalf("team cap %+v, want the human's 15 a month", got)
	}

	if _, err := b.office.AcceptProposal(ctx, p.ID, nil); !errors.Is(err, budget.ErrConflict) {
		t.Fatalf("accepted twice: %v, want ErrConflict", err)
	}

	q, err := b.office.Propose(ctx, "god", budget.Proposal{Scope: budget.Scope{Kind: budget.KindStaff}, Cap: budget.Cap{LimitUSD: 50}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := b.office.RefuseProposal(ctx, q.ID, "Not yet."); err != nil {
		t.Fatal(err)
	}

	if b.book.Get().Staff != nil || !hasSubject(b.mail("god"), "refused") {
		t.Fatalf("after the refusal: staff cap %+v, director's mail %v", b.book.Get().Staff, b.mail("god"))
	}
}
