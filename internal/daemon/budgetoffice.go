package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/events"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// percent turns a threshold into a fraction, and dollars into cents.
const percent = 100

// budgetOffice carries budget requests and cap proposals between the
// agents, the director and the human (ADR-0013, points 5 to 8), in the
// organization's own protocol:
//
//   - a held agent justifies a request with budget_request; the request
//     reaches the director as a notice from nobody, as does a staff or
//     feature request, which the director justifies itself;
//   - the director passes it on with budget_submit: the human gets a
//     "propose" message from the director, shown as a card;
//   - the human extends or refuses; the director gets "agree" or "refuse",
//     the held agents a notice from nobody.
//
// The director grants nothing: no tool changes a budget. Its cap proposals
// work like its schedule proposals: only the human's acceptance writes
// budgets.yaml.
type budgetOffice struct {
	guard    *budgetGuard
	book     *budget.Book
	store    *store.Store
	tickets  *work.Service
	messages *messaging.Service
	org      org.Source
	clock    schedule.Clock
	bus      *events.Bus
	logger   *slog.Logger
}

// newBudgetOffice builds the office and plugs it into the guard.
func newBudgetOffice(
	b *budgets, st *store.Store, tickets *work.Service, messages *messaging.Service, opts Options, bus *events.Bus,
) *budgetOffice {
	office := &budgetOffice{
		guard: b.guard, book: b.book, store: st, tickets: tickets, messages: messages,
		org: opts.Org, clock: clockOf(opts), bus: bus, logger: opts.Logger,
	}

	b.guard.onAwaitingDirector = office.tellDirector
	b.guard.afterCommit = office.afterCommit

	return office
}

// Requests lists the requests, newest first, with the agents each holds
// now and, for a ticket of a feature with a budget, what the feature has
// left to allocate.
func (o *budgetOffice) Requests(ctx context.Context, open bool) ([]budget.Request, error) {
	requests, err := o.store.ListBudgetRequests(ctx, store.BudgetRequestFilter{Open: open})
	if err != nil {
		return nil, err
	}

	for i := range requests {
		o.complete(ctx, &requests[i])
	}

	return requests, nil
}

// RequestExtension is budget_request: a held agent justifies the request it
// was asked about, which then goes to the director.
func (o *budgetOffice) RequestExtension(ctx context.Context, agent, id string, usd float64, justification string) (budget.Request, error) {
	if usd <= 0 || strings.TrimSpace(justification) == "" {
		return budget.Request{}, fmt.Errorf("%w: give the amount you need (more than 0) and a justification", budget.ErrInvalid)
	}

	r, err := o.requestFor(ctx, agent, id)
	if err != nil {
		return budget.Request{}, err
	}

	r.Status, r.RequestedBy, r.AmountUSD, r.Justification = budget.RequestAwaitingDirector, agent, &usd, strings.TrimSpace(justification)

	if err := o.store.UpdateBudgetRequest(ctx, r); err != nil {
		return budget.Request{}, err
	}

	o.changed()
	o.tellDirector(ctx, r)

	return r, nil
}

// Submit is budget_submit: the director passes a request on to the human,
// with its opinion and, for a staff or feature budget, its justification.
func (o *budgetOffice) Submit(ctx context.Context, director, id string, s budget.Submission) (budget.Request, error) {
	if !o.org.Current().IsGod(director) {
		return budget.Request{}, fmt.Errorf("%w: only the director passes budget requests on", budget.ErrInvalid)
	}

	r, err := o.store.GetBudgetRequest(ctx, id)
	if err != nil {
		return budget.Request{}, err
	}

	if r.Status != budget.RequestAwaitingDirector {
		return budget.Request{}, fmt.Errorf("%w: %s is %s, it does not wait for you", budget.ErrConflict, r.ID, r.Status)
	}

	if err := applySubmission(&r, director, s); err != nil {
		return budget.Request{}, err
	}

	now := o.clock.Now().UTC()
	r.Status, r.SubmittedAt = budget.RequestAwaitingHuman, &now

	msg, err := o.messages.Send(ctx, messaging.Draft{
		From: director, To: org.HumanAddress, Act: messaging.ActPropose, Priority: messaging.Normal,
		Subject: fmt.Sprintf("Budget request %s: %s", r.ID, r.Scope), Body: humanBrief(r),
	})
	if err != nil {
		return budget.Request{}, err
	}

	r.CardMessage = msg.ID

	if err := o.store.UpdateBudgetRequest(ctx, r); err != nil {
		return budget.Request{}, err
	}

	o.changed()

	return r, nil
}

