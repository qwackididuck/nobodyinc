package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinTemplatesAreValid(t *testing.T) {
	t.Parallel()

	templates, err := New(t.TempDir()).List()
	if err != nil {
		t.Fatal(err)
	}

	if len(templates) == 0 {
		t.Fatal("no built-in template")
	}

	for _, tt := range templates {
		if !tt.Builtin || tt.Description == "" || tt.Version < 1 || tt.Role == "" || tt.Prompt == "" {
			t.Errorf("built-in template %q needs a description, a version, a role and a prompt: %+v", tt.Name, tt)
		}
	}
}

func TestUserTemplatesExtendAndOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dev.md"), "---\ndescription: My dev\nversion: 7\n---\nMine.\n")
	writeFile(t, filepath.Join(dir, "pentester.md"), "---\ndescription: Breaks things\n---\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a template")

	c := New(dir)

	dev, err := c.Get("dev")
	if err != nil {
		t.Fatal(err)
	}

	if dev.Builtin || dev.Version != 7 || dev.Prompt != "Mine." || dev.Role != "" {
		t.Fatalf("user dev.md should replace the built-in one entirely, got %+v", dev)
	}

	if _, err := c.Get("pentester"); err != nil {
		t.Fatalf("user template not listed: %v", err)
	}

	if archi, err := c.Get("archi"); err != nil || !archi.Builtin {
		t.Fatalf("built-in archi should still be there: %+v, %v", archi, err)
	}

	if _, err := c.Get("notes"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-markdown file should be ignored, got %v", err)
	}
}

func TestListReportsTheBrokenFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.md"), "no front matter")

	_, err := New(dir).List()
	if err == nil || !strings.Contains(err.Error(), "broken.md") {
		t.Fatalf("error = %v, want one naming broken.md", err)
	}
}

func TestSave(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "not", "created", "yet")
	c := New(dir)

	def := Definition{Description: "Writes docs", Version: 1, Role: "Writer", Prompt: "Write docs."}
	if err := c.Save("writer", def); err != nil {
		t.Fatal(err)
	}

	got, err := c.Get("writer")
	if err != nil {
		t.Fatal(err)
	}

	if got.Builtin || got.Role != "Writer" || got.Prompt != "Write docs." {
		t.Fatalf("saved template read back as %+v", got)
	}

	if err := c.Save("Bad Name", def); err == nil {
		t.Fatal("invalid name accepted")
	}

	if err := c.Save("x", Definition{Permissions: Permissions{Profile: "yolo"}}); err == nil {
		t.Fatal("invalid profile accepted")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRecruit(t *testing.T) {
	t.Parallel()

	tmpl := Template{Name: "dev",
		Description: "Writes code", Version: 3, Role: "Developer",
		Tools: []string{"Read"}, Permissions: Permissions{Allow: []string{"Edit"}}, Prompt: "Code."}

	d := tmpl.Recruit()

	if d.From != "dev@3" || d.Description != "" || d.Version != 0 || d.Role != "Developer" || d.Prompt != "Code." {
		t.Fatalf("recruit = %+v", d)
	}

	d.Tools[0] = "Bash"
	d.Permissions.Allow[0] = "Bash(*)"

	if tmpl.Tools[0] != "Read" || tmpl.Permissions.Allow[0] != "Edit" {
		t.Fatal("editing the recruit changed the template")
	}
}
