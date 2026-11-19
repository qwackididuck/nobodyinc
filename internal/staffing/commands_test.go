package staffing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/commands"
)

// Saving and deleting commands reloads the organization and brings every
// agent in line, resuming its session (ADR-0017).
func TestCommandChangesRestartEveryAgent(t *testing.T) {
	t.Parallel()

	s, a := newService(t)
	ctx := context.Background()
	project := s.Org.Current().ProjectDir

	if err := s.SaveCommand(ctx, commands.ScopeProject, "front:test", "Runs the front tests.", "Run pnpm test."); err != nil {
		t.Fatal(err)
	}

	if got, ok := s.Org.Current().Commands.Get(commands.ScopeProject, "front:test"); !ok || got.Description != "Runs the front tests." {
		t.Fatalf("commands after saving: %+v", s.Org.Current().Commands)
	}

	if a.count() != 1 || a.last() != "dev1@dev" {
		t.Fatalf("applied %v, want dev1@dev", a.calls)
	}

	// Saving the same content again changes nothing: nobody restarts.
	if err := s.SaveCommand(ctx, commands.ScopeProject, "front:test", "Runs the front tests.", "Run pnpm test."); err != nil {
		t.Fatal(err)
	}

	if a.count() != 1 {
		t.Fatalf("an unchanged command restarted %v", a.calls)
	}

	if err := s.DeleteCommand(ctx, commands.ScopeProject, "front:test"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(project, ".claude", "commands", "front")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted command's directory is still on disk: %v", err)
	}

	if a.count() != 2 {
		t.Fatalf("applied %v, want 2 restarts", a.calls)
	}
}

// An existing command is saved where it is, even when its name would not be
// accepted for a new one.
func TestSaveCommandKeepsItsFile(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	path := filepath.Join(s.Org.Current().ProjectDir, ".claude", "commands", "Old_Name.md")
	writeTestFile(t, path, "---\nallowed-tools: Bash(go test *)\n---\nOld.\n")

	if _, err := s.Org.Reload(); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveCommand(ctx, commands.ScopeProject, "Old_Name", "Kept.", "New."); err != nil {
		t.Fatal(err)
	}

	c, ok := s.Org.Current().Commands.Get(commands.ScopeProject, "Old_Name")
	if !ok || c.Path != path || c.Description != "Kept." || c.AllowedTools != "Bash(go test *)" {
		t.Fatalf("command after saving: %+v (found %v)", c, ok)
	}
}

// A personal command copied into the project shadows the personal one; a
// second copy is refused.
//
// Not parallel: personal commands live in the CLAUDE_CONFIG_DIR shared by
// the tests (main_test.go), as personal skills do (TestCopySkillToProject).
func TestCopyCommandToProject(t *testing.T) { //nolint:paralleltest // see above
	s, _ := newService(t)
	ctx := context.Background()

	const name = "probe:copy"

	if err := s.SaveCommand(ctx, commands.ScopePersonal, name, "Mine.", "Body."); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.DeleteCommand(context.Background(), commands.ScopePersonal, name) })

	if err := s.CopyCommandToProject(ctx, name); err != nil {
		t.Fatal(err)
	}

	found := s.Org.Current().Commands
	if p, _ := found.Get(commands.ScopePersonal, name); !p.Shadowed {
		t.Errorf("the personal command should be shadowed by its copy: %+v", found.Commands)
	}

	if _, err := os.Stat(filepath.Join(s.Org.Current().ProjectDir, ".claude", "commands", "probe", "copy.md")); err != nil {
		t.Errorf("the copy should be at the same path in the project: %v", err)
	}

	if err := s.CopyCommandToProject(ctx, name); !errors.Is(err, ErrExists) {
		t.Fatalf("second copy = %v, want ErrExists", err)
	}
}

func TestCommandChangesRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func(s *Service) error
		wantErr error
	}{
		{
			name: "an unknown scope",
			change: func(s *Service) error {
				return s.SaveCommand(context.Background(), "builtin", "compact", "", "Body.")
			},
			wantErr: ErrInvalid,
		},
		{
			name: "an invalid name",
			change: func(s *Service) error {
				return s.SaveCommand(context.Background(), commands.ScopeProject, "../x", "", "Body.")
			},
			wantErr: ErrInvalid,
		},
		{
			name: "no prompt",
			change: func(s *Service) error {
				return s.SaveCommand(context.Background(), commands.ScopeProject, "empty", "Empty.", " ")
			},
			wantErr: ErrInvalid,
		},
		{
			name:    "deleting a missing command",
			change:  func(s *Service) error { return s.DeleteCommand(context.Background(), commands.ScopeProject, "ghost") },
			wantErr: ErrNotFound,
		},
		{
			name:    "copying a missing personal command",
			change:  func(s *Service) error { return s.CopyCommandToProject(context.Background(), "ghost") },
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, a := newService(t)

			if err := tt.change(s); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}

			if a.count() != 0 {
				t.Errorf("a refused change restarted %v", a.calls)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
