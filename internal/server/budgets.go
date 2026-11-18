package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// routeBudgets registers the costs and budgets endpoints.
func (s *server) routeBudgets(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/budgets", s.getBudgets)
	mux.HandleFunc("PUT /api/budgets", s.updateBudgets)
	mux.HandleFunc("GET /api/spend", s.getSpend)
	mux.HandleFunc("PATCH /api/tickets/{id}", s.updateTicketBudget)
	mux.HandleFunc("POST /internal/agents/{address}/statusline", s.reportStatusLine)
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

// gauges says where every budget stands now: the caps of budgets.yaml, the
// features started and not finished, and the tickets not archived, done or
// canceled that have a budget.
func (s *server) gauges(ctx context.Context) ([]budget.Gauge, error) {
	if s.Budgets == nil {
		return []budget.Gauge{}, nil
	}

	settings := s.Budgets.Get()
	if !settings.Enabled {
		return []budget.Gauge{}, nil
	}

	month, err := s.Store.SpendTotals(ctx, budget.Month(time.Now()))
	if err != nil {
		return nil, err
	}

	allTime, err := s.Store.SpendTotals(ctx, "")
	if err != nil {
		return nil, err
	}

	budgeted, err := s.budgetedWork(ctx)
	if err != nil {
		return nil, err
	}

	return budget.Gauges(settings, month, allTime, budgeted), nil
}

// budgetedWork lists the features and tickets whose budget still matters.
func (s *server) budgetedWork(ctx context.Context) ([]budget.Budgeted, error) {
	features, err := s.Tickets.Store.ListFeatures(ctx)
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

	tickets, err := s.Tickets.Store.ListTickets(ctx, work.Filter{})
	if err != nil {
		return nil, err
	}

	for _, t := range tickets {
		if t.IsSet() && (t.Status == work.StatusOpen || t.Status == work.StatusInProgress) {
			budgeted = append(budgeted, budget.Budgeted{
				Scope: budget.Scope{Kind: budget.KindTicket, ID: t.ID}, LimitUSD: *t.USD, SpentUSD: t.SpentUSD, Threshold: intOr0(t.Threshold),
			})
		}
	}

	return budgeted, nil
}

func intOr0(p *int) int {
	if p == nil {
		return 0
	}

	return *p
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
