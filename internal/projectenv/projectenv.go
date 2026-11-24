// Package projectenv holds the environment variables of a project: the ones
// its agents' processes get on top of the daemon's own environment, e.g. the
// Claude key or the token of an MCP server (ADR-0020, docs/projectenv.md).
//
// They live in <state directory>/env.yaml, readable by the user only, and
// never in the project's .nobody/, which may be committed. A value is never
// returned by the API, shown or logged: only the names are.
//
// The variables apply to the processes Nobody Inc starts for the project
// (agents, the forks that answer urgent questions, the probing of MCP
// servers). They never reach the user's shell, nor another project.
package projectenv

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
)

// FileName is the file of a project's variables, inside its state directory.
const FileName = "env.yaml"

// Errors returned by Set and Delete; the API turns them into 400 and 404.
var (
	ErrInvalid  = errors.New("invalid environment variable")
	ErrReserved = errors.New("reserved environment variable")
	ErrNotFound = errors.New("no such environment variable")
)

var validName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// reservedPrefixes and reservedNames are what Nobody Inc sets itself, or
// removes on purpose (ADR-0003). Letting a project change them would break
// the hooks, the agents' git identity or their isolation.
var (
	reservedPrefixes = []string{
		"NOBODY_", "GIT_AUTHOR_", "GIT_COMMITTER_",
		"CLAUDE_CODE_SESSION_", "CLAUDE_CODE_MESSAGING_",
	}
	reservedNames = []string{
		"CLAUDECODE", "CLAUDE_PID", "CLAUDE_JOB_DIR", "CLAUDE_EFFORT",
		"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_CHILD_SESSION",
		"CLAUDE_CODE_SSE_PORT", "CLAUDE_CODE_HOST_SHORT_TEMP_DIRS",
	}
)

// Store is a project's variables, kept in memory and in its file. A nil
// *Store holds nothing and overlays nothing: it is what a daemon without a
// state directory uses.
type Store struct {
	path string

	mu   sync.RWMutex
	vars map[string]string
}

// Open reads the variables in path; a missing file means none.
func Open(path string) (*Store, error) {
	s := &Store{path: path, vars: map[string]string{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the project's environment: %w", err)
	}

	if err := yaml.Unmarshal(data, &s.vars); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if s.vars == nil {
		s.vars = map[string]string{}
	}

	return s, nil
}

// Check reports why name or value cannot be a project variable, or nil.
func Check(name, value string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("%w: %q is not a valid name (letters, digits and _, not starting with a digit)", ErrInvalid, name)
	}

	if Reserved(name) {
		return fmt.Errorf("%w: %s is set by Nobody Inc", ErrReserved, name)
	}

	if value == "" {
		return fmt.Errorf("%w: %s has an empty value (delete it instead)", ErrInvalid, name)
	}

	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%w: the value of %s holds a NUL character", ErrInvalid, name)
	}

	return nil
}

// Reserved reports whether Nobody Inc sets name itself (or removes it from
// the agents' environment), so that a project cannot.
func Reserved(name string) bool {
	if slices.Contains(reservedNames, name) {
		return true
	}

	return slices.ContainsFunc(reservedPrefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}

// Names lists the variables set, sorted.
func (s *Store) Names() []string {
	if s == nil {
		return []string{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return slices.Sorted(maps.Keys(s.vars))
}

// Lookup returns the value of a project variable.
func (s *Store) Lookup(name string) (string, bool) {
	if s == nil {
		return "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.vars[name]

	return v, ok
}

// Set adds or replaces a variable, and saves the file.
func (s *Store) Set(name, value string) error {
	if err := Check(name, value); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := maps.Clone(s.vars)
	next[name] = value

	return s.save(next)
}

// Delete removes a variable, and saves the file.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.vars[name]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	next := maps.Clone(s.vars)
	delete(next, name)

	return s.save(next)
}

// Overlay returns base ("NAME=value" entries, like os.Environ) with the
// project's variables on top: a project variable replaces the one of the same
// name. base is not modified, nor is the daemon's own environment: the result
// is for the process about to be started.
func (s *Store) Overlay(base []string) []string {
	if s == nil {
		return base
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.vars) == 0 {
		return base
	}

	env := make([]string, 0, len(base)+len(s.vars))

	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if _, overridden := s.vars[name]; !overridden {
			env = append(env, kv)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(s.vars)) {
		env = append(env, name+"="+s.vars[name])
	}

	return env
}

// save writes next, and adopts it only once it is on disk.
func (s *Store) save(next map[string]string) error {
	data, err := yaml.Marshal(next)
	if err != nil {
		return err
	}

	if err := atomicfile.WritePrivate(s.path, data); err != nil {
		return fmt.Errorf("save the project's environment: %w", err)
	}

	s.vars = next

	return nil
}