// Extend grants an extension (ADR-0013, point 6); only the human calls it,
// from the control room. The held agents start again with their memory.
func (o *budgetOffice) Extend(ctx context.Context, id string, usd float64) (budget.Request, error) {
	r, err := o.decidable(ctx, id)
	if err != nil {
		return budget.Request{}, err
	}

	if usd <= 0 {
		return budget.Request{}, fmt.Errorf("%w: an extension must be more than 0", budget.ErrInvalid)
	}

	if err := o.checkEnough(ctx, r.Scope, usd); err != nil {
		return budget.Request{}, err
	}

	held := o.guard.holding(r.Scope)

	if err := o.raise(ctx, r, usd); err != nil {
		return budget.Request{}, err
	}

	now := o.clock.Now().UTC()
	r.Status, r.Active, r.ExtendedUSD, r.DecidedAt = budget.RequestExtended, false, &usd, &now

	if err := o.store.UpdateBudgetRequest(ctx, r); err != nil {
		return budget.Request{}, err
	}

	o.answerDirector(ctx, r, messaging.ActAgree, fmt.Sprintf("Budget of %s extended by $%.2f", r.Scope, usd),
		"The agents it held start again.")

	for _, a := range held {
		o.notice(ctx, a, fmt.Sprintf("Budget extended: %s", r.Scope),
			fmt.Sprintf("The human extended the budget of %s by $%.2f. Carry on where you stopped.", r.Scope, usd))
	}

	return r, o.settle(ctx)
}

// Refuse turns an extension down; the human chooses what happens to each
// held agent (ADR-0013, point 5).
func (o *budgetOffice) Refuse(ctx context.Context, id string, refusal budget.Refusal) (budget.Request, error) {
	r, err := o.decidable(ctx, id)
	if err != nil {
		return budget.Request{}, err
	}

	held := o.guard.holding(r.Scope)

	tickets, err := o.ticketsOf(ctx, held)
	if err != nil {
		return budget.Request{}, err
	}

	if err := checkOutcomes(refusal, held, tickets); err != nil {
		return budget.Request{}, err
	}

	now := o.clock.Now().UTC()
	refusal.Reason = strings.TrimSpace(refusal.Reason)
	r.Status, r.Refusal, r.DecidedAt = budget.RequestRefused, &refusal, &now

	if err := o.store.UpdateBudgetRequest(ctx, r); err != nil {
		return budget.Request{}, err
	}

	o.answerDirector(ctx, r, messaging.ActRefuse, fmt.Sprintf("Budget of %s: extension refused", r.Scope),
		"Reason: "+orNone(refusal.Reason))

	for _, a := range held {
		o.applyRefusal(ctx, r, a, tickets[a], refusal)
	}

	return r, o.settle(ctx)
}

// complete adds what a request shows but does not store.
func (o *budgetOffice) complete(ctx context.Context, r *budget.Request) {
	r.HeldAgents = []string{}
	if r.Active {
		r.HeldAgents = append(r.HeldAgents, o.guard.holding(r.Scope)...)
	}

	if r.Scope.Kind != budget.KindTicket {
		return
	}

	t, err := o.store.GetTicket(ctx, r.Scope.ID)
	if err != nil || t.Feature == "" {
		return
	}

	if f, err := o.store.GetFeature(ctx, t.Feature); err == nil && f.IsSet() {
		left := f.Left()
		r.FeatureRemainingUSD = &left
	}
}

