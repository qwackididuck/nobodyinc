package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
)

// routeBudgets registers the costs and budgets endpoints.
func (s *server) routeBudgets(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/budgets", s.getBudgets)
	mux.HandleFunc("PUT /api/budgets", s.updateBudgets)
	mux.HandleFunc("GET /api/spend", s.getSpend)
	mux.HandleFunc("PATCH /api/tickets/{id}", s.updateTicketBudget)
	mux.HandleFunc("POST /internal/agents/{address}/statusline", s.reportStatusLine)
	mux.HandleFunc("GET /api/budget-requests", s.listBudgetRequests)
	mux.HandleFunc("POST /api/budget-requests/{id}/extend", s.extendBudget)
	mux.HandleFunc("POST /api/budget-requests/{id}/refuse", s.refuseBudget)
	mux.HandleFunc("GET /api/budget-proposals", s.listBudgetProposals)
	mux.HandleFunc("POST /api/budget-proposals/{id}/accept", s.acceptBudgetProposal)
	mux.HandleFunc("POST /api/budget-proposals/{id}/refuse", s.refuseBudgetProposal)
}

// statusLineReport is the part of the statusLine JSON the daemon reads
// (StatusLineReport in api/openapi.yaml).
type statusLineReport struct {
	SessionID string `json:"session_id"`
	Cost      struct {
		TotalCostUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
}

// reportStatusLine records what an agent's session spent, from the JSON its
// statusLine received (ADR-0013).
func (s *server) reportStatusLine(w http.ResponseWriter, r *http.Request) {
	address, ok := s.agentAddress(w, r)
	if !ok {
		return
	}

	var report statusLineReport
	if err := decodeJSON(w, r, &report, false); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if report.SessionID == "" || report.Cost.TotalCostUSD == nil {
		s.writeError(w, http.StatusBadRequest, errors.New("session_id and cost.total_cost_usd are required"))

		return
	}

	if s.Spend != nil {
		if err := s.Spend.Report(r.Context(), address, report.SessionID, *report.Cost.TotalCostUSD); err != nil {
			s.Logger.Error("record spend", "agent", address, "err", err)
			s.writeError(w, http.StatusInternalServerError, errors.New("cannot record the spend"))

			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// monthPattern is a "YYYY-MM" month.
var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[012])$`)

// getSpend answers what was spent in a month, the current one by default.
func (s *server) getSpend(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = budget.Month(time.Now())
	}

	if !monthPattern.MatchString(month) {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("month %q: want YYYY-MM", month))

		return
	}

	summary, err := s.Store.SpendSummary(r.Context(), month)
	if err == nil {
		summary.Gauges, err = s.gauges(r.Context())
	}

	if err != nil {
		s.Logger.Error("read spend", "month", month, "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot read the spend"))

		return
	}

	s.writeJSON(w, http.StatusOK, summary)
}

// gauges says where every budget stands now (budget.Evaluate).
func (s *server) gauges(ctx context.Context) ([]budget.Gauge, error) {
	if s.Budgets == nil {
		return []budget.Gauge{}, nil
	}

	return budget.Evaluate(ctx, s.Budgets.Get(), s.Store, time.Now())
}

// budgetsAvailable answers 501 when the daemon has no budgets file.
func (s *server) budgetsAvailable(w http.ResponseWriter) bool {
	if s.Budgets == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("budgets are not available"))

		return false
	}

	return true
}

func (s *server) getBudgets(w http.ResponseWriter, _ *http.Request) {
	if s.budgetsAvailable(w) {
		s.writeJSON(w, http.StatusOK, s.Budgets.Get())
	}
}

// updateBudgets replaces .nobody/budgets.yaml (ADR-0013, point 3).
func (s *server) updateBudgets(w http.ResponseWriter, r *http.Request) {
	if !s.budgetsAvailable(w) {
		return
	}

	var settings budget.Settings
	if err := decodeJSON(w, r, &settings, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	saved, err := s.Budgets.Replace(settings)
	switch {
	case errors.Is(err, budget.ErrInvalid):
		s.writeError(w, http.StatusBadRequest, err)
	case err != nil:
		s.Logger.Error("write budgets", "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot write the budgets"))
	default:
		s.writeJSON(w, http.StatusOK, saved)
	}
}

// BudgetOffice is what backs the budget requests and the director's cap
// proposals (implemented in internal/daemon). Only the human decides.
type BudgetOffice interface {
	Requests(ctx context.Context, open bool) ([]budget.Request, error)
	Extend(ctx context.Context, id string, usd float64) (budget.Request, error)
	Refuse(ctx context.Context, id string, r budget.Refusal) (budget.Request, error)
	Proposals(ctx context.Context) ([]budget.Proposal, error)
	AcceptProposal(ctx context.Context, id string, edited *budget.Cap) (budget.Proposal, error)
	RefuseProposal(ctx context.Context, id, reason string) (budget.Proposal, error)
}

// budgetOfficeAvailable answers 501 when the daemon has no budget office.
func (s *server) budgetOfficeAvailable(w http.ResponseWriter) bool {
	if s.BudgetOffice == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("budget requests are not available"))

		return false
	}

	return true
}

// budgetReply answers a budget office call: its value, or its error as a
// status (404, 400, 409; a ticket error as the ticket routes do).
func (s *server) budgetReply(w http.ResponseWriter, v any, err error) {
	switch {
	case err == nil:
		s.writeJSON(w, http.StatusOK, v)
	case errors.Is(err, budget.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, budget.ErrInvalid):
		s.writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, budget.ErrConflict):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.ticketError(w, err)
	}
}

func (s *server) listBudgetRequests(w http.ResponseWriter, r *http.Request) {
	if s.budgetOfficeAvailable(w) {
		requests, err := s.BudgetOffice.Requests(r.Context(), r.URL.Query().Get("open") == "true")
		s.budgetReply(w, requests, err)
	}
}

func (s *server) extendBudget(w http.ResponseWriter, r *http.Request) {
	if !s.budgetOfficeAvailable(w) {
		return
	}

	var body struct {
		AmountUSD float64 `json:"amount_usd"`
	}
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	request, err := s.BudgetOffice.Extend(r.Context(), r.PathValue("id"), body.AmountUSD)
	s.budgetReply(w, request, err)
}

func (s *server) refuseBudget(w http.ResponseWriter, r *http.Request) {
	if !s.budgetOfficeAvailable(w) {
		return
	}

	var refusal budget.Refusal
	if err := decodeJSON(w, r, &refusal, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	request, err := s.BudgetOffice.Refuse(r.Context(), r.PathValue("id"), refusal)
	s.budgetReply(w, request, err)
}

func (s *server) listBudgetProposals(w http.ResponseWriter, r *http.Request) {
	if s.budgetOfficeAvailable(w) {
		proposals, err := s.BudgetOffice.Proposals(r.Context())
		s.budgetReply(w, proposals, err)
	}
}

func (s *server) acceptBudgetProposal(w http.ResponseWriter, r *http.Request) {
	if !s.budgetOfficeAvailable(w) {
		return
	}

	var body struct {
		Cap *budget.Cap `json:"cap"`
	}

	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &body, true); err != nil && !errors.Is(err, io.EOF) {
			s.writeError(w, http.StatusBadRequest, err)

			return
		}
	}

	proposal, err := s.BudgetOffice.AcceptProposal(r.Context(), r.PathValue("id"), body.Cap)
	s.budgetReply(w, proposal, err)
}

func (s *server) refuseBudgetProposal(w http.ResponseWriter, r *http.Request) {
	if !s.budgetOfficeAvailable(w) {
		return
	}

	var body refusal
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	proposal, err := s.BudgetOffice.RefuseProposal(r.Context(), r.PathValue("id"), body.Reason)
	s.budgetReply(w, proposal, err)
}
