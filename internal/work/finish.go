package work

// Finishing a feature (ADR-0007). The human reviews what the feature branch
// brings to its base branch, then finishes it: the feature branch is merged
// into the base branch (locally, never pushed), the feature is finished and
// no longer active, its tickets are archived, and OnFeatureFinished brings
// the agents back to the project. Only the human finishes a feature, and only
// the active one.

import (
	"context"
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// ReviewCommit is a commit the feature brings.
type ReviewCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// FileChange is a file the feature changes; Added is -1 for a binary file.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// Changes is what the feature branch brings to its base branch.
type Changes struct {
	Commits       []ReviewCommit `json:"commits"`
	Files         []FileChange   `json:"files"`
	Diff          string         `json:"diff"`
	DiffTruncated bool           `json:"diff_truncated"`
}

// FeatureReview is what the human reads before finishing a feature.
type FeatureReview struct {
	Changes

	Feature Feature `json:"feature"`
	// UnfinishedTickets are the feature's tickets not done or canceled.
	UnfinishedTickets []Ticket `json:"unfinished_tickets"`
}

// ReviewFeature describes what a started feature brings to its base branch.
func (s *Service) ReviewFeature(ctx context.Context, id string) (FeatureReview, error) {
	f, err := s.Store.GetFeature(ctx, id)
	if err != nil {
		return FeatureReview{}, err
	}

	if f.Status != FeatureStarted && f.Status != FeatureFinished {
		return FeatureReview{}, fmt.Errorf("%w: %s is %s: there is nothing to review", ErrConflict, f.ID, f.Status)
	}

	review := FeatureReview{Feature: f}
	review.Commits, review.Files = []ReviewCommit{}, []FileChange{}

	if s.Workspace != nil && f.Branch != "" {
		if review.Changes, err = s.Workspace.ReviewFeature(ctx, f); err != nil {
			return FeatureReview{}, err
		}
	}

	if review.UnfinishedTickets, err = s.unfinishedTickets(ctx, f.ID); err != nil {
		return FeatureReview{}, err
	}

	return review, nil
}

// FinishFeature merges the active feature into its base branch and ends it;
// its tickets are archived. While some of them are not done or canceled, it
// is refused unless force is set: they are then canceled, and the work they
// have not delivered is lost.
func (s *Service) FinishFeature(ctx context.Context, by, id string, force bool) (Feature, error) {
	f, err := s.finishable(ctx, by, id, force)
	if err != nil {
		return Feature{}, err
	}

	if s.Workspace != nil && f.Branch != "" {
		if f.MergeCommit, err = s.Workspace.FinishFeature(ctx, f); err != nil {
			return Feature{}, err
		}
	}

	f.Status = FeatureFinished
	if err := s.Store.UpdateFeature(ctx, f); err != nil {
		return Feature{}, err
	}

	if err := s.Store.DeactivateFeature(ctx, f.ID); err != nil {
		return Feature{}, err
	}

	if err := s.archiveTickets(ctx, f.ID); err != nil {
		return Feature{}, err
	}

	f.Active = false
	if s.OnFeatureFinished != nil {
		s.OnFeatureFinished(f)
	}

	return f, nil
}

// finishable loads the feature and checks it may be finished: by the human,
// the active one, with every ticket done or canceled unless forced.
func (s *Service) finishable(ctx context.Context, by, id string, force bool) (Feature, error) {
	if by != org.HumanAddress {
		return Feature{}, fmt.Errorf("%w: only the human finishes a feature", ErrForbidden)
	}

	f, err := s.Store.GetFeature(ctx, id)
	if err != nil {
		return Feature{}, err
	}

	if f.Status != FeatureStarted || !f.Active {
		return Feature{}, fmt.Errorf("%w: only the active feature can be finished, %s is %s", ErrConflict, f.ID, describeFeature(f))
	}

	if force {
		return f, nil
	}

	unfinished, err := s.unfinishedTickets(ctx, f.ID)
	if err != nil {
		return Feature{}, err
	}

	if len(unfinished) > 0 {
		ids := make([]string, len(unfinished))
		for i, t := range unfinished {
			ids[i] = t.ID
		}

		return Feature{}, fmt.Errorf("%w: %s. Finish or cancel them, or finish anyway", ErrUnfinished, strings.Join(ids, ", "))
	}

	return f, nil
}

// archiveTickets cancels the feature's unfinished tickets (only left when the
// finish was forced), then archives all of them.
func (s *Service) archiveTickets(ctx context.Context, feature string) error {
	unfinished, err := s.unfinishedTickets(ctx, feature)
	if err != nil {
		return err
	}

	for _, t := range unfinished {
		t.Status, t.UpdatedAt = StatusCanceled, s.now()
		if err := s.Store.UpdateTicket(ctx, t); err != nil {
			return err
		}
	}

	_, err = s.Store.ArchiveFeatureTickets(ctx, feature)

	return err
}

func (s *Service) unfinishedTickets(ctx context.Context, feature string) ([]Ticket, error) {
	all, err := s.Store.ListTickets(ctx, Filter{})
	if err != nil {
		return nil, err
	}

	unfinished := []Ticket{}

	for _, t := range all {
		if t.Feature == feature && (t.Status == StatusOpen || t.Status == StatusInProgress) {
			unfinished = append(unfinished, t)
		}
	}

	return unfinished, nil
}

func describeFeature(f Feature) string {
	if f.Status == FeatureStarted {
		return "not active"
	}

	return string(f.Status)
}
