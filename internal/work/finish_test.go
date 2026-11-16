package work_test

import (
	"context"
	"errors"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

func TestFinishFeature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		by   string
		// openTicket leaves a ticket of the feature open.
		openTicket bool
		force      bool
		finishErr  error
		wantErr    error
	}{
		{name: "the human finishes the active feature", by: org.HumanAddress},
		{name: "nobody else does", by: "god", wantErr: work.ErrForbidden},
		{name: "unfinished tickets refuse it", by: org.HumanAddress, openTicket: true, wantErr: work.ErrUnfinished},
		{name: "unless forced", by: org.HumanAddress, openTicket: true, force: true},
		{name: "a git refusal changes nothing", by: org.HumanAddress, finishErr: errors.New("conflict"), wantErr: errors.New("conflict")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newService(t)
			ws := &fakeWorkspace{finishErr: tt.finishErr}
			s.Workspace = ws
			ctx := context.Background()

			var finished []work.Feature

			s.OnFeatureFinished = func(f work.Feature) { finished = append(finished, f) }

			f, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
			if err != nil {
				t.Fatal(err)
			}

			if tt.openTicket {
				create(t, s, "dev")
			}

			got, err := s.FinishFeature(ctx, tt.by, f.ID, tt.force)
			if tt.wantErr != nil {
				if err == nil || (!errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}

				stored, _ := s.Store.GetFeature(ctx, f.ID)
				if stored.Status != work.FeatureStarted || !stored.Active || len(finished) != 0 {
					t.Fatalf("a refused finish must change nothing: %+v, hook calls %d", stored, len(finished))
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			stored, _ := s.Store.GetFeature(ctx, f.ID)
			if got.Status != work.FeatureFinished || stored.Status != work.FeatureFinished || stored.Active || stored.MergeCommit != "merge123" {
				t.Fatalf("finished feature = %+v (stored %+v)", got, stored)
			}

			if len(finished) != 1 || ws.finished != 1 {
				t.Fatalf("hook calls %d, merges %d", len(finished), ws.finished)
			}

			if _, err := s.FinishFeature(ctx, org.HumanAddress, f.ID, false); !errors.Is(err, work.ErrConflict) {
				t.Fatalf("finishing twice: error = %v", err)
			}
		})
	}
}

func TestReviewFeature(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	s.Workspace = &fakeWorkspace{}
	ctx := context.Background()

	f, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
	if err != nil {
		t.Fatal(err)
	}

	open := create(t, s, "dev")

	review, err := s.ReviewFeature(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(review.Commits) != 1 || len(review.UnfinishedTickets) != 1 || review.UnfinishedTickets[0].ID != open.ID {
		t.Fatalf("review = %+v", review)
	}

	proposed, err := s.ProposeFeature(ctx, "god", work.FeatureDraft{Title: "Later"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReviewFeature(ctx, proposed.ID); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("a proposed feature has nothing to review: error = %v", err)
	}
}
