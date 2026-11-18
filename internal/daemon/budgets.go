package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/hook"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// budgets are the project's budget settings and the guard that holds the
// agents to them.
type budgets struct {
	book  *budget.Book
	guard *budgetGuard
	// stop ends the guard's loop; done is closed once it has.
	stop context.CancelFunc
	done chan struct{}
}

// wireBudgets reads .nobody/budgets.yaml, works out the holds once (they
// are not stored, see budgetGuard), and starts the guard's loop. Every
// change of the file is announced and reconciled.
func wireBudgets(ctx context.Context, opts Options, st *store.Store, statuses *runtime.Statuses, bus *events.Bus) (*budgets, error) {
	var guard *budgetGuard

	book, err := budget.OpenBook(opts.Org.Current().ProjectDir, opts.Org, func(budget.Settings) {
		bus.Publish(events.Event{Type: events.BudgetsChanged, Data: events.Empty{}})
		guard.trigger()
	})
	if err != nil {
		return nil, err
	}

	guard = newBudgetGuard(book, st, opts.Org, statuses, clockOf(opts), bus, opts.Logger)
	if err := guard.reconcile(ctx); err != nil {
		return nil, err
	}

	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	context.AfterFunc(ctx, stop)

	done := make(chan struct{})

	go func() {
		defer close(done)

		guard.run(runCtx)
	}()

	return &budgets{book: book, guard: guard, stop: stop, done: done}, nil
}

// clockOf is the daemon's clock: the machine's, or the test's.
func clockOf(opts Options) schedule.Clock {
	if opts.Clock == nil {
		return schedule.RealClock{}
	}

	return opts.Clock
}

// budgetGuard holds agents to their budgets (ADR-0013, points 4 and 5).
//
// A hold is not stored: an agent is on hold while a budget that covers it is
// reached (budget.Gauges, budget.Covered), so anything that moves a budget
// away from its threshold (an extension, a monthly reset, a budget raised
// or removed, budgets turned off) releases its agents by itself. reconcile
// works this out again after every spend record, budget change, ticket or
// feature change, and at the start of every month:
//
//   - a budget newly reached gets a request, the one request of that budget
//     while it stays reached: awaiting its first agent's justification
//     (ticket, agent, team), or the director's (staff, feature);
//   - a request whose budget is no longer reached stops being active, and
//     is closed if nobody decided yet;
//   - an idle agent newly held goes on budget hold at once; one in the
//     middle of a turn finishes it, and its Stop is handled by atStop;
//   - an agent no longer held becomes idle again, which hands it its mail
//     and its work (Statuses.OnIdle).
//
// The director is never held; its spend still counts for the staff.
type budgetGuard struct {
	book     *budget.Book
	store    *store.Store
	org      org.Source
	statuses *runtime.Statuses
	clock    schedule.Clock
	bus      *events.Bus
	logger   *slog.Logger

	// mu serializes reconcile and atStop, and guards held and reached.
	mu sync.Mutex
	// held lists, by agent, the reached budgets that cover it.
	held map[string][]budget.Scope
	// reached are the budgets at their threshold.
	reached map[budget.Scope]budget.Gauge

	// wake asks the loop (run) for a reconcile; it holds at most one.
	wake chan struct{}

	// commits are the agents released for one turn to commit their work in
	// progress after a refusal; the outcome applies at the end of the turn
	// (ADR-0013, point 5). Kept in memory: a daemon restart drops them, and
	// the agents stay held.
	commits map[string]commitTurn
	// toDirector are the requests that just went to the director, told
	// outside the lock by onAwaitingDirector.
	toDirector []budget.Request

	// onAwaitingDirector, when set, tells the director a request waits for
	// it (budgetOffice). afterCommit applies a refusal's outcome once the
	// agent's commit turn is over.
	onAwaitingDirector func(ctx context.Context, r budget.Request)
	afterCommit        func(ctx context.Context, agent string, c commitTurn)
}

// commitTurn is what follows an agent's commit turn.
type commitTurn struct {
	request string
	ticket  string
	outcome budget.Outcome
}

func newBudgetGuard(book *budget.Book, st *store.Store, o org.Source, statuses *runtime.Statuses,
	clock schedule.Clock, bus *events.Bus, logger *slog.Logger,
) *budgetGuard {
	return &budgetGuard{
		book: book, store: st, org: o, statuses: statuses, clock: clock, bus: bus, logger: logger,
		held: map[string][]budget.Scope{}, reached: map[budget.Scope]budget.Gauge{}, wake: make(chan struct{}, 1),
		commits: map[string]commitTurn{},
	}
}

