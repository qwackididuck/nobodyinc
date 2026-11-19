package kb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string // "" = refused
	}{
		{path: "docs", want: "docs"},
		{path: "docs/", want: "docs"},
		{path: "./docs/kb", want: "docs/kb"},
		{path: "docs//adr/./", want: "docs/adr"},
		{path: "documentation", want: "documentation"},
		{path: ".github/docs", want: ".github/docs"},
		{path: ""},
		{path: "   "},
		{path: "."},
		{path: "./"},
		{path: "/etc"},
		{path: "../other"},
		{path: "docs/../.."},
		{path: "docs/../../x"},
		{path: ".nobody"},
		{path: ".nobody/teams"},
		{path: ".claude/docs"},
		{path: ".git"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got, err := CheckPath(tt.path)
			if tt.want == "" {
				if !errors.Is(err, ErrBadPath) {
					t.Fatalf("CheckPath(%q) = %q, %v; want ErrBadPath", tt.path, got, err)
				}

				return
			}

			if err != nil || got != tt.want {
				t.Fatalf("CheckPath(%q) = %q, %v; want %q", tt.path, got, err, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	project := t.TempDir()
	mustMkdir(t, filepath.Join(project, "docs", "adr"))
	mustMkdir(t, filepath.Join(project, ".nobody"))
	mustSymlink(t, outside, filepath.Join(project, "escape"))
	mustSymlink(t, filepath.Join(project, ".nobody"), filepath.Join(project, "config"))
	mustSymlink(t, filepath.Join(project, "docs"), filepath.Join(project, "manual"))

	realProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, path string
		want       string // "" = refused
	}{
		{name: "an existing folder", path: "docs", want: filepath.Join(realProject, "docs")},
		{name: "a folder to create", path: "docs/kb/new", want: filepath.Join(realProject, "docs", "kb", "new")},
		{name: "a link inside the project", path: "manual/adr", want: filepath.Join(realProject, "docs", "adr")},
		{name: "a link leaving the project", path: "escape"},
		{name: "through a link leaving the project", path: "escape/sub"},
		{name: "a link into .nobody", path: "config"},
		{name: "a path the text rules refuse", path: "../x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(project, tt.path)
			if tt.want == "" {
				if !errors.Is(err, ErrBadPath) {
					t.Fatalf("Resolve(%q) = %q, %v; want ErrBadPath", tt.path, got, err)
				}

				return
			}

			if err != nil || got != tt.want {
				t.Fatalf("Resolve(%q) = %q, %v; want %q", tt.path, got, err, tt.want)
			}
		})
	}
}

func TestCleanPagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string // "" = refused
	}{
		{path: "adr/0003.md", want: "adr/0003.md"},
		{path: "/adr/0003.md", want: "adr/0003.md"},
		{path: "adr/./0003.md", want: "adr/0003.md"},
		{path: ""},
		{path: "."},
		{path: "../secret.md"},
		{path: "adr/../../secret.md"},
		{path: ".hidden/page.md"},
		{path: "adr/.draft.md"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got, err := CleanPagePath(tt.path)
			if tt.want == "" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("CleanPagePath(%q) = %q, %v; want ErrNotFound", tt.path, got, err)
				}

				return
			}

			if err != nil || got != tt.want {
				t.Fatalf("CleanPagePath(%q) = %q, %v; want %q", tt.path, got, err, tt.want)
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// writeFiles writes files (path relative to root → content) under root.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		mustMkdir(t, filepath.Dir(full))

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
