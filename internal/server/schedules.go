package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// Scheduled tasks (ADR-0012, docs/schedules.md): the human lists, creates,
// edits, pauses, resumes, runs and deletes schedules, reads what they did,
// and decides on the director's proposals. The schedules are written to the
// project's .nobody/schedules.yaml by the daemon.

// Schedules is what backs the scheduled tasks' endpoints (implemented in
// internal/daemon).
type Schedules interface {
	List(ctx context.Context) ([]schedule.View, error)
	Create(ctx context.Context, s schedule.Schedule) (schedule.View, error)
	Update(ctx context.Context, id string, s schedule.Schedule) (schedule.View, error)
	Delete(ctx context.Context, id string) error
	SetEnabled(ctx context.Context, id string, enabled bool) (schedule.View, error)
	RunNow(ctx context.Context, id string) (schedule.Run, error)
	Runs(ctx context.Context, scheduleID string, limit int) ([]schedule.Run, error)
	Proposals(ctx context.Context) ([]schedule.Proposal, error)
	Accept(ctx context.Context, id string, edited *schedule.Schedule) (schedule.Proposal, error)
	Refuse(ctx context.Context, id, reason string) (schedule.Proposal, error)
}

// previewCount is how many occurrences POST /api/schedules/preview shows.
const previewCount = 3

// Bounds of GET /api/schedule-runs?limit=.
const (
	defaultRunsLimit = 100
	maxRunsLimit     = 500
)

// routeSchedules mounts the scheduled tasks' endpoints; without Schedules
// they answer 501.
func (s *server) routeSchedules(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/schedules", s.listSchedules)
	mux.HandleFunc("POST /api/schedules/preview", s.previewRecurrence)
	mux.HandleFunc("POST /api/schedules", s.createSchedule)
	mux.HandleFunc("PUT /api/schedules/{id}", s.updateSchedule)
	mux.HandleFunc("DELETE /api/schedules/{id}", s.deleteSchedule)
	mux.HandleFunc("POST /api/schedules/{id}/pause", s.setScheduleEnabled(false))
	mux.HandleFunc("POST /api/schedules/{id}/resume", s.setScheduleEnabled(true))
	mux.HandleFunc("POST /api/schedules/{id}/run", s.runSchedule)
	mux.HandleFunc("GET /api/schedule-runs", s.listScheduleRuns)
	mux.HandleFunc("GET /api/schedule-proposals", s.listScheduleProposals)
	mux.HandleFunc("POST /api/schedule-proposals/{id}/accept", s.acceptScheduleProposal)
	mux.HandleFunc("POST /api/schedule-proposals/{id}/refuse", s.refuseScheduleProposal)
}

// schedulesAvailable answers 501 when the daemon has no scheduled tasks.
func (s *server) schedulesAvailable(w http.ResponseWriter) bool {
	if s.Schedules == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("scheduled tasks are not available"))

		return false
	}

	return true
}

func (s *server) listSchedules(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	views, err := s.Schedules.List(r.Context())
	s.scheduleReply(w, http.StatusOK, views, err)
}

// recurrence is the body of POST /api/schedules/preview.
type recurrence struct {
	Every string `json:"every"`
	Cron  string `json:"cron"`
}

// preview is the answer of POST /api/schedules/preview.
type preview struct {
	Next []time.Time `json:"next"`
}

// previewRecurrence checks a recurrence and shows its next occurrences; it
// needs no schedule, nor the daemon's scheduled tasks.
func (s *server) previewRecurrence(w http.ResponseWriter, r *http.Request) {
	var in recurrence
	if err := decodeJSON(w, r, &in, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	next, err := schedule.Preview(in.Every, in.Cron, time.Now(), previewCount)
	s.scheduleReply(w, http.StatusOK, preview{Next: next}, err)
}

func (s *server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in schedule.Schedule
	if !s.schedulesAvailable(w) || !s.decodeSchedule(w, r, &in) {
		return
	}

	view, err := s.Schedules.Create(r.Context(), in)
	s.scheduleReply(w, http.StatusCreated, view, err)
}

func (s *server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var in schedule.Schedule
	if !s.schedulesAvailable(w) || !s.decodeSchedule(w, r, &in) {
		return
	}

	view, err := s.Schedules.Update(r.Context(), r.PathValue("id"), in)
	s.scheduleReply(w, http.StatusOK, view, err)
}

func (s *server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	if err := s.Schedules.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.scheduleError(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// setScheduleEnabled pauses (false) or resumes (true) a schedule.
func (s *server) setScheduleEnabled(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.schedulesAvailable(w) {
			return
		}

		view, err := s.Schedules.SetEnabled(r.Context(), r.PathValue("id"), enabled)
		s.scheduleReply(w, http.StatusOK, view, err)
	}
}

func (s *server) runSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	run, err := s.Schedules.RunNow(r.Context(), r.PathValue("id"))
	s.scheduleReply(w, http.StatusAccepted, run, err)
}

func (s *server) listScheduleRuns(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	limit := defaultRunsLimit

	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxRunsLimit {
			s.writeError(w, http.StatusBadRequest, fmt.Errorf("limit must be between 1 and %d", maxRunsLimit))

			return
		}

		limit = n
	}

	runs, err := s.Schedules.Runs(r.Context(), r.URL.Query().Get("schedule"), limit)
	s.scheduleReply(w, http.StatusOK, runs, err)
}

func (s *server) listScheduleProposals(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	proposals, err := s.Schedules.Proposals(r.Context())
	s.scheduleReply(w, http.StatusOK, proposals, err)
}

// acceptance is the body of POST /api/schedule-proposals/{id}/accept: the
// schedule as the human edited it, if they did.
type acceptance struct {
	Schedule *schedule.Schedule `json:"schedule"`
}

func (s *server) acceptScheduleProposal(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	var body acceptance

	// The body is optional: no body accepts the proposal as it is.
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &body, true); err != nil {
			s.writeError(w, http.StatusBadRequest, err)

			return
		}
	}

	p, err := s.Schedules.Accept(r.Context(), r.PathValue("id"), body.Schedule)
	s.scheduleReply(w, http.StatusOK, p, err)
}

func (s *server) refuseScheduleProposal(w http.ResponseWriter, r *http.Request) {
	if !s.schedulesAvailable(w) {
		return
	}

	var body refusal
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	p, err := s.Schedules.Refuse(r.Context(), r.PathValue("id"), body.Reason)
	s.scheduleReply(w, http.StatusOK, p, err)
}

func (s *server) decodeSchedule(w http.ResponseWriter, r *http.Request, in *schedule.Schedule) bool {
	if err := decodeJSON(w, r, in, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return false
	}

	return true
}

// scheduleReply writes v with status, or the error.
func (s *server) scheduleReply(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		s.scheduleError(w, err)

		return
	}

	s.writeJSON(w, status, v)
}

func (s *server) scheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, schedule.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, schedule.ErrInvalid):
		s.writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, schedule.ErrExists), errors.Is(err, schedule.ErrBusy), errors.Is(err, schedule.ErrDecided):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.Logger.Error("schedule request", "err", err)
		s.writeError(w, http.StatusInternalServerError, errors.New("schedule request failed"))
	}
}