// trigger asks for a reconcile without waiting for it: callers may hold
// locks that reconcile's consequences (an agent going idle) need.
func (g *budgetGuard) trigger() {
	select {
	case g.wake <- struct{}{}:
	default: // one is already due
	}
}

// run reconciles when triggered, and when a month begins (monthly caps
// start again from 0), until ctx ends.
func (g *budgetGuard) run(ctx context.Context) {
	for {
		timer := g.clock.NewTimer(untilNextMonth(g.clock.Now()))

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-g.wake:
			timer.Stop()
		case <-timer.C():
		}

		if err := g.reconcile(ctx); err != nil && ctx.Err() == nil {
			g.logger.Error("hold agents to their budgets", "err", err)
		}
	}
}

// untilNextMonth is the time left until the first day of the next month, at
// midnight in the machine's time zone.
func untilNextMonth(now time.Time) time.Duration {
	local := now.In(time.Local)
	next := time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, time.Local)

	return next.Sub(now)
}

// held reports whether a budget holds an agent.
func (g *budgetGuard) isHeld(address string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.held[address]) > 0
}

// reconcile works out which budgets are reached and which agents they hold,
// updates the requests, then the agents' statuses.
func (g *budgetGuard) reconcile(ctx context.Context) error {
	g.mu.Lock()

	toHold, toRelease, changed, err := g.reconcileLocked(ctx)

	g.mu.Unlock()

	if err != nil {
		return err
	}

	// Outside the lock: an agent becoming idle gets its mail and work, which
	// asks the guard again.
	for _, a := range toHold {
		g.statuses.Set(a, runtime.StatusBudgetHold)
	}

	for _, a := range toRelease {
		g.logger.Info("budget hold lifted", "agent", a)
		g.statuses.Set(a, runtime.StatusIdle)
	}

	if changed {
		g.bus.Publish(events.Event{Type: events.BudgetsChanged, Data: events.Empty{}})
	}

	g.flushDirector(ctx)

	return nil
}

// flushDirector tells the director about the requests that just went to it.
func (g *budgetGuard) flushDirector(ctx context.Context) {
	g.mu.Lock()
	requests := g.toDirector
	g.toDirector = nil
	g.mu.Unlock()

	if g.onAwaitingDirector == nil {
		return
	}

	for _, r := range requests {
		g.onAwaitingDirector(ctx, r)
	}
}

// grantCommitTurn releases a held agent for one turn, to commit its work in
// progress; the outcome applies when that turn ends.
func (g *budgetGuard) grantCommitTurn(agent string, c commitTurn) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.commits[agent] = c
}

// takeCommitTurn ends an agent's commit turn, if it had one.
func (g *budgetGuard) takeCommitTurn(agent string) (commitTurn, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	c, ok := g.commits[agent]
	delete(g.commits, agent)

	return c, ok
}

// reconcileLocked does reconcile's work under g.mu; it returns the idle
// agents to put on hold, those to release, and whether a request changed.
func (g *budgetGuard) reconcileLocked(ctx context.Context) ([]string, []string, bool, error) {
	settings := g.book.Get()
	o := g.org.Current()

	gauges, err := budget.Evaluate(ctx, settings, g.store, g.clock.Now())
	if err != nil {
		return nil, nil, false, err
	}

	inProgress, err := g.store.InProgress(ctx)
	if err != nil {
		return nil, nil, false, err
	}

	reached := map[budget.Scope]budget.Gauge{}
	held := map[string][]budget.Scope{}

	var order []budget.Scope // gauge order: staff, teams, agents, features, tickets

	for _, gauge := range gauges {
		if !gauge.Held {
			continue
		}

		reached[gauge.Scope] = gauge
		order = append(order, gauge.Scope)

		for _, a := range budget.Covered(gauge.Scope, o, inProgress) {
			if _, committing := g.commits[a]; !committing {
				held[a] = append(held[a], gauge.Scope)
			}
		}
	}

	changed, err := g.updateRequests(ctx, settings, o, inProgress, reached, order)
	if err != nil {
		return nil, nil, false, err
	}

	g.held, g.reached = held, reached
	toHold, toRelease := g.statusChanges(o, held)

	return toHold, toRelease, changed, nil
}

