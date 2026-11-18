package mcpservers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// Errors returned for a server that Nobody Inc refuses to write.
var (
	ErrInvalid = errors.New("invalid MCP server")
	// ErrSecret refuses an env or headers value without a ${VARIABLE}
	// reference: Nobody Inc never writes a secret (ADR-0010).
	ErrSecret = errors.New("secrets are never written: refer to an environment variable instead, e.g. ${GITLAB_TOKEN}, " +
		"and set it in the environment the daemon is started from")
)

// validName is what Claude Code accepts as a server name.
var validName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Validate checks a server before it is written: a valid name other than the
// office server's, what its type needs (a command, or a URL), and env and
// headers values that refer to environment variables.
func Validate(name string, cfg Config) error {
	switch {
	case !validName.MatchString(name):
		return fmt.Errorf("%w: name %q: use letters, digits, dashes and underscores", ErrInvalid, name)
	case name == OfficeServer:
		return fmt.Errorf("%w: the name %q is reserved for the office server", ErrInvalid, OfficeServer)
	}

	if err := validateTransport(cfg); err != nil {
		return err
	}

	return validateNoSecret(cfg)
}

// validateTransport checks that the server has what its type needs.
func validateTransport(cfg Config) error {
	switch kind := TypeOf(cfg); kind {
	case "stdio":
		if stringField(cfg, "command") == "" {
			return fmt.Errorf("%w: a stdio server needs a command", ErrInvalid)
		}
	case "http", "sse":
		if stringField(cfg, "url") == "" {
			return fmt.Errorf("%w: an %s server needs a url", ErrInvalid, kind)
		}
	default:
		return fmt.Errorf("%w: type %q: use stdio, http or sse", ErrInvalid, kind)
	}

	return nil
}

// validateNoSecret checks that every env and headers value refers to an
// environment variable.
func validateNoSecret(cfg Config) error {
	for _, field := range []string{"env", "headers"} {
		values, _ := cfg[field].(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(values)) {
			if s, ok := values[key].(string); !ok || !RefersToVariable(s) {
				return fmt.Errorf("%w (%s.%s)", ErrSecret, field, key)
			}
		}
	}

	return nil
}

// Writer adds and removes servers through Claude Code itself (claude mcp
// add-json / remove), so that Claude Code keeps owning its files: their
// format, and the servers and keys Nobody Inc does not know.
type Writer struct {
	// ClaudePath is the claude executable; defaults to "claude" from PATH.
	ClaudePath string
	// ProjectDir is the project's main checkout: claude runs there, which is
	// where the project and local scopes belong.
	ProjectDir string
}

// Add writes the server in scope, replacing one of the same name there. The
// server must have been validated (Validate).
func (w Writer) Add(ctx context.Context, scope Scope, name string, cfg Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode server %s: %w", name, err)
	}

	// add-json refuses a name already taken in the scope: remove it first.
	if err := w.Remove(ctx, scope, name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	return w.run(ctx, "mcp", "add-json", "--scope", string(scope), name, string(data))
}

// ErrNotFound is returned when removing a server the scope does not have.
var ErrNotFound = errors.New("no such MCP server in this scope")

// Remove deletes the server from scope.
func (w Writer) Remove(ctx context.Context, scope Scope, name string) error {
	err := w.run(ctx, "mcp", "remove", "--scope", string(scope), name)
	if err != nil && strings.Contains(err.Error(), "No MCP server named") {
		return fmt.Errorf("%w: %s (%s)", ErrNotFound, name, scope)
	}

	return err
}

func (w Writer) run(ctx context.Context, args ...string) error {
	claude := w.ClaudePath
	if claude == "" {
		claude = "claude"
	}

	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = w.ProjectDir

	var out bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude %s: %w: %s", strings.Join(args[:3], " "), err, strings.TrimSpace(out.String()))
	}

	return nil
}
