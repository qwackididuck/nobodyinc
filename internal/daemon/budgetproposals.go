package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Overview is budget_list: the settings, this month's spend with every
// gauge, and the requests still open.
func (o *budgetOffice) Overview(ctx context.Context) (budget.Overview, error) {
	now := o.clock.Now()

	spend, err := o.store.SpendSummary(ctx, budget.Month(now))
	if err != nil {
		return budget.Overview{}, err
	}

	if spend.Gauges, err = budget.Evaluate(ctx, o.book.Get(), o.store, now); err != nil {
		return budget.Overview{}, err
	}

	requests, err := o.Requests(ctx, true)
	if err != nil {
		return budget.Overview{}, err
	}

	return budget.Overview{Settings: o.book.Get(), Spend: spend, Requests: requests}, nil
}

// Propose is budget_propose: the director proposes an agent, team or staff
// cap; the human accepts it, possibly edited, or refuses it (ADR-0013,
// point 8).
func (o *budgetOffice) Propose(ctx context.Context, director string, p budget.Proposal) (budget.Proposal, error) {
	cur := o.org.Current()
	if !cur.IsGod(director) {
		return budget.Proposal{}, fmt.Errorf("%w: only the director proposes caps", budget.ErrInvalid)
	}

	if !o.book.Enabled() {
		return budget.Proposal{}, fmt.Errorf("%w: budgets are turned off for this project", budget.ErrConflict)
	}

	if err := checkProposedCap(cur, p.Scope, p.Cap); err != nil {
		return budget.Proposal{}, err
	}

	p.Status, p.ProposedBy, p.CreatedAt, p.Reason = budget.ProposalPending, director, o.clock.Now().UTC(), strings.TrimSpace(p.Reason)

	p, err := o.store.InsertBudgetProposal(ctx, p)
	if err != nil {
		return budget.Proposal{}, err
	}

	msg, err := o.messages.Send(ctx, messaging.Draft{
		From: director, To: org.HumanAddress, Act: messaging.ActPropose, Priority: messaging.Normal,
		Subject: fmt.Sprintf("Cap proposal %s: %s", p.ID, p.Scope), Body: proposedCapText(p),
	})
	if err != nil {
		o.logger.Error("send a cap proposal", "proposal", p.ID, "err", err)
	} else {
		p.ProposalMessage = msg.ID
		if err := o.store.UpdateBudgetProposal(ctx, p); err != nil {
			return budget.Proposal{}, err
		}
	}

	o.changed()

	return p, nil
}

// checkProposedCap checks a cap on an agent, a team or the staff that exist.
func checkProposedCap(o *org.Org, scope budget.Scope, c budget.Cap) error {
	if !scope.IsCap() {
		return fmt.Errorf("%w: a cap is for an agent, a team or the staff", budget.ErrInvalid)
	}

	candidate := budget.Settings{Threshold: budget.DefaultThreshold}

	switch scope.Kind {
	case budget.KindStaff:
		candidate.Staff = &c
	case budget.KindTeam:
		candidate.Teams = map[string]budget.Cap{scope.ID: c}
	case budget.KindAgent:
		candidate.Agents = map[string]budget.Cap{scope.ID: c}
	case budget.KindTicket, budget.KindFeature:
	}

	if err := candidate.Check(); err != nil {
		return err
	}

	return candidate.CheckOrg(o)
}

func proposedCapText(p budget.Proposal) string {
	period := "cumulative"
	if p.Cap.Monthly {
		period = "reset every month"
	}

	threshold := "the project's threshold"
	if p.Cap.Threshold > 0 {
		threshold = fmt.Sprintf("a %d%% threshold", p.Cap.Threshold)
	}

	return fmt.Sprintf("I propose a cap of $%.2f on %s, %s, with %s.\n\nWhy: %s\n\n"+
		"Accept it (possibly edited) or refuse it from the control room.", p.Cap.LimitUSD, p.Scope, period, threshold, orNone(p.Reason))
}

// Proposals lists the director's cap proposals, newest first.
func (o *budgetOffice) Proposals(ctx context.Context) ([]budget.Proposal, error) {
	return o.store.ListBudgetProposals(ctx)
}

// AcceptProposal writes a proposed cap, possibly edited by the human, to
// budgets.yaml, and answers the director with "agree".
func (o *budgetOffice) AcceptProposal(ctx context.Context, id string, edited *budget.Cap) (budget.Proposal, error) {
	p, err := o.pendingProposal(ctx, id)
	if err != nil {
		return budget.Proposal{}, err
	}

	if !o.book.Enabled() {
		return budget.Proposal{}, fmt.Errorf("%w: budgets are turned off for this project", budget.ErrConflict)
	}

	if edited != nil {
		p.Cap = *edited
	}

	if _, err := o.book.SetCap(p.Scope, p.Cap); err != nil {
		return budget.Proposal{}, err
	}

	now := o.clock.Now().UTC()
	p.Status, p.DecidedAt = budget.ProposalAccepted, &now

	if err := o.store.UpdateBudgetProposal(ctx, p); err != nil {
		return budget.Proposal{}, err
	}

	note := ""
	if edited != nil {
		note = " The human edited it before accepting; see budget_list."
	}

	o.answerProposal(ctx, p, messaging.ActAgree, fmt.Sprintf("Cap on %s accepted", p.Scope),
		fmt.Sprintf("The cap of $%.2f on %s is written.%s", p.Cap.LimitUSD, p.Scope, note))
	o.changed()

	return p, nil
}

// RefuseProposal turns a cap proposal down and answers the director with
// "refuse".
func (o *budgetOffice) RefuseProposal(ctx context.Context, id, reason string) (budget.Proposal, error) {
	p, err := o.pendingProposal(ctx, id)
	if err != nil {
		return budget.Proposal{}, err
	}

	now := o.clock.Now().UTC()
	p.Status, p.DecidedAt, p.RefusalReason = budget.ProposalRefused, &now, strings.TrimSpace(reason)

	if err := o.store.UpdateBudgetProposal(ctx, p); err != nil {
		return budget.Proposal{}, err
	}

	o.answerProposal(ctx, p, messaging.ActRefuse, fmt.Sprintf("Cap proposal %s refused", p.ID), "Reason: "+orNone(p.RefusalReason))
	o.changed()

	return p, nil
}

func (o *budgetOffice) pendingProposal(ctx context.Context, id string) (budget.Proposal, error) {
	p, err := o.store.GetBudgetProposal(ctx, id)
	if err != nil {
		return budget.Proposal{}, err
	}

	if p.Status != budget.ProposalPending {
		return budget.Proposal{}, fmt.Errorf("%w: %s is already %s", budget.ErrConflict, p.ID, p.Status)
	}

	return p, nil
}

func (o *budgetOffice) answerProposal(ctx context.Context, p budget.Proposal, act messaging.Act, subject, body string) {
	_, err := o.messages.Send(ctx, messaging.Draft{
		From: org.HumanAddress, To: p.ProposedBy, Act: act, Priority: messaging.Normal, InReplyTo: p.ProposalMessage,
		Subject: subject, Body: body,
	})
	if err != nil {
		o.logger.Error("answer a cap proposal", "proposal", p.ID, "err", err)
	}
}
