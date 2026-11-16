package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

// DaemonInfo describes how the daemon was launched, for the control room's
// Settings tab. These are launch options: the API does not change them.
type DaemonInfo struct {
	ProjectID   string
	StateDir    string
	Address     string
	ClaudePath  string
	StartAgents bool
}

// settingsView is the body of GET and PUT /api/settings.
type settingsView struct {
	ProjectDir       string   `json:"project_dir"`
	ProjectID        string   `json:"project_id"`
	StateDir         string   `json:"state_dir"`
	Address          string   `json:"address"`
	Origins          []string `json:"origins"`
	ClaudePath       string   `json:"claude_path"`
	StartAgents      bool     `json:"start_agents"`
	ConfigProtection string   `json:"config_protection"`
}

// settingsUpdate is the body of PUT /api/settings.
type settingsUpdate struct {
	ConfigProtection string `json:"config_protection"`
}

func (s *server) getSettings(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *server) updateSettings(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		s.writeError(w, http.StatusNotImplemented, errors.New("settings cannot be changed on this daemon"))

		return
	}

	var u settingsUpdate
	if err := decodeJSON(w, r, &u, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if _, err := s.Settings.SetConfigProtection(u.ConfigProtection); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	s.writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *server) settingsView() settingsView {
	v := settingsView{
		ProjectDir:  s.Org.Current().ProjectDir,
		ProjectID:   s.Info.ProjectID,
		StateDir:    s.Info.StateDir,
		Address:     s.Info.Address,
		Origins:     slices.Clone(s.Origins),
		ClaudePath:  s.Info.ClaudePath,
		StartAgents: s.Info.StartAgents,
	}

	if v.Origins == nil {
		v.Origins = []string{}
	}

	if s.Settings != nil {
		v.ConfigProtection = s.Settings.Current().ConfigProtection
	}

	return v
}

// mcpServerView is one entry of GET /api/mcp. Secrets are never returned:
// only the names of the environment variables the server refers to.
type mcpServerView struct {
	Name   string        `json:"name"`
	Type   string        `json:"type"`
	Target string        `json:"target"`
	Env    []envVarState `json:"env"`
	UsedBy []string      `json:"used_by"`
}

type envVarState struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

func (s *server) listMCPServers(w http.ResponseWriter, _ *http.Request) {
	o := s.Org.Current()

	views := make([]mcpServerView, 0, len(o.MCP))
	for name, cfg := range o.MCP {
		views = append(views, mcpServerView{
			Name:   name,
			Type:   mcpType(cfg),
			Target: mcpTarget(cfg),
			Env:    s.envState(envReferences(cfg)),
			UsedBy: usersOf(o, name),
		})
	}

	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })

	s.writeJSON(w, http.StatusOK, views)
}

// mcpType is the server's transport: its "type", or stdio when it only has a
// command (Claude Code's default).
func mcpType(cfg org.MCPServer) string {
	if t, ok := cfg["type"].(string); ok && t != "" {
		return t
	}

	return "stdio"
}

// mcpTarget is what the server runs or reaches: the command with its
// arguments, or the URL.
func mcpTarget(cfg org.MCPServer) string {
	if url, ok := cfg["url"].(string); ok && url != "" {
		return url
	}

	parts := []string{fmt.Sprint(cfg["command"])}

	if args, ok := cfg["args"].([]any); ok {
		for _, a := range args {
			parts = append(parts, fmt.Sprint(a))
		}
	}

	return strings.Join(parts, " ")
}

// envReference matches ${NAME} and ${NAME:-default}, as Claude Code expands
// them in MCP configurations.
var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}`)

// envReferences lists, sorted and without duplicates, the environment
// variables referred to anywhere in a server's configuration.
func envReferences(v any) []string {
	seen := map[string]bool{}
	collectEnvReferences(v, seen)

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// collectEnvReferences adds to seen the variables referred to in v, a value
// decoded from YAML (strings, maps and lists, at any depth).
func collectEnvReferences(v any, seen map[string]bool) {
	switch x := v.(type) {
	case string:
		for _, m := range envReference.FindAllStringSubmatch(x, -1) {
			seen[m[1]] = true
		}
	case map[string]any:
		for _, e := range x {
			collectEnvReferences(e, seen)
		}
	case []any:
		for _, e := range x {
			collectEnvReferences(e, seen)
		}
	}
}

func (s *server) envState(names []string) []envVarState {
	states := make([]envVarState, 0, len(names))
	for _, name := range names {
		value, ok := s.lookupEnv(name)
		states = append(states, envVarState{Name: name, Set: ok && value != ""})
	}

	return states
}

// usersOf lists the agents given an MCP server.
func usersOf(o *org.Org, server string) []string {
	users := []string{}

	for _, m := range o.Members() {
		if slices.Contains(m.Agent.MCP, server) {
			users = append(users, m.Address)
		}
	}

	return users
}
