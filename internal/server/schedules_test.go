package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// fakeSchedules records calls and fails with err when set.
type fakeSchedules struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeSchedules) List(context.Context) ([]schedule.View, error) {
	return []schedule.View{}, f.record("list")
}

func (f *fakeSchedules) Create(_ context.Context, s schedule.Schedule) (schedule.View, error) {
	return schedule.View{Schedule: s}, f.record("create %s %s", s.ID, s.Every)
}

func (f *fakeSchedules) Update(_ context.Context, id string, s schedule.Schedule) (schedule.View, error) {
	return schedule.View{Schedule: s}, f.record("update %s %s", id, s.Every)
}

func (f *fakeSchedules) Delete(_ context.Context, id string) error {
	return f.record("delete %s", id)
}

func (f *fakeSchedules) SetEnabled(_ context.Context, id string, enabled bool) (schedule.View, error) {
	return schedule.View{}, f.record("enabled %s %v", id, enabled)
}

func (f *fakeSchedules) RunNow(_ context.Context, id string) (schedule.Run, error) {
	return schedule.Run{ScheduleID: id}, f.record("run %s", id)
}

func (f *fakeSchedules) Runs(_ context.Context, id string, limit int) ([]schedule.Run, error) {
	return []schedule.Run{}, f.record("runs %q %d", id, limit)
}

func (f *fakeSchedules) Proposals(context.Context) ([]schedule.Proposal, error) {
	return []schedule.Proposal{}, f.record("proposals")
}

func (f *fakeSchedules) Accept(_ context.Context, id string, edited *schedule.Schedule) (schedule.Proposal, error) {
	every := "as proposed"
	if edited != nil {
		every = edited.Every
	}

	return schedule.Proposal{ID: id}, f.record("accept %s %s", id, every)
}

func (f *fakeSchedules) Refuse(_ context.Context, id, reason string) (schedule.Proposal, error) {
	return schedule.Proposal{ID: id}, f.record("refuse %s %s", id, reason)
}

func (f *fakeSchedules) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, fmt.Sprintf(format, args...))

	return f.err
}

func (f *fakeSchedules) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.calls) == 0 {
		return ""
	}

	return f.calls[len(f.calls)-1]
}

func TestSchedulesAPI(t *testing.T) {
	t.Parallel()

	const body = `{"id":"watch","title":"Watch","every":"10m","target":"@ops","mode":"message","instructions":"Look."}`

	tests := []struct {
		name         string
		method, path string
		body         string
		err          error
		code         int
		call         string
	}{
		{"list", "GET", "/api/schedules", "", nil, http.StatusOK, "list"},
		{"create", "POST", "/api/schedules", body, nil, http.StatusCreated, "create watch 10m"},
		{"create, unknown field", "POST", "/api/schedules", `{"id":"x","when":"now"}`, nil, http.StatusBadRequest, ""},
		{"create, invalid", "POST", "/api/schedules", body, schedule.ErrInvalid, http.StatusBadRequest, "create watch 10m"},
		{"create, exists", "POST", "/api/schedules", body, schedule.ErrExists, http.StatusConflict, "create watch 10m"},
		{"update", "PUT", "/api/schedules/watch", body, nil, http.StatusOK, "update watch 10m"},
		{"update, unknown", "PUT", "/api/schedules/nope", body, schedule.ErrNotFound, http.StatusNotFound, "update nope 10m"},
		{"delete", "DELETE", "/api/schedules/watch", "", nil, http.StatusNoContent, "delete watch"},
		{"pause", "POST", "/api/schedules/watch/pause", "", nil, http.StatusOK, "enabled watch false"},
		{"resume", "POST", "/api/schedules/watch/resume", "", nil, http.StatusOK, "enabled watch true"},
		{"run now", "POST", "/api/schedules/watch/run", "", nil, http.StatusAccepted, "run watch"},
		{"run now, busy", "POST", "/api/schedules/watch/run", "", schedule.ErrBusy, http.StatusConflict, "run watch"},
		{"runs", "GET", "/api/schedule-runs", "", nil, http.StatusOK, `runs "" 100`},
		{"runs of one", "GET", "/api/schedule-runs?schedule=watch&limit=5", "", nil, http.StatusOK, `runs "watch" 5`},
		{"runs, bad limit", "GET", "/api/schedule-runs?limit=0", "", nil, http.StatusBadRequest, ""},
		{"proposals", "GET", "/api/schedule-proposals", "", nil, http.StatusOK, "proposals"},
		{"accept as is", "POST", "/api/schedule-proposals/SP-1/accept", "", nil, http.StatusOK, "accept SP-1 as proposed"},
		{"accept edited", "POST", "/api/schedule-proposals/SP-1/accept", `{"schedule":` + body + `}`, nil, http.StatusOK, "accept SP-1 10m"},
		{"accept, decided", "POST", "/api/schedule-proposals/SP-1/accept", "", schedule.ErrDecided, http.StatusConflict, "accept SP-1 as proposed"},
		{"refuse", "POST", "/api/schedule-proposals/SP-2/refuse", `{"reason":"too often"}`, nil, http.StatusOK, "refuse SP-2 too often"},
		{"failure", "GET", "/api/schedules", "", errors.New("disk full"), http.StatusInternalServerError, "list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeSchedules{err: tt.err}
			h := New(Deps{Logger: slog.New(slog.DiscardHandler), Schedules: fake})

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.code, rec.Body)
			}

			if got := fake.last(); got != tt.call {
				t.Fatalf("call = %q, want %q", got, tt.call)
			}
		})
	}
}

func TestSchedulesAPIWithoutSchedules(t *testing.T) {
	t.Parallel()

	rec := newTestEnv(t).do(t, http.MethodGet, "/api/schedules", "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestPreviewRecurrence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		body string
		code int
		want string // part of the answer
	}{
		{`{"every":"10m"}`, http.StatusOK, `"next":["`},
		{`{"cron":"0 9 * * 1-5"}`, http.StatusOK, `"next":["`},
		{`{"every":"30s"}`, http.StatusBadRequest, "at least"},
		{`{"every":"10m","cron":"@daily"}`, http.StatusBadRequest, "not both"},
		{`{}`, http.StatusBadRequest, "every"},
	}

	h := New(Deps{Logger: slog.New(slog.DiscardHandler)}) // no scheduled tasks needed

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/schedules/preview", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.want) {
				t.Fatalf("%d %s, want %d with %q", rec.Code, rec.Body, tt.code, tt.want)
			}
		})
	}
}
