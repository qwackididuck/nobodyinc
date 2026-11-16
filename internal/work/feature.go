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
	ID    string `json:"id"` // "F-3"
	Title string `json:"title"`
	// Description is the need: what is wanted and why.
	Description        string `json:"description,omitempty"`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty"`
	// Slug, BaseBranch and Branch are set when the feature starts.
	Slug       string        `json:"slug"`
	BaseBranch string        `json:"base_branch"`
	Branch     string        `json:"branch"`
	Active     bool          `json:"active"`
	Status     FeatureStatus `json:"status"`
	// ProposalMessage is the "propose" message the director's proposal sent
	// to the human; the decision answers it.
	ProposalMessage string `json:"proposal_message,omitempty"`
	RefusalReason   string `json:"refusal_reason,omitempty"`
	// MergeCommit is the merge commit on the base branch once finished (""
	// when the feature brought nothing).
	MergeCommit string    `json:"merge_commit,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// FeatureStatus is where a feature stands.
type FeatureStatus string

// Feature statuses: the director proposes, the human starts or refuses
// (docs/god.md). A feature the human creates starts at once. The human
// finishes it by merging it into its base branch (finish.go).
const (
	FeatureProposed FeatureStatus = "proposed"
	FeatureStarted  FeatureStatus = "started"
	FeatureRefused  FeatureStatus = "refused"
	FeatureFinished FeatureStatus = "finished"
)

// FeatureDraft is what the human provides to start a feature, or the
// director to propose one.
type FeatureDraft struct {
	Title       string
	Description string
	// AcceptanceCriteria say how to know the feature is done.
	AcceptanceCriteria string
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
	// ReviewFeature describes what the feature branch brings to its base.
	ReviewFeature(ctx context.Context, f Feature) (Changes, error)
	// FinishFeature merges the feature branch into its base branch and
	// returns the merge commit ("" when there was nothing to merge).
	FinishFeature(ctx context.Context, f Feature) (string, error)
}

// StartFeature creates a feature, prepares its branches and makes it the
// active one. Only the human starts features directly; the director proposes
// them (ProposeFeature).
func (s *Service) StartFeature(ctx context.Context, by string, d FeatureDraft) (Feature, error) {
	if by != org.HumanAddress {
		return Feature{}, fmt.Errorf("%w: only the human starts features", ErrForbidden)
	}

	f, err := newFeature(by, d, s.now())
	if err != nil {
		return Feature{}, err
	}

	if f, err = s.prepare(ctx, f); err != nil {
		return Feature{}, err
	}

	if f, err = s.Store.InsertFeature(ctx, f); err != nil {
		return Feature{}, err
	}

	return s.activate(ctx, f)
}

// ProposeFeature records a feature the director proposes, without touching
// git or the agents: it starts only if the human agrees
// (StartProposedFeature). The proposal is sent to the human through
// OnFeatureProposed.
func (s *Service) ProposeFeature(ctx context.Context, by string, d FeatureDraft) (Feature, error) {
	if !s.Org.Current().IsGod(by) {
		return Feature{}, fmt.Errorf("%w: only the director proposes features", ErrForbidden)
	}

	f, err := newFeature(by, d, s.now())
	if err != nil {
		return Feature{}, err
	}

	f.Status = FeatureProposed

	if f, err = s.Store.InsertFeature(ctx, f); err != nil {
		return Feature{}, err
	}

	if s.OnFeatureProposed != nil {
		if f.ProposalMessage = s.OnFeatureProposed(f); f.ProposalMessage != "" {
			if err := s.Store.UpdateFeature(ctx, f); err != nil {
				return Feature{}, err
			}
		}
	}

	return f, nil
}

// StartProposedFeature starts a feature the director proposed: its branches
// are prepared and it becomes the active one. Only the human decides.
func (s *Service) StartProposedFeature(ctx context.Context, by, id string) (Feature, error) {
	f, err := s.proposal(ctx, by, id)
	if err != nil {
		return Feature{}, err
	}

	if f, err = s.prepare(ctx, f); err != nil {
		return Feature{}, err
	}

	f.Status = FeatureStarted
	if err := s.Store.UpdateFeature(ctx, f); err != nil {
		return Feature{}, err
	}

	if f, err = s.activate(ctx, f); err != nil {
		return Feature{}, err
	}

	s.decided(f)

	return f, nil
}

// RefuseFeature turns down a feature the director proposed; the reason is
// passed on to it. Only the human decides.
func (s *Service) RefuseFeature(ctx context.Context, by, id, reason string) (Feature, error) {
	f, err := s.proposal(ctx, by, id)
	if err != nil {
		return Feature{}, err
	}

	f.Status = FeatureRefused
	f.RefusalReason = strings.TrimSpace(reason)

	if err := s.Store.UpdateFeature(ctx, f); err != nil {
		return Feature{}, err
	}

	s.decided(f)

	return f, nil
}

// proposal loads a feature waiting for the human's decision.
func (s *Service) proposal(ctx context.Context, by, id string) (Feature, error) {
	if by != org.HumanAddress {
		return Feature{}, fmt.Errorf("%w: only the human decides on a proposed feature", ErrForbidden)
	}

	f, err := s.Store.GetFeature(ctx, id)
	if err != nil {
		return Feature{}, err
	}

	if f.Status != FeatureProposed {
		return Feature{}, fmt.Errorf("%w: %s is %s, not proposed", ErrConflict, f.ID, f.Status)
	}

	return f, nil
}

func newFeature(by string, d FeatureDraft, now time.Time) (Feature, error) {
	if strings.TrimSpace(d.Title) == "" {
		return Feature{}, fmt.Errorf("%w: a feature needs a title", ErrInvalid)
	}

	return Feature{
		Title:              strings.TrimSpace(d.Title),
		Description:        d.Description,
		AcceptanceCriteria: d.AcceptanceCriteria,
		BaseBranch:         d.BaseBranch,
		Status:             FeatureStarted,
		CreatedBy:          by,
		CreatedAt:          now,
	}, nil
}

// prepare creates the feature's branch and the teams' worktrees.
func (s *Service) prepare(ctx context.Context, f Feature) (Feature, error) {
	if s.Workspace == nil {
		return f, nil
	}

	prepared, err := s.Workspace.PrepareFeature(ctx, f)
	if err != nil {
		return Feature{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	return prepared, nil
}

// activate makes f the active feature and tells OnFeatureStarted.
func (s *Service) activate(ctx context.Context, f Feature) (Feature, error) {
	if err := s.Store.ActivateFeature(ctx, f.ID); err != nil {
		return Feature{}, err
	}

	f.Active = true
	if s.OnFeatureStarted != nil {
		s.OnFeatureStarted(f)
	}

	return f, nil
}

func (s *Service) decided(f Feature) {
	if s.OnFeatureDecided != nil {
		s.OnFeatureDecided(f)
	}
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
