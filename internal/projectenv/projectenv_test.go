package projectenv

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), FileName)

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	return s, path
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, value string
		want        error
	}{
		{"GITLAB_TOKEN", "abc", nil},
		{"_x1", "v", nil},
		{"1BAD", "v", ErrInvalid},
		{"A=B", "v", ErrInvalid},
		{"", "v", ErrInvalid},
		{"HAS SPACE", "v", ErrInvalid},
		{"EMPTY", "", ErrInvalid},
		{"NUL", "a\x00b", ErrInvalid},
		{"NOBODY_AGENT", "x", ErrReserved},
		{"NOBODY_HOME", "x", ErrReserved},
		{"GIT_AUTHOR_NAME", "x", ErrReserved},
		{"GIT_COMMITTER_EMAIL", "x", ErrReserved},
		{"CLAUDECODE", "1", ErrReserved},
		{"CLAUDE_CODE_SESSION_ID", "1", ErrReserved},
		{"CLAUDE_CODE_MESSAGING_TOKEN", "1", ErrReserved},
		{"ANTHROPIC_API_KEY", "sk", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := Check(tt.name, tt.value); !errors.Is(err, tt.want) {
				t.Fatalf("Check(%q) = %v, want %v", tt.name, err, tt.want)
			}
		})
	}
}

func TestSetDeleteAndReopen(t *testing.T) {
	t.Parallel()

	s, path := open(t)

	if err := s.Set("B_KEY", "two"); err != nil {
		t.Fatal(err)
	}

	if err := s.Set("A_KEY", "one"); err != nil {
		t.Fatal(err)
	}

	if err := s.Set("B_KEY", "deux"); err != nil {
		t.Fatal(err)
	}

	if got := s.Names(); !slices.Equal(got, []string{"A_KEY", "B_KEY"}) {
		t.Fatalf("Names = %v", got)
	}

	if err := s.Delete("A_KEY"); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete("A_KEY"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	if v, ok := reopened.Lookup("B_KEY"); !ok || v != "deux" {
		t.Fatalf("after reopening, B_KEY = %q, %v", v, ok)
	}

	if _, ok := reopened.Lookup("A_KEY"); ok {
		t.Fatal("A_KEY should be gone")
	}
}

func TestFileIsPrivate(t *testing.T) {
	t.Parallel()

	s, path := open(t)

	if err := s.Set("TOKEN", "secret"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("env.yaml is %o, want 600", perm)
	}
}

func TestOpenRefusesBrokenFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("- not\n- a map\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("a file that is not a map must be refused")
	}
}

func TestOverlay(t *testing.T) {
	t.Parallel()

	s, _ := open(t)

	for name, value := range map[string]string{"ANTHROPIC_API_KEY": "project", "GITLAB_TOKEN": "glpat"} {
		if err := s.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}

	base := []string{"PATH=/bin", "ANTHROPIC_API_KEY=user", "HOME=/h"}
	got := s.Overlay(base)
	want := []string{"PATH=/bin", "HOME=/h", "ANTHROPIC_API_KEY=project", "GITLAB_TOKEN=glpat"}

	if !slices.Equal(got, want) {
		t.Fatalf("Overlay = %v, want %v", got, want)
	}

	if base[1] != "ANTHROPIC_API_KEY=user" {
		t.Fatal("Overlay must not modify its base: the daemon's environment stays the user's")
	}
}

func TestNilStore(t *testing.T) {
	t.Parallel()

	var s *Store

	if got := s.Overlay([]string{"A=1"}); !slices.Equal(got, []string{"A=1"}) {
		t.Fatalf("nil Overlay = %v", got)
	}

	if _, ok := s.Lookup("A"); ok {
		t.Fatal("nil Lookup found something")
	}

	if got := s.Names(); len(got) != 0 {
		t.Fatalf("nil Names = %v", got)
	}
}
