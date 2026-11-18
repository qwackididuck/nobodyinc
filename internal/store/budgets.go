package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// BudgetedWork lists the started features and the open or in-progress
// tickets that have a budget, with what they spent (budget.Figures).
func (s *Store) BudgetedWork(ctx context.Context) ([]budget.Budgeted, error) {
	features, err := s.ListFeatures(ctx)
	if err != nil {
		return nil, err
	}

	var budgeted []budget.Budgeted

	for _, f := range features {
		if f.IsSet() && f.Status == work.FeatureStarted {
			budgeted = append(budgeted, budget.Budgeted{
				Scope: budget.Scope{Kind: budget.KindFeature, ID: f.ID}, LimitUSD: *f.USD, SpentUSD: f.SpentUSD, Threshold: intOr0(f.Threshold),
			})
		}
	}

	tickets, err := s.queryTickets(ctx,
		selectTickets+` WHERE budget_usd IS NOT NULL AND status IN ('open', 'in_progress') AND archived = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}

	for _, t := range tickets {
		budgeted = append(budgeted, budget.Budgeted{
			Scope: budget.Scope{Kind: budget.KindTicket, ID: t.ID}, LimitUSD: *t.USD, SpentUSD: t.SpentUSD, Threshold: intOr0(t.Threshold),
		})
	}

	return budgeted, nil
}

// InProgress lists the tickets in progress and their agents: what a ticket
// or feature budget holds.
func (s *Store) InProgress(ctx context.Context) ([]budget.Assignment, error) {
	tickets, err := s.ListTickets(ctx, work.Filter{Status: work.StatusInProgress})
	if err != nil {
		return nil, err
	}

	assignments := make([]budget.Assignment, 0, len(tickets))
	for _, t := range tickets {
		assignments = append(assignments, budget.Assignment{Ticket: t.ID, Feature: t.Feature, Agent: t.Assignee})
	}

	return assignments, nil
}

func intOr0(p *int) int {
	if p == nil {
		return 0
	}

	return *p
}

// InsertBudgetRequest stores a new request and returns it with its ID.
func (s *Store) InsertBudgetRequest(ctx context.Context, r budget.Request) (budget.Request, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO budget_requests (scope_kind, scope_id, status, active, limit_usd, spent_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Scope.Kind, r.Scope.ID, r.Status, r.Active, r.LimitUSD, r.SpentUSD, formatTime(r.CreatedAt))
	if err != nil {
		return budget.Request{}, fmt.Errorf("insert budget request: %w", err)
	}

	n, err := res.LastInsertId()
	if err != nil {
		return budget.Request{}, err
	}

	r.ID = budget.FormatRequestID(n)

	return r, s.UpdateBudgetRequest(ctx, r)
}

// UpdateBudgetRequest saves everything that changes in a request.
func (s *Store) UpdateBudgetRequest(ctx context.Context, r budget.Request) error {
	n, err := budget.ParseRequestID(r.ID)
	if err != nil {
		return err
	}

	refusal := ""

	if r.Refusal != nil {
		data, err := json.Marshal(r.Refusal)
		if err != nil {
			return err
		}

		refusal = string(data)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE budget_requests SET status = ?, active = ?, asked_agent = ?, requested_by = ?, amount_usd = ?, justification = ?,
			director_opinion = ?, recommended_usd = ?, card_message = ?, extended_usd = ?, refusal = ?, closed_reason = ?,
			submitted_at = ?, decided_at = ?
		WHERE id = ?`,
		r.Status, r.Active, r.AskedAgent, r.RequestedBy, r.AmountUSD, r.Justification,
		r.DirectorOpinion, r.RecommendedUSD, r.CardMessage, r.ExtendedUSD, refusal, r.ClosedReason,
		formatOptionalTime(r.SubmittedAt), formatOptionalTime(r.DecidedAt), n)
	if err != nil {
		return fmt.Errorf("update budget request %s: %w", r.ID, err)
	}

	return nil
}

