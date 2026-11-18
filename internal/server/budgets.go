package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/budget"
)

// routeBudgets registers the costs and budgets endpoints.
func (s *server) routeBudgets(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/spend", s.getSpend)
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
	if err != nil {
		s.Logger.Error("read spend", "month", month, "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("cannot read the spend"))

		return
	}

	s.writeJSON(w, http.StatusOK, summary)
}
