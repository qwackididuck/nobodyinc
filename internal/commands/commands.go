// Package commands finds the commands the agents get, and changes them for
// the human (ADR-0017).
//
// A command is a markdown file, invoked as /<name>: an optional YAML header
// (description, argument-hint, allowed-tools, model,
// disable-model-invocation...), then the prompt (internal/frontmatter). A
// command in a sub-directory is named after its path, "/" written ":":
// front/test.md is front:test (checked by test/contract). Every agent gets
// two kinds of commands:
//
//   - the project's, in .claude/commands/ of the directory it works in:
//     Claude Code loads them itself, as agents keep the "project" settings
//     source;
//   - the user's personal commands, in commands/ of Claude Code's
//     configuration directory ($CLAUDE_CONFIG_DIR, else ~/.claude): Claude
//     Code would only load them with the "user" settings source, which agents
//     do not get (ADR-0003). Nobody Inc links them into the staging directory
//     of the personal skills (Stage), which every agent gets with --add-dir.
//
// A personal command named like a project command is shadowed: agents get
// the project's, and the personal one is left out of the staging directory.
package commands

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

// Scope tells where a command comes from.
type Scope string

// The scopes.
const (
	ScopeProject  Scope = "project"
	ScopePersonal Scope = "personal"
)

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool {
	return s == ScopeProject || s == ScopePersonal
}

// Command is a project or personal command.
type Command struct {
	// Name is how it is invoked, without the "/": its path under the
	// commands directory, without ".md", "/" written ":".
	Name         string
	Description  string
	ArgumentHint string
	// AllowedTools is the header's allowed-tools, "" for none: tools the
	// command lets run without asking while it runs (ADR-0017).
	AllowedTools string
	Scope        Scope
	// Path is the command's file.
	Path string
	// Shadowed is true for a personal command named like a project command:
	// agents get the project's.
	Shadowed bool
	// Digest identifies the content of the file, so that a change is noticed
	// (org reloads, agent restarts).
	Digest string
}

// Found is what Load found.
type Found struct {
	// Commands are the project commands, then the personal ones, each by
	// name.
	Commands []Command
	// Ignored are commands that could not be read, as "<path>: <why>"; for a
	// warning.
	Ignored []string
}

// Get returns the command of that scope and name.
func (f Found) Get(scope Scope, name string) (Command, bool) {
	for _, c := range f.Commands {
		if c.Scope == scope && c.Name == name {
			return c, true
		}
	}

	return Command{}, false
}

// Personal returns the personal commands agents get: those not shadowed by a
// project command.
func (f Found) Personal() []Command {
	var out []Command

	for _, c := range f.Commands {
		if c.Scope == ScopePersonal && !c.Shadowed {
			out = append(out, c)
		}
	}

	return out
}

// dirMode is the mode of the directories Nobody Inc creates for commands.
const dirMode = 0o750

// ext is the extension of a command file.
const ext = ".md"

// separator joins the segments of a command's name: front/test.md is
// front:test.
const separator = ":"

// validSegment is the rule for each segment of the name of a command the
// control room creates, the same as for skills. Commands found on disk keep
// whatever name their file gives them.
var validSegment = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidName reports whether name can name a new command: segments of
// lowercase letters, digits and hyphens, separated by ":".
func ValidName(name string) bool {
	for segment := range strings.SplitSeq(name, separator) {
		if !validSegment.MatchString(segment) {
			return false
		}
	}

	return true
}

// ProjectDir is where the project's commands live: .claude/commands of the
// project's main checkout.
func ProjectDir(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "commands")
}

// PersonalDir is where the user's personal commands live: commands/ of
// Claude Code's configuration directory, which is $CLAUDE_CONFIG_DIR when
// set, else ~/.claude.
func PersonalDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "commands"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the personal commands: %w", err)
	}

	return filepath.Join(home, ".claude", "commands"), nil
}

// Path returns the file of a new command of that scope and name.
func Path(projectDir, personalDir string, scope Scope, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("command name %q: use lowercase letters, digits and hyphens (at most 64), and \":\" for a sub-directory", name)
	}

	rel := filepath.FromSlash(strings.ReplaceAll(name, separator, "/")) + ext

	switch scope {
	case ScopeProject:
		return filepath.Join(ProjectDir(projectDir), rel), nil
	case ScopePersonal:
		return filepath.Join(personalDir, rel), nil
	default:
		return "", fmt.Errorf("command scope %q: use project or personal", scope)
	}
}

// Load reads the commands of the project at projectDir (its main checkout)
// and the personal commands in personalDir. Missing directories hold no
// commands; a command that cannot be read is left out and listed in
// Found.Ignored, so that one broken file does not stop the organization from
// loading.
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

		commands, ignored, err := readScope(src.scope, src.dir)
		if err != nil {
			return Found{}, err
		}

		found.Commands = append(found.Commands, commands...)
		found.Ignored = append(found.Ignored, ignored...)
	}

	for i, c := range found.Commands {
		if c.Scope == ScopePersonal {
			_, inProject := found.Get(ScopeProject, c.Name)
			found.Commands[i].Shadowed = inProject
		}
	}

	return found, nil
}

// readScope reads every command file under dir, sub-directories included.
// Hidden files and directories are skipped. A symbolic link to a file is
// followed; one to a directory is not (fs.WalkDir does not follow them).
func readScope(scope Scope, dir string) ([]Command, []string, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}

	var (
		commands []Command
		ignored  []string
	)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if skip, err := skipEntry(dir, path, d); skip {
			return err
		}

		c, err := readCommand(scope, dir, path)
		if err != nil {
			ignored = append(ignored, fmt.Sprintf("%s: %v", path, err))

			return nil
		}

		commands = append(commands, c)

		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read %s commands: %w", scope, err)
	}

	slices.SortFunc(commands, func(a, b Command) int { return strings.Compare(a.Name, b.Name) })

	return commands, ignored, nil
}

// skipEntry tells readScope to skip an entry that is not a command file:
// a directory (a hidden one with all it holds: filepath.SkipDir), a hidden
// file, or a file without the .md extension.
func skipEntry(root, path string, d fs.DirEntry) (bool, error) {
	hidden := path != root && strings.HasPrefix(d.Name(), ".")

	switch {
	case d.IsDir() && hidden:
		return true, filepath.SkipDir
	case d.IsDir(), hidden, !strings.HasSuffix(d.Name(), ext), !isFile(path):
		return true, nil
	default:
		return false, nil
	}
}

// readCommand reads the command file at path, under the commands directory
// root.
func readCommand(scope Scope, root, path string) (Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Command{}, err
	}

	f, err := ParseFile(data)
	if err != nil {
		return Command{}, err
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Command{}, err
	}

	sum := sha256.Sum256(data)

	return Command{
		Name:         nameOf(rel),
		Description:  f.Description,
		ArgumentHint: f.Value(argumentHintKey),
		AllowedTools: f.Value(allowedToolsKey),
		Scope:        scope,
		Path:         path,
		Digest:       hex.EncodeToString(sum[:]),
	}, nil
}

// nameOf is the name of the command whose file is at rel under the commands
// directory.
func nameOf(rel string) string {
	return strings.ReplaceAll(filepath.ToSlash(strings.TrimSuffix(rel, ext)), "/", separator)
}

// isFile follows symbolic links: a command may be a link to a file.
func isFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}