// statusChanges lists the idle agents to put on hold and the agents on hold
// to release.
func (g *budgetGuard) statusChanges(o *org.Org, held map[string][]budget.Scope) ([]string, []string) {
	var toHold, toRelease []string

	for _, m := range o.Members() {
		switch st := g.statuses.Get(m.Address); {
		case len(held[m.Address]) > 0 && st == runtime.StatusIdle:
			toHold = append(toHold, m.Address)
		case len(held[m.Address]) == 0 && st == runtime.StatusBudgetHold:
			toRelease = append(toRelease, m.Address)
		}
	}

	return toHold, toRelease
}

// updateRequests ends the requests of budgets no longer reached, opens one
// for each budget newly reached, and hands a request nobody can justify
// any more to the director.
func (g *budgetGuard) updateRequests(ctx context.Context, settings budget.Settings, o *org.Org,
	inProgress []budget.Assignment, reached map[budget.Scope]budget.Gauge, order []budget.Scope,
) (bool, error) {
	open, changed, err := g.endRequests(ctx, settings, reached)
	if err != nil {
		return false, err
	}

	for _, scope := range order {
		updated, err := g.ensureRequest(ctx, open, reached[scope], budget.Covered(scope, o, inProgress))
		if err != nil {
			return false, err
		}

		changed = changed || updated
	}

	return changed, nil
}

// endRequests ends the active requests whose budget is no longer reached,
// and returns the others by budget.
func (g *budgetGuard) endRequests(
	ctx context.Context, settings budget.Settings, reached map[budget.Scope]budget.Gauge,
) (map[budget.Scope]budget.Request, bool, error) {
	active, err := g.store.ListBudgetRequests(ctx, store.BudgetRequestFilter{Active: true})
	if err != nil {
		return nil, false, err
	}

	changed := false
	open := map[budget.Scope]budget.Request{}

	for _, r := range active {
		if _, ok := reached[r.Scope]; ok {
			open[r.Scope] = r

			continue
		}

		r.Active = false
		if r.Status.Open() {
			r.Status, r.ClosedReason = budget.RequestClosed, closedReason(settings, r, g.clock.Now())
		}

		if err := g.store.UpdateBudgetRequest(ctx, r); err != nil {
			return nil, false, err
		}

		changed = true
	}

	return open, changed, nil
}

// ensureRequest opens the request of a reached budget that has none, and
// hands a request its agents can no longer justify to the director. It
// reports whether a request changed.
func (g *budgetGuard) ensureRequest(
	ctx context.Context, open map[budget.Scope]budget.Request, gauge budget.Gauge, covered []string,
) (bool, error) {
	r, exists := open[gauge.Scope]
	changed := false

	if !exists {
		if !gauge.Scope.Global() && len(covered) == 0 {
			return false, nil // nobody to hold: a ticket waiting in its queue
		}

		var err error
		if r, err = g.openRequest(ctx, gauge); err != nil {
			return false, err
		}

		changed = true
	}

	// Its agents stopped before being asked (the turn's last report came
	// after their Stop): the director explains it instead.
	if r.Status == budget.RequestAwaitingAgent && r.AskedAgent == "" && !slices.ContainsFunc(covered, g.inTurn) {
		r.Status = budget.RequestAwaitingDirector
		g.toDirector = append(g.toDirector, r)

		return true, g.store.UpdateBudgetRequest(ctx, r)
	}

	return changed, nil
}

// inTurn reports whether an agent is starting or in the middle of a turn:
// it will stop, and can be asked then.
func (g *budgetGuard) inTurn(address string) bool {
	return g.statuses.Busy(address) || g.statuses.Get(address) == runtime.StatusStarting
}

// openRequest records the request of a budget newly reached.
func (g *budgetGuard) openRequest(ctx context.Context, gauge budget.Gauge) (budget.Request, error) {
	status := budget.RequestAwaitingAgent
	if gauge.Scope.Global() {
		status = budget.RequestAwaitingDirector
	}

	r, err := g.store.InsertBudgetRequest(ctx, budget.Request{
		Scope: gauge.Scope, Status: status, Active: true,
		LimitUSD: gauge.LimitUSD, SpentUSD: gauge.SpentUSD, CreatedAt: g.clock.Now().UTC(),
	})
	if err != nil {
		return budget.Request{}, err
	}

	g.logger.Info("budget reached", "budget", gauge.Scope.String(), "request", r.ID,
		"spent_usd", gauge.SpentUSD, "limit_usd", gauge.LimitUSD, "threshold", gauge.Threshold)

	if status == budget.RequestAwaitingDirector {
		g.toDirector = append(g.toDirector, r)
	}

	return r, nil
}

