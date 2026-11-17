package staffing

import (
	"context"
	"errors"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// MCP servers (ADR-0010). They live in Claude Code's own files, written by
// Claude Code itself (mcpservers.Writer); Nobody Inc validates what it is
// asked to write, never writes a secret, then reloads: every agent is
// brought in line once idle, resuming its session (changedAgents).

// MCPWriter adds and removes MCP servers in Claude Code's scopes
// (implemented by mcpservers.Writer).
type MCPWriter interface {
	Add(ctx context.Context, scope mcpservers.Scope, name string, cfg mcpservers.Config) error
	Remove(ctx context.Context, scope mcpservers.Scope, name string) error
}

// errNoMCPWriter refuses MCP changes when the service has no writer.
var errNoMCPWriter = fmt.Errorf("%w: MCP servers cannot be changed by this daemon", ErrRefused)

// AddMCPServer adds, or replaces, a server in scope, then brings every agent
// in line. Env and headers values must refer to environment variables
// (mcpservers.Validate).
func (s *Service) AddMCPServer(ctx context.Context, scope mcpservers.Scope, name string, cfg mcpservers.Config) error {
	if err := checkScope(scope); err != nil {
		return err
	}

	if err := mcpservers.Validate(name, cfg); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	return s.changeMCP(ctx, func(w MCPWriter) error { return w.Add(ctx, scope, name, cfg) })
}

// RemoveMCPServer removes a server from scope, then brings every agent in
// line.
func (s *Service) RemoveMCPServer(ctx context.Context, scope mcpservers.Scope, name string) error {
	if err := checkScope(scope); err != nil {
		return err
	}

	return s.changeMCP(ctx, func(w MCPWriter) error {
		err := w.Remove(ctx, scope, name)
		if errors.Is(err, mcpservers.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		}

		return err
	})
}

// changeMCP makes one change through the writer, then reloads and applies.
func (s *Service) changeMCP(ctx context.Context, change func(MCPWriter) error) error {
	if s.MCP == nil {
		return errNoMCPWriter
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := change(s.MCP); err != nil {
		return err
	}

	return s.reloadAndApply(ctx, "", runtime.ApplyOptions{})
}

func checkScope(scope mcpservers.Scope) error {
	if !scope.Valid() {
		return fmt.Errorf("%w: scope %q: use project, local or user", ErrInvalid, scope)
	}

	return nil
}
