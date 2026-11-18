package staffing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// Saving, copying and deleting skills reloads the organization and brings
// every agent in line, resuming its session (ADR-0011).
func TestSkillChangesRestartEveryAgent(t *testing.T) {
	t.Parallel()

	s, a := newService(t)
	ctx := context.Background()
	project := s.Org.Current().ProjectDir

	if err := s.SaveSkill(ctx, skills.ScopeProject, "review", "Reviews code.", "Check the tests."); err != nil {
		t.Fatal(err)
	}

	if got, ok := s.Org.Current().Skills.Get(skills.ScopeProject, "review"); !ok || got.Description != "Reviews code." {
		t.Fatalf("skills after saving: %+v", s.Org.Current().Skills)
	}

	if a.count() != 1 || a.last() != "dev1@dev" {
		t.Fatalf("applied %v, want dev1@dev", a.calls)
	}

	// Saving the same content again changes nothing: nobody restarts.
	if err := s.SaveSkill(ctx, skills.ScopeProject, "review", "Reviews code.", "Check the tests."); err != nil {
		t.Fatal(err)
	}

	if a.count() != 1 {
		t.Fatalf("an unchanged skill restarted %v", a.calls)
	}

	// A new body is a change: preloaded skills follow it.
	if err := s.SaveSkill(ctx, skills.ScopeProject, "review", "Reviews code.", "Check the tests and the docs."); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSkill(ctx, skills.ScopeProject, "review"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "review")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted skill still on disk: %v", err)
	}

	if a.count() != 3 {
		t.Fatalf("applied %v, want 3 restarts", a.calls)
	}
}

// A personal skill copied into the project shadows the personal one; a
// second copy is refused.
//
// Not parallel: personal skills live in the CLAUDE_CONFIG_DIR shared by the
// tests (main_test.go), and every project sees them; another test reloading
// meanwhile would see its agents' skills change. Go runs the parallel tests
// once this one has ended, and its cleanup removed the skill.
func TestCopySkillToProject(t *testing.T) { //nolint:paralleltest // see above
	s, _ := newService(t)
	ctx := context.Background()

	const name = "copy-probe"

	if err := s.SaveSkill(ctx, skills.ScopePersonal, name, "Mine.", "Body."); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.DeleteSkill(context.Background(), skills.ScopePersonal, name) })

	if err := s.CopySkillToProject(ctx, name); err != nil {
		t.Fatal(err)
	}

	found := s.Org.Current().Skills
	if p, _ := found.Get(skills.ScopePersonal, name); !p.Shadowed {
		t.Errorf("the personal skill should be shadowed by its copy: %+v", found.Skills)
	}

	if err := s.CopySkillToProject(ctx, name); !errors.Is(err, ErrExists) {
		t.Fatalf("second copy = %v, want ErrExists", err)
	}
}

func TestSkillChangesRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func(s *Service) error
		wantErr error
	}{
		{
			name: "a built-in skill",
			change: func(s *Service) error {
				return s.SaveSkill(context.Background(), skills.ScopeBuiltin, "loop", "L.", "")
			},
			wantErr: ErrInvalid,
		},
		{
			name: "an invalid name",
			change: func(s *Service) error {
				return s.SaveSkill(context.Background(), skills.ScopeProject, "../x", "X.", "")
			},
			wantErr: ErrInvalid,
		},
		{
			name: "no description",
			change: func(s *Service) error {
				return s.SaveSkill(context.Background(), skills.ScopeProject, "empty", " ", "Body.")
			},
			wantErr: ErrInvalid,
		},
		{
			name:    "deleting a missing skill",
			change:  func(s *Service) error { return s.DeleteSkill(context.Background(), skills.ScopeProject, "ghost") },
			wantErr: ErrNotFound,
		},
		{
			name:    "copying a missing personal skill",
			change:  func(s *Service) error { return s.CopySkillToProject(context.Background(), "ghost") },
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, a := newService(t)

			if err := tt.change(s); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			if a.count() != 0 {
				t.Fatalf("a refused change applied %v", a.calls)
			}
		})
	}
}
