package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

func TestScheduleRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)
	t0 := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

	insert := func(id string, status schedule.RunStatus) schedule.Run {
		t.Helper()

		r, err := s.InsertRun(ctx, schedule.Run{
			ScheduleID: id, ScheduleTitle: "T " + id, Trigger: schedule.TriggerSchedule, Mode: schedule.ModeMessage,
			Status: status, DueAt: t0,
		})
		if err != nil {
			t.Fatal(err)
		}

		return r
	}

	insert("watch", schedule.RunSkipped)
	waiting := insert("watch", schedule.RunWaiting)
	insert("report", schedule.RunWaiting)

	started := t0.Add(time.Minute)
	waiting.Status, waiting.StartedAt, waiting.Agent, waiting.Message = schedule.RunRunning, &started, "w@ops", "m1"

	if err := s.UpdateRun(ctx, waiting); err != nil {
		t.Fatal(err)
	}

	runs, err := s.ListRuns(ctx, RunFilter{Schedule: "watch"})
	if err != nil {
		t.Fatal(err)
	}

	if len(runs) != 2 || runs[0].ID != waiting.ID || runs[0].Status != schedule.RunRunning ||
		runs[0].StartedAt == nil || !runs[0].StartedAt.Equal(started) || runs[0].EndedAt != nil ||
		runs[0].Agent != "w@ops" || runs[0].Mode != schedule.ModeMessage || !runs[0].DueAt.Equal(t0) {
		t.Fatalf("ListRuns(watch) = %+v", runs)
	}

	active, err := s.ListRuns(ctx, RunFilter{Statuses: []schedule.RunStatus{schedule.RunWaiting, schedule.RunRunning}})
	if err != nil {
		t.Fatal(err)
	}

	if len(active) != 2 {
		t.Fatalf("active runs = %+v", active)
	}

	if last, _ := s.ListRuns(ctx, RunFilter{Limit: 1}); len(last) != 1 || last[0].ScheduleID != "report" {
		t.Fatalf("newest run = %+v", last)
	}
}

func TestScheduleProposals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)
	t0 := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

	p, err := s.InsertProposal(ctx, schedule.Proposal{
		Schedule: schedule.Schedule{ID: "watch", Title: "Watch", Every: "10m", Target: "w@ops", Mode: schedule.ModeMessage},
		Status:   schedule.ProposalPending, ProposedBy: "god", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	if p.ID != "SP-1" {
		t.Fatalf("ID = %q", p.ID)
	}

	decided := t0.Add(time.Hour)
	p.Status, p.DecidedAt, p.ProposalMessage, p.Schedule.Every = schedule.ProposalAccepted, &decided, "m9", "15m"

	if err := s.UpdateProposal(ctx, p); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProposal(ctx, "SP-1")
	if err != nil {
		t.Fatal(err)
	}

	if got.Status != schedule.ProposalAccepted || got.Schedule.Every != "15m" || got.ProposalMessage != "m9" ||
		got.DecidedAt == nil || !got.DecidedAt.Equal(decided) || got.ProposedBy != "god" {
		t.Fatalf("GetProposal = %+v", got)
	}

	if _, err := s.GetProposal(ctx, "SP-7"); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("missing proposal: %v", err)
	}

	if all, _ := s.ListProposals(ctx); len(all) != 1 {
		t.Fatalf("ListProposals = %+v", all)
	}
}
