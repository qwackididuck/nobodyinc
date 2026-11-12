package work

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Feature is a request the organization works on ("add the order API"). It
// has its own git branch in the project, where every team's work ends up;
// merging it anywhere is the user's decision (see docs/workspaces.md).
type Feature struct {
	ID          string    `json:"id"` // "F-3"
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Slug        string    `json:"slug"`
	BaseBranch  string    `json:"base_branch"`
	Branch      string    `json:"branch"`
	Active      bool      `json:"active"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// FeatureDraft is what the human provides to start a feature.
type FeatureDraft struct {
	Title       string
	Description string
	// BaseBranch is where the feature branch starts; empty means the branch
	// the project is currently on.
	BaseBranch string
}

// Workspace ties tickets to the project's git repository (implemented in
// internal/daemon on top of internal/workspace). Nil means no git: tickets
// then work without branches.
type Workspace interface {
	// PrepareFeature completes a feature (slug, branches) and creates its
	// branch and the teams' worktrees.
	PrepareFeature(ctx context.Context, f Feature) (Feature, error)
	// BeforeClaim brings the latest validated work into the claimer's team.
	BeforeClaim(ctx context.Context, f Feature, agent string) error
	// BeforeDone delivers the team's work to the feature branch and returns
	// the commits it brought, as deliverables.
	BeforeDone(ctx context.Context, f Feature, agent string) ([]Deliverable, error)
}

// StartFeature creates a feature, prepares its branches and makes it the
// active one. Only the human starts features.
func (s *Service) StartFeature(ctx context.Context, by string, d FeatureDraft) (Feature, error) {
	if by != org.HumanAddress {
		return Feature{}, fmt.Errorf("%w: only the human starts features", ErrForbidden)
	}

	if strings.TrimSpace(d.Title) == "" {
		return Feature{}, fmt.Errorf("%w: a feature needs a title", ErrInvalid)
	}

	f := Feature{
		Title:       strings.TrimSpace(d.Title),
		Description: d.Description,
		BaseBranch:  d.BaseBranch,
		CreatedBy:   by,
		CreatedAt:   s.now(),
	}

	if s.Workspace != nil {
		var err error
		if f, err = s.Workspace.PrepareFeature(ctx, f); err != nil {
			return Feature{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
	}

	f, err := s.Store.InsertFeature(ctx, f)
	if err != nil {
		return Feature{}, err
	}

	if err := s.Store.ActivateFeature(ctx, f.ID); err != nil {
		return Feature{}, err
	}

	f.Active = true
	if s.OnFeatureStarted != nil {
		s.OnFeatureStarted(f)
	}

	return f, nil
}

// FormatFeatureID turns a stored feature number into its ID: 3 → "F-3".
func FormatFeatureID(n int64) string {
	return "F-" + strconv.FormatInt(n, 10)
}

// ParseFeatureID turns "F-3" (or "3") into 3.
func ParseFeatureID(id string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(id)), "F-"), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: %q (expected F-<number>)", ErrInvalid, id)
	}

	return n, nil
}
