// Package catalog holds agent templates: the job descriptions an agent is
// recruited from (docs/agents.md, ADR-0004).
//
// Templates come from two places:
//
//   - built-in templates, shipped inside the binary (builtin/*.md);
//   - the user's templates, in ~/.nobody/catalog/*.md.
//
// A user template with the same name as a built-in one replaces it entirely.
// The user directory is read on every call, so a file dropped there shows up
// without restarting the daemon.
//
// The package also defines the file format shared by templates and recruited
// agents (Definition, Parse, Format): recruiting is a copy.
package catalog

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
)

//go:embed builtin/*.md
var builtinFiles embed.FS

// The director is not in the catalog: every project has one, created from
// this definition (docs/god.md). Its name is also its address.
//
//go:embed director/god.md
var directorFile []byte

// DirectorName is the name, and address, of the director a project gets.
const DirectorName = "god"

// Director returns the built-in director as a template, ready to recruit.
func Director() (Template, error) {
	d, err := Parse(directorFile)
	if err != nil {
		return Template{}, fmt.Errorf("built-in director: %w", err)
	}

	return Template{Definition: d, Name: DirectorName, Builtin: true}, nil
}

// Errors returned by the catalog.
var (
	ErrNotFound = errors.New("template not found")
	ErrBuiltin  = errors.New("built-in templates cannot be deleted")
	ErrInvalid  = errors.New("invalid template")
)

const fileExt = ".md"

// Template is a named definition from the catalog.
type Template struct {
	Definition

	Name string `json:"name"`
	// Builtin is true for a template shipped with Nobody Inc and not
	// replaced by a user template.
	Builtin bool `json:"builtin"`
	// OverridesBuiltin is true for a user template that replaces a built-in
	// one: deleting it brings the built-in template back.
	OverridesBuiltin bool `json:"overrides_builtin,omitempty"`
}

// Recruit returns the definition of a new agent hired from this template: a
// copy that remembers where it comes from. The copy is independent: later
// changes to the template never reach it (ADR-0004).
func (t Template) Recruit() Definition {
	d := t.Definition
	d.Description = ""
	d.Version = 0
	d.From = fmt.Sprintf("%s@%d", t.Name, t.Version)
	d.OnLeave = false

	// Slices are copied so that editing the recruit never alters the template.
	d.Tools = slices.Clone(d.Tools)
	d.MCPExclude = slices.Clone(d.MCPExclude)
	d.SkillsExclude = slices.Clone(d.SkillsExclude)
	d.Skills = slices.Clone(d.Skills)
	d.Permissions.Allow = slices.Clone(d.Permissions.Allow)
	d.Permissions.Deny = slices.Clone(d.Permissions.Deny)

	return d
}

// Catalog lists and stores templates.
type Catalog struct {
	builtin fs.FS
	userDir string
}

// New returns the catalog made of the built-in templates and the user's
// templates in userDir (which may not exist yet).
func New(userDir string) *Catalog {
	sub, err := fs.Sub(builtinFiles, "builtin")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}

	return &Catalog{builtin: sub, userDir: userDir}
}

// List returns every template, sorted by name. A malformed file is an error
// naming that file, so that a hand-edited mistake is noticed at once.
func (c *Catalog) List() ([]Template, error) {
	builtin, err := readTemplates(c.builtin, true)
	if err != nil {
		return nil, fmt.Errorf("built-in templates: %w", err)
	}

	user, err := c.userTemplates()
	if err != nil {
		return nil, err
	}

	byName := map[string]Template{}
	for _, t := range builtin {
		byName[t.Name] = t
	}

	for _, t := range user {
		_, t.OverridesBuiltin = byName[t.Name]
		byName[t.Name] = t // user templates win
	}

	templates := make([]Template, 0, len(byName))
	for _, t := range byName {
		templates = append(templates, t)
	}

	slices.SortFunc(templates, func(a, b Template) int { return strings.Compare(a.Name, b.Name) })

	return templates, nil
}

// Get returns one template, or ErrNotFound.
func (c *Catalog) Get(name string) (Template, error) {
	templates, err := c.List()
	if err != nil {
		return Template{}, err
	}

	i := slices.IndexFunc(templates, func(t Template) bool { return t.Name == name })
	if i < 0 {
		return Template{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}

	return templates[i], nil
}

// Save writes a user template, creating it or replacing it. Saving under the
// name of a built-in template overrides that template.
func (c *Catalog) Save(name string, d Definition) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: name %q: use lowercase letters, digits and dashes", ErrInvalid, name)
	}

	if err := d.Validate(); err != nil {
		return fmt.Errorf("%w %q: %w", ErrInvalid, name, err)
	}

	data, err := Format(d)
	if err != nil {
		return err
	}

	return atomicfile.Write(filepath.Join(c.userDir, name+fileExt), data)
}

// Delete removes a user template. Deleting one that overrides a built-in
// template brings the built-in one back; built-in templates themselves
// cannot be deleted (ErrBuiltin). Agents recruited from it are not changed.
func (c *Catalog) Delete(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}

	err := os.Remove(filepath.Join(c.userDir, name+fileExt))
	if err == nil {
		return nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete template %q: %w", name, err)
	}

	if _, statErr := fs.Stat(c.builtin, name+fileExt); statErr == nil {
		return fmt.Errorf("%w: %q", ErrBuiltin, name)
	}

	return fmt.Errorf("%w: %q", ErrNotFound, name)
}

// userTemplates reads the user directory; a missing directory is empty.
func (c *Catalog) userTemplates() ([]Template, error) {
	templates, err := readTemplates(os.DirFS(c.userDir), false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("templates in %s: %w", c.userDir, err)
	}

	return templates, nil
}

// readTemplates parses every "*.md" file at the root of fsys.
func readTemplates(fsys fs.FS, builtin bool) ([]Template, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}

	var templates []Template

	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), fileExt)
		if e.IsDir() || !ok {
			continue
		}

		t, err := readTemplate(fsys, e.Name(), name)
		if err != nil {
			return nil, err
		}

		t.Builtin = builtin
		templates = append(templates, t)
	}

	return templates, nil
}

func readTemplate(fsys fs.FS, file, name string) (Template, error) {
	if !ValidName(name) {
		return Template{}, fmt.Errorf("%s: file name must be lowercase letters, digits and dashes", file)
	}

	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return Template{}, err
	}

	d, err := Parse(data)
	if err != nil {
		return Template{}, fmt.Errorf("%s: %w", file, err)
	}

	return Template{Name: name, Definition: d}, nil
}