// requestFor finds the request an agent may justify: the one named, or the
// one it was asked about.
func (o *budgetOffice) requestFor(ctx context.Context, agent, id string) (budget.Request, error) {
	active, err := o.store.ListBudgetRequests(ctx, store.BudgetRequestFilter{Active: true})
	if err != nil {
		return budget.Request{}, err
	}

	for _, r := range active {
		if id != "" && r.ID != id || id == "" && r.AskedAgent != agent {
			continue
		}

		if r.Status != budget.RequestAwaitingAgent {
			return budget.Request{}, fmt.Errorf("%w: %s no longer waits for a justification (%s)", budget.ErrConflict, r.ID, r.Status)
		}

		if !slices.Contains(o.guard.holding(r.Scope), agent) {
			return budget.Request{}, fmt.Errorf("%w: %s does not hold you", budget.ErrConflict, r.ID)
		}

		return r, nil
	}

	return budget.Request{}, fmt.Errorf("%w: no budget request waits for your justification", budget.ErrNotFound)
}

// tellDirector sends the director a request that waits for it.
func (o *budgetOffice) tellDirector(ctx context.Context, r budget.Request) {
	director := officeDirector(o.org.Current())
	if director == "" {
		return
	}

	_, err := o.messages.Send(ctx, messaging.Draft{
		From: org.SystemAddress, To: director, Act: messaging.ActInform, Priority: messaging.Normal,
		Subject: fmt.Sprintf("Budget request %s: %s reached its threshold", r.ID, r.Scope),
		Body:    directorBrief(r),
	})
	if err != nil {
		o.logger.Error("send a budget request to the director", "request", r.ID, "err", err)
	}
}

// decidable loads a request waiting for the human.
func (o *budgetOffice) decidable(ctx context.Context, id string) (budget.Request, error) {
	r, err := o.store.GetBudgetRequest(ctx, id)
	if err != nil {
		return budget.Request{}, err
	}

	if r.Status != budget.RequestAwaitingHuman {
		return budget.Request{}, fmt.Errorf("%w: %s is %s; the human decides once the director passed it on", budget.ErrConflict, r.ID, r.Status)
	}

	return r, nil
}

// checkEnough refuses an extension that would leave the budget at its
// threshold: its agents would stay on hold.
func (o *budgetOffice) checkEnough(ctx context.Context, scope budget.Scope, usd float64) error {
	gauges, err := budget.Evaluate(ctx, o.book.Get(), o.store, o.clock.Now())
	if err != nil {
		return err
	}

	for _, g := range gauges {
		if g.Scope != scope {
			continue
		}

		// The limit at which the spend is under the threshold, plus a cent.
		needed := g.SpentUSD*percent/float64(g.Threshold) - g.LimitUSD + 0.01
		if usd < needed {
			return fmt.Errorf("%w: $%.2f still leaves %s at its %d%% threshold; extend it by at least $%.2f",
				budget.ErrConflict, usd, scope, g.Threshold, math.Ceil(needed*percent)/percent)
		}

		return nil
	}

	return fmt.Errorf("%w: %s is no longer reached", budget.ErrConflict, scope)
}

// raise applies an extension where its budget lives.
func (o *budgetOffice) raise(ctx context.Context, r budget.Request, usd float64) error {
	switch r.Scope.Kind {
	case budget.KindTicket:
		_, err := o.tickets.ExtendTicketBudget(ctx, r.Scope.ID, usd)

		return err
	case budget.KindFeature:
		_, err := o.tickets.ExtendFeatureBudget(ctx, r.Scope.ID, usd)

		return err
	case budget.KindAgent, budget.KindTeam, budget.KindStaff:
		c, ok := o.book.Get().Cap(r.Scope)
		if !ok {
			return fmt.Errorf("%w: %s has no cap any more", budget.ErrConflict, r.Scope)
		}

		if c.Monthly {
			return o.store.AddMonthlyExtension(ctx, r.Scope, budget.Month(o.clock.Now()), usd, r.ID)
		}

		_, err := o.book.RaiseCap(r.Scope, usd)

		return err
	}

	return fmt.Errorf("%w: unknown budget %s", budget.ErrInvalid, r.Scope)
}