// GetBudgetRequest returns one request.
func (s *Store) GetBudgetRequest(ctx context.Context, id string) (budget.Request, error) {
	n, err := budget.ParseRequestID(id)
	if err != nil {
		return budget.Request{}, err
	}

	r, err := scanBudgetRequest(s.db.QueryRowContext(ctx, selectBudgetRequests+` WHERE id = ?`, n))
	if errors.Is(err, sql.ErrNoRows) {
		return budget.Request{}, fmt.Errorf("%w: budget request %s", budget.ErrNotFound, id)
	}

	return r, err
}

// BudgetRequestFilter narrows ListBudgetRequests.
type BudgetRequestFilter struct {
	// Open keeps the requests waiting for someone; Active those whose budget
	// is still reached.
	Open, Active bool
}

// ListBudgetRequests returns requests, newest first.
func (s *Store) ListBudgetRequests(ctx context.Context, f BudgetRequestFilter) ([]budget.Request, error) {
	query := selectBudgetRequests + ` WHERE 1 = 1`
	if f.Open {
		query += ` AND status IN ('awaiting_agent', 'awaiting_director', 'awaiting_human')`
	}

	if f.Active {
		query += ` AND active = 1`
	}

	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list budget requests: %w", err)
	}
	defer rows.Close()

	requests := []budget.Request{}

	for rows.Next() {
		r, err := scanBudgetRequest(rows)
		if err != nil {
			return nil, err
		}

		requests = append(requests, r)
	}

	return requests, rows.Err()
}

const selectBudgetRequests = `
	SELECT id, scope_kind, scope_id, status, active, asked_agent, requested_by, amount_usd, justification,
		director_opinion, recommended_usd, limit_usd, spent_usd, card_message, extended_usd, refusal, closed_reason,
		created_at, submitted_at, decided_at
	FROM budget_requests`

func scanBudgetRequest(row scanner) (budget.Request, error) {
	var (
		r                             budget.Request
		n                             int64
		refusal, created, sub, decide string
	)

	if err := row.Scan(&n, &r.Scope.Kind, &r.Scope.ID, &r.Status, &r.Active, &r.AskedAgent, &r.RequestedBy, &r.AmountUSD,
		&r.Justification, &r.DirectorOpinion, &r.RecommendedUSD, &r.LimitUSD, &r.SpentUSD, &r.CardMessage, &r.ExtendedUSD,
		&refusal, &r.ClosedReason, &created, &sub, &decide); err != nil {
		return budget.Request{}, err
	}

	r.ID = budget.FormatRequestID(n)
	r.HeldAgents = []string{}

	if refusal != "" {
		r.Refusal = &budget.Refusal{}
		if err := json.Unmarshal([]byte(refusal), r.Refusal); err != nil {
			return budget.Request{}, fmt.Errorf("read the refusal of %s: %w", r.ID, err)
		}
	}

	var err error
	if r.CreatedAt, err = parseTime(created); err != nil {
		return budget.Request{}, err
	}

	if r.SubmittedAt, err = parseOptionalTime(sub); err != nil {
		return budget.Request{}, err
	}

	r.DecidedAt, err = parseOptionalTime(decide)

	return r, err
}

// AddMonthlyExtension records an extension of a monthly cap for a month
// (ADR-0013, point 6).
func (s *Store) AddMonthlyExtension(ctx context.Context, scope budget.Scope, month string, usd float64, request string) error {
	n, err := budget.ParseRequestID(request)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO budget_extensions (scope_kind, scope_id, month, amount_usd, request) VALUES (?, ?, ?, ?, ?)`,
		scope.Kind, scope.ID, month, usd, n)
	if err != nil {
		return fmt.Errorf("record a monthly extension: %w", err)
	}

	return nil
}

// MonthlyExtensions returns the extensions granted for a month, by budget.
func (s *Store) MonthlyExtensions(ctx context.Context, month string) (map[budget.Scope]float64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT scope_kind, scope_id, SUM(amount_usd) FROM budget_extensions WHERE month = ? GROUP BY scope_kind, scope_id`, month)
	if err != nil {
		return nil, fmt.Errorf("monthly extensions: %w", err)
	}
	defer rows.Close()

	extensions := map[budget.Scope]float64{}

	for rows.Next() {
		var (
			scope budget.Scope
			usd   float64
		)
		if err := rows.Scan(&scope.Kind, &scope.ID, &usd); err != nil {
			return nil, err
		}

		extensions[scope] = usd
	}

	return extensions, rows.Err()
}

