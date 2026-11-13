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

// ErrNotFound is returned when no template has the requested name.
var ErrNotFound = errors.New("template not found")

const fileExt = ".md"

// Template is a named definition from the catalog.
type Template struct {
	Definition

	Name string `json:"name"`
	// Builtin is true for a template shipped with Nobody Inc and not
	// replaced by a user template.
	Builtin bool `json:"builtin"`
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
	d.MCP = slices.Clone(d.MCP)
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
	for _, t := range append(builtin, user...) {
		byName[t.Name] = t // user templates come last and win
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
		return fmt.Errorf("template name %q: use lowercase letters, digits and dashes", name)
	}

	if err := d.Validate(); err != nil {
		return fmt.Errorf("template %q: %w", name, err)
	}

	data, err := Format(d)
	if err != nil {
		return err
	}

	return atomicfile.Write(filepath.Join(c.userDir, name+fileExt), data)
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