// ticketsOf returns each agent's ticket in progress, if any.
func (o *budgetOffice) ticketsOf(ctx context.Context, agents []string) (map[string]string, error) {
	inProgress, err := o.store.InProgress(ctx)
	if err != nil {
		return nil, err
	}

	tickets := map[string]string{}

	for _, a := range inProgress {
		if slices.Contains(agents, a.Agent) {
			tickets[a.Agent] = a.Ticket
		}
	}

	return tickets, nil
}

// applyRefusal tells a held agent, and applies its outcome now or after its
// commit turn.
func (o *budgetOffice) applyRefusal(ctx context.Context, r budget.Request, agent, ticket string, refusal budget.Refusal) {
	c := commitTurn{request: r.ID, ticket: ticket, outcome: refusal.Outcomes[agent]}

	if refusal.CommitFirst && ticket != "" {
		o.guard.grantCommitTurn(agent, c)
		o.notice(ctx, agent, "Budget refused: commit your work in progress",
			fmt.Sprintf("The human refused to extend the budget of %s. Commit your work in progress on %s now, "+
				"comment the ticket with where you stopped, then end your turn. %s", r.Scope, ticket, outcomeText(c)))

		return
	}

	o.notice(ctx, agent, "Budget refused", fmt.Sprintf("The human refused to extend the budget of %s. %s", r.Scope, outcomeText(c)))
	o.applyOutcome(ctx, agent, c)
}

// afterCommit applies a refusal's outcome once the agent's commit turn is
// over.
func (o *budgetOffice) afterCommit(ctx context.Context, agent string, c commitTurn) {
	o.applyOutcome(ctx, agent, c)
}

// applyOutcome cancels the agent's ticket, gives it back to the queue, or
// leaves the agent on hold.
func (o *budgetOffice) applyOutcome(ctx context.Context, agent string, c commitTurn) {
	status := work.StatusCanceled

	switch c.outcome {
	case budget.OutcomeHold:
		return
	case budget.OutcomeRelease:
		status = work.StatusOpen
	case budget.OutcomeCancel:
	}

	if _, err := o.tickets.SetStatus(ctx, org.HumanAddress, c.ticket, status); err != nil {
		o.logger.Error("apply a budget refusal", "agent", agent, "ticket", c.ticket, "outcome", c.outcome, "err", err)
	}
}

// answerDirector answers the director's card with the human's decision.
func (o *budgetOffice) answerDirector(ctx context.Context, r budget.Request, act messaging.Act, subject, body string) {
	director := officeDirector(o.org.Current())
	if director == "" {
		return
	}

	_, err := o.messages.Send(ctx, messaging.Draft{
		From: org.HumanAddress, To: director, Act: act, Priority: messaging.Normal, InReplyTo: r.CardMessage,
		Subject: subject, Body: body,
	})
	if err != nil {
		o.logger.Error("answer a budget request", "request", r.ID, "err", err)
	}
}

// notice sends an agent a notice from nobody.
func (o *budgetOffice) notice(ctx context.Context, agent, subject, body string) {
	_, err := o.messages.Send(ctx, messaging.Draft{
		From: org.SystemAddress, To: agent, Act: messaging.ActInform, Priority: messaging.Normal, Subject: subject, Body: body,
	})
	if err != nil {
		o.logger.Error("send a budget notice", "agent", agent, "err", err)
	}
}

// settle works the holds out again after a decision, and announces it.
func (o *budgetOffice) settle(ctx context.Context) error {
	o.changed()

	return o.guard.reconcile(ctx)
}

func (o *budgetOffice) changed() {
	o.bus.Publish(events.Event{Type: events.BudgetsChanged, Data: events.Empty{}})
}

