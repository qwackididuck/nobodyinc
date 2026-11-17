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
		if !tt.Builtin || tt.Description == "" || tt.Version < 1 || tt.Role == "" || tt.Expertise == "" || tt.Prompt == "" {
			t.Errorf("built-in template %q needs a description, a version, a role, an expertise and a prompt: %+v", tt.Name, tt)
		}

		if err := tt.Validate(); err != nil {
			t.Errorf("built-in template %q is invalid: %v", tt.Name, err)
		}

		// Templates are about a trade: they name roles, never addresses or
		// teams, which belong to the organization (docs/agents.md).
		if strings.Contains(tt.Prompt, "@") {
			t.Errorf("built-in template %q names an address", tt.Name)
		}
	}
}

func TestUserTemplatesExtendAndOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "developer.md"), "---\ndescription: My dev\nversion: 7\n---\nMine.\n")
	writeFile(t, filepath.Join(dir, "pentester.md"), "---\ndescription: Breaks things\n---\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a template")

	c := New(dir)

	dev, err := c.Get("developer")
	if err != nil {
		t.Fatal(err)
	}

	if dev.Builtin || !dev.OverridesBuiltin || dev.Version != 7 || dev.Prompt != "Mine." || dev.Role != "" {
		t.Fatalf("user developer.md should replace the built-in one entirely, got %+v", dev)
	}

	if _, err := c.Get("pentester"); err != nil {
		t.Fatalf("user template not listed: %v", err)
	}

	if architect, err := c.Get("architect"); err != nil || !architect.Builtin {
		t.Fatalf("built-in architect should still be there: %+v, %v", architect, err)
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

func TestDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "developer.md"), "---\ndescription: My dev\n---\n")
	writeFile(t, filepath.Join(dir, "pentester.md"), "---\ndescription: Breaks things\n---\n")

	c := New(dir)

	if err := c.Delete("pentester"); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Get("pentester"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted template still there: %v", err)
	}

	// Deleting an override brings the built-in template back.
	if err := c.Delete("developer"); err != nil {
		t.Fatal(err)
	}

	if dev, err := c.Get("developer"); err != nil || !dev.Builtin {
		t.Fatalf("the built-in developer should be back: %+v, %v", dev, err)
	}

	if err := c.Delete("developer"); !errors.Is(err, ErrBuiltin) {
		t.Fatalf("deleting a built-in template: err = %v, want ErrBuiltin", err)
	}

	if err := c.Delete("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting an unknown template: err = %v, want ErrNotFound", err)
	}
}

func TestTheDirectorIsNotInTheCatalog(t *testing.T) {
	t.Parallel()

	templates, err := New(t.TempDir()).List()
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range templates {
		if tt.Name == DirectorName || tt.Role == "Director" {
			t.Fatalf("the director is listed in the catalog: %+v", tt)
		}
	}

	god, err := Director()
	if err != nil {
		t.Fatal(err)
	}

	if god.Name != DirectorName || god.Role == "" || god.Prompt == "" || god.Expertise == "" || god.Model != "opus" {
		t.Fatalf("director = %+v", god)
	}

	if err := god.Validate(); err != nil {
		t.Fatalf("the built-in director is invalid: %v", err)
	}
}

func TestExpertiseIsReadWrittenAndRecruited(t *testing.T) {
	t.Parallel()

	tmpl := Template{Name: "qa", Version: 1, Role: "QA", Expertise: "test plans, edge cases", Prompt: "Test."}

	data, err := Format(tmpl.Recruit())
	if err != nil {
		t.Fatal(err)
	}

	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	if d.Expertise != "test plans, edge cases" {
		t.Fatalf("expertise lost: %q in\n%s", d.Expertise, data)
	}
}
