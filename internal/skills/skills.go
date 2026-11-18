// Package skills finds the skills the agents get, and changes the ones the
// human may change (ADR-0011).
//
// A skill is a directory holding a SKILL.md: a YAML header (name,
// description, and keys Nobody Inc does not use, such as allowed-tools or
// hooks), then the instructions. Every agent gets three kinds of skills:
//
//   - the project's, in .claude/skills/ of the directory it works in: Claude
//     Code loads them itself, as agents keep the "project" settings source;
//   - the user's personal skills, in skills/ of Claude Code's configuration
//     directory ($CLAUDE_CONFIG_DIR, else ~/.claude): Claude Code would only
//     load them with the "user" settings source, which agents do not get
//     (ADR-0003). Nobody Inc links them into a staging directory (Stage) that
//     every agent gets with --add-dir;
//   - Claude Code's built-in skills, which the daemon discovers by starting
//     claude without reaching the API (DiscoverBuiltin).
//
// A personal skill named like a project skill is shadowed: agents get the
// project's, and the personal one is left out of the staging directory.
//
// Load reads the project and personal skills; skillfile.go reads and writes
// a SKILL.md, keeping the header keys it does not know.
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Scope tells where a skill comes from.
type Scope string

// The scopes. Only project and personal skills can be changed.
const (
	ScopeProject  Scope = "project"
	ScopePersonal Scope = "personal"
	ScopeBuiltin  Scope = "builtin"
)

// Writable reports whether the control room may change skills of this scope.
func (s Scope) Writable() bool {
	return s == ScopeProject || s == ScopePersonal
}

// Skill is a project or personal skill.
type Skill struct {
	Name        string
	Description string
	Scope       Scope
	// Dir is the skill's directory, holding its SKILL.md.
	Dir string
	// HasHooks is true when the header declares hooks: invoking the skill
	// asks for permission (checked by test/contract).
	HasHooks bool
	// Shadowed is true for a personal skill named like a project skill:
	// agents get the project's.
	Shadowed bool
	// Digest identifies the content of the SKILL.md, so that a change is
	// noticed (org reloads, agent restarts).
	Digest string
}

// Found is what Load found.
type Found struct {
	// Skills are the project skills, then the personal ones, each by name.
	Skills []Skill
	// Ignored are skills that could not be read, as "<dir>: <why>"; for a
	// warning.
	Ignored []string
}

// Get returns the skill of that scope and name.
func (f Found) Get(scope Scope, name string) (Skill, bool) {
	for _, s := range f.Skills {
		if s.Scope == scope && s.Name == name {
			return s, true
		}
	}

	return Skill{}, false
}

// ForAgent returns the skill agents get under that name: the project's when
// there is one, else the personal one.
func (f Found) ForAgent(name string) (Skill, bool) {
	if s, ok := f.Get(ScopeProject, name); ok {
		return s, true
	}

	return f.Get(ScopePersonal, name)
}

// Personal returns the personal skills agents get: those not shadowed by a
// project skill.
func (f Found) Personal() []Skill {
	var out []Skill

	for _, s := range f.Skills {
		if s.Scope == ScopePersonal && !s.Shadowed {
			out = append(out, s)
		}
	}

	return out
}

// dirMode is the mode of the directories Nobody Inc creates for skills.
const dirMode = 0o750

// skillFile is the file that makes a directory a skill.
const skillFile = "SKILL.md"

// validName is Claude Code's rule for skill names, which are also the
// names of their directories.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidName reports whether name can name a skill.
func ValidName(name string) bool {
	return validName.MatchString(name)
}

// ProjectDir is where the project's skills live: .claude/skills of the
// project's main checkout.
func ProjectDir(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "skills")
}

// PersonalDir is where the user's personal skills live: skills/ of Claude
// Code's configuration directory, which is $CLAUDE_CONFIG_DIR when set, else
// ~/.claude. (This is not where ~/.claude.json lives: without
// CLAUDE_CONFIG_DIR, that file is in the home directory itself.)
func PersonalDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "skills"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the personal skills: %w", err)
	}

	return filepath.Join(home, ".claude", "skills"), nil
}

// Dir returns the directory of a skill of a writable scope.
func Dir(projectDir, personalDir string, scope Scope, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("skill name %q: use lowercase letters, digits and hyphens (at most 64)", name)
	}

	switch scope {
	case ScopeProject:
		return filepath.Join(ProjectDir(projectDir), name), nil
	case ScopePersonal:
		return filepath.Join(personalDir, name), nil
	case ScopeBuiltin:
		return "", fmt.Errorf("skill %s: built-in skills cannot be changed", name)
	default:
		return "", fmt.Errorf("skill scope %q: use project or personal", scope)
	}
}

// Load reads the skills of the project at projectDir (its main checkout) and
// the personal skills in personalDir. Missing directories hold no skills; a
// skill that cannot be read is left out and listed in Found.Ignored, so that
// one broken file does not stop the organization from loading.
func Load(projectDir, personalDir string) (Found, error) {
	var found Found

	for _, src := range []struct {
		scope Scope
		dir   string
	}{
		{ScopeProject, ProjectDir(projectDir)},
		{ScopePersonal, personalDir},
	} {
		if src.dir == "" {
			continue
		}

		skills, ignored, err := readScope(src.scope, src.dir)
		if err != nil {
			return Found{}, err
		}

		found.Skills = append(found.Skills, skills...)
		found.Ignored = append(found.Ignored, ignored...)
	}

	for i, s := range found.Skills {
		if s.Scope == ScopePersonal {
			_, inProject := found.Get(ScopeProject, s.Name)
			found.Skills[i].Shadowed = inProject
		}
	}

	return found, nil
}

// readScope reads every skill directory of dir.
func readScope(scope Scope, dir string) ([]Skill, []string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}

	if err != nil {
		return nil, nil, fmt.Errorf("read %s skills: %w", scope, err)
	}

	var (
		skills  []Skill
		ignored []string
	)

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())

		if !isDir(path) || strings.HasPrefix(e.Name(), ".") {
			continue
		}

		s, err := readSkill(scope, path)
		if errors.Is(err, fs.ErrNotExist) {
			continue // a directory without SKILL.md is not a skill
		}

		if err != nil {
			ignored = append(ignored, fmt.Sprintf("%s: %v", path, err))

			continue
		}

		skills = append(skills, s)
	}

	slices.SortFunc(skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })

	return skills, ignored, nil
}

// readSkill reads the skill in dir. Its name is its directory's name, as for
// Claude Code.
func readSkill(scope Scope, dir string) (Skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, skillFile))
	if err != nil {
		return Skill{}, err
	}

	f, err := ParseFile(data)
	if err != nil {
		return Skill{}, err
	}

	sum := sha256.Sum256(data)

	return Skill{
		Name:        filepath.Base(dir),
		Description: f.Description,
		Scope:       scope,
		Dir:         dir,
		HasHooks:    f.HasHooks(),
		Digest:      hex.EncodeToString(sum[:]),
	}, nil
}

// isDir follows symbolic links: a skill may be a link to a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}