// InsertBudgetProposal stores a cap the director proposed and returns it
// with its ID.
func (s *Store) InsertBudgetProposal(ctx context.Context, p budget.Proposal) (budget.Proposal, error) {
	data, err := json.Marshal(p.Cap)
	if err != nil {
		return budget.Proposal{}, err
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO budget_proposals (scope_kind, scope_id, cap, reason, status, proposed_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Scope.Kind, p.Scope.ID, string(data), p.Reason, p.Status, p.ProposedBy, formatTime(p.CreatedAt))
	if err != nil {
		return budget.Proposal{}, fmt.Errorf("insert budget proposal: %w", err)
	}

	n, err := res.LastInsertId()
	if err != nil {
		return budget.Proposal{}, err
	}

	p.ID = budget.FormatProposalID(n)

	return p, nil
}

// UpdateBudgetProposal saves a proposal's cap (edited by the human), status,
// message, reason and decision time.
func (s *Store) UpdateBudgetProposal(ctx context.Context, p budget.Proposal) error {
	n, err := budget.ParseProposalID(p.ID)
	if err != nil {
		return err
	}

	data, err := json.Marshal(p.Cap)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE budget_proposals SET cap = ?, status = ?, proposal_message = ?, refusal_reason = ?, decided_at = ? WHERE id = ?`,
		string(data), p.Status, p.ProposalMessage, p.RefusalReason, formatOptionalTime(p.DecidedAt), n)
	if err != nil {
		return fmt.Errorf("update budget proposal %s: %w", p.ID, err)
	}

	return nil
}

// GetBudgetProposal returns one proposal.
func (s *Store) GetBudgetProposal(ctx context.Context, id string) (budget.Proposal, error) {
	n, err := budget.ParseProposalID(id)
	if err != nil {
		return budget.Proposal{}, err
	}

	p, err := scanBudgetProposal(s.db.QueryRowContext(ctx, selectBudgetProposals+` WHERE id = ?`, n))
	if errors.Is(err, sql.ErrNoRows) {
		return budget.Proposal{}, fmt.Errorf("%w: budget proposal %s", budget.ErrNotFound, id)
	}

	return p, err
}

// ListBudgetProposals returns every proposal, newest first.
func (s *Store) ListBudgetProposals(ctx context.Context) ([]budget.Proposal, error) {
	rows, err := s.db.QueryContext(ctx, selectBudgetProposals+` ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list budget proposals: %w", err)
	}
	defer rows.Close()

	proposals := []budget.Proposal{}

	for rows.Next() {
		p, err := scanBudgetProposal(rows)
		if err != nil {
			return nil, err
		}

		proposals = append(proposals, p)
	}

	return proposals, rows.Err()
}

const selectBudgetProposals = `
	SELECT id, scope_kind, scope_id, cap, reason, status, proposed_by, proposal_message, refusal_reason, created_at, decided_at
	FROM budget_proposals`

func scanBudgetProposal(row scanner) (budget.Proposal, error) {
	var (
		p                     budget.Proposal
		n                     int64
		capJSON, created, dec string
	)

	if err := row.Scan(&n, &p.Scope.Kind, &p.Scope.ID, &capJSON, &p.Reason, &p.Status, &p.ProposedBy, &p.ProposalMessage,
		&p.RefusalReason, &created, &dec); err != nil {
		return budget.Proposal{}, err
	}

	p.ID = budget.FormatProposalID(n)

	if err := json.Unmarshal([]byte(capJSON), &p.Cap); err != nil {
		return budget.Proposal{}, fmt.Errorf("read the cap of %s: %w", p.ID, err)
	}

	var err error
	if p.CreatedAt, err = parseTime(created); err != nil {
		return budget.Proposal{}, err
	}

	p.DecidedAt, err = parseOptionalTime(dec)

	return p, err
}
