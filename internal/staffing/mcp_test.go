package staffing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
)

// projectMCPWriter stands in for Claude Code: it writes the project scope's
// .mcp.json, as `claude mcp add-json --scope project` would.
type projectMCPWriter struct {
	dir   string
	calls []string
}

func (w *projectMCPWriter) Add(_ context.Context, scope mcpservers.Scope, name string, cfg mcpservers.Config) error {
	w.calls = append(w.calls, "add "+string(scope)+" "+name)

	return w.change(func(servers map[string]any) error {
		servers[name] = cfg

		return nil
	})
}

func (w *projectMCPWriter) Remove(_ context.Context, scope mcpservers.Scope, name string) error {
	w.calls = append(w.calls, "remove "+string(scope)+" "+name)

	return w.change(func(servers map[string]any) error {
		if _, ok := servers[name]; !ok {
			return mcpservers.ErrNotFound
		}

		delete(servers, name)

		return nil
	})
}

func (w *projectMCPWriter) change(edit func(map[string]any) error) error {
	path := mcpservers.ProjectFile(w.dir)

	file := struct {
		MCPServers map[string]any `json:"mcpServers"` //nolint:tagliatelle // Claude Code's format
	}{MCPServers: map[string]any{}}

	if data, err := os.ReadFile(filepath.Clean(path)); err == nil {
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
	}

	if err := edit(file.MCPServers); err != nil {
		return err
	}

	data, err := json.Marshal(file)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// Adding or removing a server reloads the organization and brings every
// agent in line, resuming its session: MCP servers are launch flags.
func TestMCPServerChangesRestartEveryAgent(t *testing.T) {
	t.Parallel()

	s, a := newService(t)
	w := &projectMCPWriter{dir: s.Org.Current().ProjectDir}
	s.MCP = w
	ctx := context.Background()

	if err := s.AddMCPServer(ctx, mcpservers.ScopeProject, "ragflow", mcpservers.Config{"type": "http", "url": "http://r"}); err != nil {
		t.Fatal(err)
	}

	if srv, ok := s.Org.Current().MCP["ragflow"]; !ok || srv.Scope != mcpservers.ScopeProject {
		t.Fatalf("servers after the change: %v", s.Org.Current().MCP)
	}

	if strings.Join(a.calls, ",") != "dev1@dev" {
		t.Fatalf("applied %v, want dev1@dev resuming its session", a.calls)
	}

	if err := s.RemoveMCPServer(ctx, mcpservers.ScopeProject, "ragflow"); err != nil {
		t.Fatal(err)
	}

	if _, ok := s.Org.Current().MCP["ragflow"]; ok || a.count() != 2 {
		t.Fatalf("ragflow should be gone and dev1 applied again: %v, %v", s.Org.Current().MCP, a.calls)
	}
}

func TestMCPServerChangesRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func(s *Service) error
		wantErr error
	}{
		{
			name: "a secret in clear",
			change: func(s *Service) error {
				return s.AddMCPServer(context.Background(), mcpservers.ScopeProject, "gitlab",
					mcpservers.Config{"command": "gitlab-mcp", "env": map[string]any{"GITLAB_TOKEN": "glpat-123"}})
			},
			wantErr: ErrInvalid,
		},
		{
			name: "an unknown scope",
			change: func(s *Service) error {
				return s.AddMCPServer(context.Background(), "team", "x", mcpservers.Config{"command": "x"})
			},
			wantErr: ErrInvalid,
		},
		{
			name: "removing a server the scope does not have",
			change: func(s *Service) error {
				return s.RemoveMCPServer(context.Background(), mcpservers.ScopeProject, "ghost")
			},
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, a := newService(t)
			w := &projectMCPWriter{dir: s.Org.Current().ProjectDir}
			s.MCP = w

			if err := tt.change(s); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			if a.count() != 0 {
				t.Fatalf("a refused change applied %v", a.calls)
			}
		})
	}
}

func TestMCPServerChangesNeedAWriter(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)

	err := s.AddMCPServer(context.Background(), mcpservers.ScopeProject, "x", mcpservers.Config{"command": "x"})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("error = %v, want ErrRefused", err)
	}
}