// closedReason says why a request ended before anyone decided.
func closedReason(settings budget.Settings, r budget.Request, now time.Time) string {
	switch {
	case !settings.Enabled:
		return "budgets were turned off"
	case budget.Month(r.CreatedAt) != budget.Month(now):
		return "the monthly reset"
	default:
		return "the budget is no longer reached: raised, removed or its work finished"
	}
}

// atStop decides what a held agent's Stop gets (ADR-0013, point 4): the
// first agent to stop on a request awaiting its justification is asked once
// to call budget_request before ending its turn; any other held agent
// simply ends its turn on budget hold. It reports false for an agent no
// budget holds.
func (g *budgetGuard) atStop(ctx context.Context, address string) (*hook.Response, bool, error) {
	// A commit turn is over: its outcome applies now (the ticket canceled or
	// given back, or the agent kept on hold).
	if c, ok := g.takeCommitTurn(address); ok && g.afterCommit != nil {
		g.afterCommit(ctx, address, c)
	}

	// The turn's spend may have just arrived: decide on fresh figures.
	if err := g.reconcile(ctx); err != nil {
		return nil, false, err
	}

	response, held, err := g.decideStop(ctx, address)

	g.flushDirector(ctx)

	return response, held, err
}

// decideStop is atStop's decision, under g.mu.
func (g *budgetGuard) decideStop(ctx context.Context, address string) (*hook.Response, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	scopes := g.held[address]
	if len(scopes) == 0 {
		return nil, false, nil
	}

	active, err := g.store.ListBudgetRequests(ctx, store.BudgetRequestFilter{Active: true})
	if err != nil {
		return nil, false, err
	}

	for _, r := range active {
		if r.Status != budget.RequestAwaitingAgent || !slices.Contains(scopes, r.Scope) {
			continue
		}

		if response, asked, err := g.askOrPassOn(ctx, r, address); asked || err != nil {
			return response, err == nil, err
		}
	}

	return nil, true, nil
}

// askOrPassOn asks the first agent to stop on a request to justify it; when
// that agent stops again without answering, the request goes to the
// director unjustified. It reports whether the agent was asked.
func (g *budgetGuard) askOrPassOn(ctx context.Context, r budget.Request, address string) (*hook.Response, bool, error) {
	switch r.AskedAgent {
	case "":
		r.AskedAgent = address
		if err := g.store.UpdateBudgetRequest(ctx, r); err != nil {
			return nil, false, err
		}

		return budgetRequestPrompt(r, g.reached[r.Scope]), true, nil
	case address:
		r.Status = budget.RequestAwaitingDirector
		if err := g.store.UpdateBudgetRequest(ctx, r); err != nil {
			return nil, false, err
		}

		g.toDirector = append(g.toDirector, r)
		g.bus.Publish(events.Event{Type: events.BudgetsChanged, Data: events.Empty{}})
	}

	return nil, false, nil
}

// holding lists the agents a reached budget holds now.
func (g *budgetGuard) holding(scope budget.Scope) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	var agents []string

	for a, scopes := range g.held {
		if slices.Contains(scopes, scope) {
			agents = append(agents, a)
		}
	}

	slices.Sort(agents)

	return agents
}

// budgetRequestPrompt refuses the end of a turn once, and asks the agent to
// justify an extension.
func budgetRequestPrompt(r budget.Request, gauge budget.Gauge) *hook.Response {
	return &hook.Response{
		Decision: "block",
		Reason: fmt.Sprintf("Budget: %s has reached %d%% of its $%.2f budget ($%.2f spent). "+
			"Stop working on it now. Before ending your turn, call the budget_request tool (request %s) with the amount "+
			"you need to finish and a short justification: where you are, what remains. Then end your turn: you will stay "+
			"on hold, with this conversation kept, until the human decides.",
			r.Scope, gauge.Threshold, gauge.LimitUSD, gauge.SpentUSD, r.ID),
	}
}

// checkClaim refuses a claim to an agent on budget hold, and of a ticket
// whose budget, or its feature's, is reached.
func (g *budgetGuard) checkClaim(_ context.Context, agent string, t work.Ticket) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.held[agent]) > 0 {
		return fmt.Errorf("%w: you are on budget hold until the human decides on an extension", work.ErrConflict)
	}

	for _, scope := range []budget.Scope{{Kind: budget.KindTicket, ID: t.ID}, {Kind: budget.KindFeature, ID: t.Feature}} {
		if _, ok := g.reached[scope]; ok && scope.ID != "" {
			return fmt.Errorf("%w: the budget of %s is reached; it waits for the human's decision", work.ErrConflict, scope)
		}
	}

	return nil
}
