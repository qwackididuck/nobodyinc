package work_test

import (
	"context"
	"errors"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

func TestFeatureProposals(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	var proposed, decided []work.Feature

	s.OnFeatureProposed = func(f work.Feature) string {
		proposed = append(proposed, f)

		return "m-proposal"
	}
	s.OnFeatureDecided = func(f work.Feature) { decided = append(decided, f) }

	draft := work.FeatureDraft{Title: "Orders", Description: "Customers order", AcceptanceCriteria: "An order can be placed"}

	if _, err := s.ProposeFeature(ctx, "dev1@dev", draft); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("a team agent proposed a feature: err = %v", err)
	}

	f, err := s.ProposeFeature(ctx, "god", draft)
	if err != nil {
		t.Fatal(err)
	}

	if f.Status != work.FeatureProposed || f.Active || f.ProposalMessage != "m-proposal" || len(proposed) != 1 {
		t.Fatalf("proposal = %+v (proposed callbacks: %d)", f, len(proposed))
	}

	if _, err := s.StartProposedFeature(ctx, "god", f.ID, nil); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("the director started its own proposal: err = %v", err)
	}

	started, err := s.StartProposedFeature(ctx, org.HumanAddress, f.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if started.Status != work.FeatureStarted || !started.Active || len(decided) != 1 {
		t.Fatalf("started = %+v (decided callbacks: %d)", started, len(decided))
	}

	stored, err := s.Store.GetFeature(ctx, f.ID)
	if err != nil || stored.Status != work.FeatureStarted || stored.ProposalMessage != "m-proposal" || stored.AcceptanceCriteria != "An order can be placed" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	if _, err := s.StartProposedFeature(ctx, org.HumanAddress, f.ID, nil); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("starting a started feature again: err = %v", err)
	}
}

func TestRefuseFeature(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	var decided []work.Feature

	s.OnFeatureDecided = func(f work.Feature) { decided = append(decided, f) }

	f, err := s.ProposeFeature(ctx, "god", work.FeatureDraft{Title: "Rewrite in COBOL"})
	if err != nil {
		t.Fatal(err)
	}

	refused, err := s.RefuseFeature(ctx, org.HumanAddress, f.ID, "  not now  ")
	if err != nil {
		t.Fatal(err)
	}

	if refused.Status != work.FeatureRefused || refused.RefusalReason != "not now" || refused.Active || len(decided) != 1 {
		t.Fatalf("refused = %+v", refused)
	}

	if _, err := s.StartProposedFeature(ctx, org.HumanAddress, f.ID, nil); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("starting a refused feature: err = %v", err)
	}
}

func TestHumanStartsAFeatureDirectly(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)

	f, err := s.StartFeature(context.Background(), org.HumanAddress, work.FeatureDraft{Title: "Orders"})
	if err != nil || f.Status != work.FeatureStarted || !f.Active {
		t.Fatalf("feature = %+v, %v", f, err)
	}
}