// directorBrief is what the director reads about a request.
func directorBrief(r budget.Request) string {
	var b strings.Builder

	fmt.Fprintf(&b, "The budget of %s reached its threshold: $%.2f spent of $%.2f.\n", r.Scope, r.SpentUSD, r.LimitUSD)

	switch {
	case r.Scope.Global():
		b.WriteString("It is a budget only you see as a whole: write the justification yourself (where the work stands, " +
			"what remains, why it costs more than planned) and the amount to ask for.\n")
	case r.Justification == "":
		b.WriteString("The agent stopped without justifying it: explain the situation yourself, from what you know.\n")
	default:
		fmt.Fprintf(&b, "%s asks for $%.2f more:\n%s\n", r.RequestedBy, deref(r.AmountUSD), r.Justification)
	}

	fmt.Fprintf(&b, "\nThe agents it covers are on hold, their sessions kept. Pass it on to the human with budget_submit "+
		"(request %s): your opinion and the amount you recommend. You grant nothing yourself: the human decides.", r.ID)

	return b.String()
}

// applySubmission records the director's opinion; for a request nobody
// justified (staff, feature, or an agent that stopped without answering),
// its justification is required.
func applySubmission(r *budget.Request, director string, s budget.Submission) error {
	if strings.TrimSpace(s.Opinion) == "" {
		return fmt.Errorf("%w: give your opinion", budget.ErrInvalid)
	}

	if s.RecommendedUSD != nil && *s.RecommendedUSD <= 0 {
		return fmt.Errorf("%w: the amount you recommend must be more than 0", budget.ErrInvalid)
	}

	if r.Justification == "" {
		if strings.TrimSpace(s.Justification) == "" || s.AmountUSD == nil || *s.AmountUSD <= 0 {
			return fmt.Errorf("%w: nobody justified %s yet: give the justification and the amount to ask for", budget.ErrInvalid, r.ID)
		}

		r.Justification, r.AmountUSD, r.RequestedBy = strings.TrimSpace(s.Justification), s.AmountUSD, director
	}

	r.DirectorOpinion, r.RecommendedUSD = strings.TrimSpace(s.Opinion), s.RecommendedUSD

	return nil
}

// humanBrief is the card's message to the human.
func humanBrief(r budget.Request) string {
	return fmt.Sprintf("The budget of %s reached its threshold: $%.2f spent of $%.2f. Its agents are on hold.\n\n"+
		"%s asks for $%.2f more:\n%s\n\nMy opinion: %s\n\nExtend it or refuse it from this card.",
		r.Scope, r.SpentUSD, r.LimitUSD, r.RequestedBy, deref(r.AmountUSD), r.Justification, r.DirectorOpinion)
}

// checkOutcomes requires an outcome for every held agent, and only "hold"
// for an agent without a ticket.
func checkOutcomes(refusal budget.Refusal, held []string, tickets map[string]string) error {
	for _, a := range slices.Sorted(maps.Keys(refusal.Outcomes)) {
		if !slices.Contains(held, a) {
			return fmt.Errorf("%w: %s is not held by this budget", budget.ErrInvalid, a)
		}
	}

	for _, a := range held {
		switch outcome := refusal.Outcomes[a]; outcome {
		case budget.OutcomeHold:
		case budget.OutcomeCancel, budget.OutcomeRelease:
			if tickets[a] == "" {
				return fmt.Errorf("%w: %s has no ticket: it can only stay on hold", budget.ErrInvalid, a)
			}
		default:
			return fmt.Errorf("%w: choose cancel, release or hold for %s", budget.ErrInvalid, a)
		}
	}

	return nil
}

func outcomeText(c commitTurn) string {
	switch c.outcome {
	case budget.OutcomeCancel:
		return "Your ticket " + c.ticket + " is canceled."
	case budget.OutcomeRelease:
		return "Your ticket " + c.ticket + " goes back to your team's queue."
	case budget.OutcomeHold:
	}

	return "You stay on hold, your conversation kept, until the human decides otherwise."
}

// officeDirector is the director's address, or "" without one.
func officeDirector(o *org.Org) string {
	if o.God == nil {
		return ""
	}

	return o.God.Name
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}

	return *p
}
