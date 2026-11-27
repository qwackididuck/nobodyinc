package work_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

func TestDeleteTicket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		by          string
		cascade     bool
		wantErr     error
		wantDeleted []string
		// wantBlocked is what T-3 waits for afterwards ("-" when deleted).
		wantBlocked []string
	}{
		{name: "the human deletes, dependents are unblocked", by: org.HumanAddress, wantDeleted: []string{"T-1"}, wantBlocked: nil},
		{name: "with cascade, dependents and theirs go too", by: org.HumanAddress, cascade: true, wantDeleted: []string{"T-1", "T-2", "T-3"}},
		{name: "nobody else deletes", by: "god", wantErr: work.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newService(t)
			ctx := context.Background()

			// T-1 ← T-2 ← T-3: T-2 waits for T-1, T-3 for T-2. T-4 is unrelated.
			first := create(t, s, "specs")
			second := create(t, s, "dev", first.ID)
			create(t, s, "dev", second.ID)
			create(t, s, "qa")

			if _, err := s.Claim(ctx, "archi@specs", first.ID); err != nil {
				t.Fatal(err)
			}

			var stopped []string

			s.OnTicketDeleted = func(tk work.Ticket) { stopped = append(stopped, tk.ID+" "+tk.Assignee) }

			deleted, err := s.DeleteTicket(ctx, tt.by, first.ID, tt.cascade)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			if !slices.Equal(deleted, tt.wantDeleted) {
				t.Fatalf("deleted %v, want %v", deleted, tt.wantDeleted)
			}

			if !slices.Equal(stopped, []string{"T-1 archi@specs"}) {
				t.Fatalf("the agent working on T-1 must be told: %v", stopped)
			}

			left, err := s.Store.ListTickets(ctx, work.Filter{})
			if err != nil {
				t.Fatal(err)
			}

			for _, tk := range left {
				if slices.Contains(deleted, tk.ID) {
					t.Fatalf("%s is still there", tk.ID)
				}

				if tk.ID == second.ID && len(tk.BlockedBy) != 0 {
					t.Fatalf("T-2 should no longer wait for the deleted T-1: %v", tk.BlockedBy)
				}
			}
		})
	}
}

func TestDependents(t *testing.T) {
	t.Parallel()

	tickets := []work.Ticket{
		{ID: "T-1"},
		{ID: "T-2", BlockedBy: []string{"T-1"}},
		{ID: "T-3", BlockedBy: []string{"T-2", "T-1"}},
		{ID: "T-4", BlockedBy: []string{"T-3"}},
		{ID: "T-5"},
	}

	if got := work.Dependents(tickets, "T-1"); !slices.Equal(got, []string{"T-2", "T-3", "T-4"}) {
		t.Fatalf("dependents of T-1 = %v", got)
	}

	if got := work.Dependents(tickets, "T-5"); len(got) != 0 {
		t.Fatalf("T-5 has no dependents, got %v", got)
	}
}

func TestFinishingArchivesTheFeatureTickets(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	s.Workspace = &fakeWorkspace{}
	ctx := context.Background()

	f, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
	if err != nil {
		t.Fatal(err)
	}

	open := create(t, s, "dev")

	if _, err := s.FinishFeature(ctx, org.HumanAddress, f.ID, true); err != nil {
		t.Fatal(err)
	}

	if active, _ := s.Store.ListTickets(ctx, work.Filter{}); len(active) != 0 {
		t.Fatalf("the feature's tickets should leave the board: %+v", active)
	}

	archived, err := s.Store.ListTickets(ctx, work.Filter{Archived: true})
	if err != nil || len(archived) != 1 || archived[0].ID != open.ID || archived[0].Status != work.StatusCanceled || !archived[0].Archived {
		t.Fatalf("archived = %+v, %v (a forced finish cancels what is not done)", archived, err)
	}

	if _, err := s.DeleteTicket(ctx, org.HumanAddress, open.ID, false); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("an archived ticket cannot be shredded: error = %v", err)
	}
}

func TestCleanBoard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		by        string
		scope     work.CleanScope
		wantErr   error
		wantGone  []string
		wantStops []string
	}{
		{name: "the human cleans the closed tickets", by: org.HumanAddress, scope: work.CleanClosed, wantGone: []string{"T-1", "T-2"}},
		{name: "the director cleans everything", by: "god", scope: work.CleanAll,
			wantGone: []string{"T-1", "T-2", "T-3", "T-4"}, wantStops: []string{"T-3 dev1@dev"}},
		{name: "a team agent may not", by: "dev1@dev", scope: work.CleanAll, wantErr: work.ErrForbidden},
		{name: "an unknown scope is refused", by: org.HumanAddress, scope: "some", wantErr: work.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newService(t)
			ctx := context.Background()

			// T-1 done, T-2 canceled, T-3 in progress, T-4 open.
			done := create(t, s, "dev")
			canceled := create(t, s, "dev")
			working := create(t, s, "dev")
			create(t, s, "qa")

			for id, status := range map[string]work.Status{done.ID: work.StatusDone, canceled.ID: work.StatusCanceled} {
				if _, err := s.SetStatus(ctx, org.HumanAddress, id, status); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := s.Claim(ctx, "dev1@dev", working.ID); err != nil {
				t.Fatal(err)
			}

			var stopped []string

			s.OnTicketDeleted = func(tk work.Ticket) { stopped = append(stopped, tk.ID+" "+tk.Assignee) }

			got, err := s.CleanBoard(ctx, tt.by, tt.scope)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			if !slices.Equal(got, tt.wantGone) {
				t.Fatalf("deleted %v, want %v", got, tt.wantGone)
			}

			if !slices.Equal(stopped, tt.wantStops) {
				t.Fatalf("agents told to stop: %v, want %v", stopped, tt.wantStops)
			}

			left, err := s.Store.ListTickets(ctx, work.Filter{})
			if err != nil {
				t.Fatal(err)
			}

			if want := 4 - len(tt.wantGone); len(left) != want {
				t.Fatalf("%d tickets left, want %d", len(left), want)
			}
		})
	}
}
