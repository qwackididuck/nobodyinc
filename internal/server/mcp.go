package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// MCP servers (ADR-0010): every agent gets the servers of Claude Code's three
// scopes, read by org (internal/mcpservers). The API lists them without ever
// returning a value in clear, adds and removes them through Claude Code
// (staffing.Service), and lists a server's tools so that the human can
// exclude some from an agent.

// routeMCP mounts the MCP endpoints.
func (s *server) routeMCP(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mcp", s.listMCPServers)
	mux.HandleFunc("POST /api/mcp", s.addMCPServer)
	mux.HandleFunc("GET /api/mcp/{name}", s.getMCPServer)
	mux.HandleFunc("PUT /api/mcp/{name}", s.updateMCPServer)
	mux.HandleFunc("DELETE /api/mcp/{name}", s.removeMCPServer)
	mux.HandleFunc("GET /api/mcp/{name}/tools", s.listMCPTools)
}

// mcpServerView is one entry of GET /api/mcp. No value is returned in clear:
// only the names of the environment variables the server refers to, the
// command without its arguments, or the URL without its query.
type mcpServerView struct {
	Name       string           `json:"name"`
	Source     mcpservers.Scope `json:"source"`
	Type       string           `json:"type"`
	Target     string           `json:"target"`
	Env        []envVarState    `json:"env"`
	ExcludedBy []string         `json:"excluded_by"`
}

type envVarState struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

// mcpServerDetail is GET /api/mcp/{name}: the server to edit, its values
// written in clear emptied (mcpservers.Redact).
type mcpServerDetail struct {
	Name        string            `json:"name"`
	Source      mcpservers.Scope  `json:"source"`
	Server      mcpservers.Config `json:"server"`
	ClearValues []string          `json:"clear_values"`
}

// mcpServerChange is the body of PUT /api/mcp/{name}.
type mcpServerChange struct {
	Scope  mcpservers.Scope  `json:"scope"`
	Server mcpservers.Config `json:"server"`
}

// mcpServerInput is the body of POST /api/mcp.
type mcpServerInput struct {
	Name   string            `json:"name"`
	Scope  mcpservers.Scope  `json:"scope"`
	Server mcpservers.Config `json:"server"`
}

func (s *server) listMCPServers(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.mcpViews(s.Org.Current()))
}

func (s *server) mcpViews(o *org.Org) []mcpServerView {
	views := make([]mcpServerView, 0, len(o.MCP))
	for name, srv := range o.MCP {
		views = append(views, mcpServerView{
			Name:       name,
			Source:     srv.Scope,
			Type:       mcpservers.TypeOf(srv.Config),
			Target:     mcpTarget(srv.Config),
			Env:        s.envState(mcpservers.References(srv.Config)),
			ExcludedBy: excludersOf(o, name),
		})
	}

	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })

	return views
}

func (s *server) addMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body mcpServerInput
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.Staff.AddMCPServer(context.WithoutCancel(r.Context()), body.Scope, body.Name, body.Server); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, s.mcpViews(s.Org.Current()))
}

func (s *server) removeMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	scope := mcpservers.Scope(r.URL.Query().Get("scope"))
	if err := s.Staff.RemoveMCPServer(context.WithoutCancel(r.Context()), scope, r.PathValue("name")); err != nil {
		s.staffError(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) getMCPServer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	srv, ok := s.Org.Current().MCP[name]
	if !ok {
		s.writeError(w, http.StatusNotFound, errors.New("no MCP server named "+name))

		return
	}

	cfg, cleared := mcpservers.Redact(srv.Config)
	s.writeJSON(w, http.StatusOK, mcpServerDetail{Name: name, Source: srv.Scope, Server: cfg, ClearValues: cleared})
}

func (s *server) updateMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body mcpServerChange
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.Staff.UpdateMCPServer(context.WithoutCancel(r.Context()), body.Scope, r.PathValue("name"), body.Server); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, s.mcpViews(s.Org.Current()))
}

// listMCPTools starts the server to ask for its tools (mcpservers.ListTools).
func (s *server) listMCPTools(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	srv, ok := s.Org.Current().MCP[name]
	if !ok {
		s.writeError(w, http.StatusNotFound, errors.New("no MCP server named "+name))

		return
	}

	tools, err := mcpservers.ListTools(r.Context(), srv.Config, s.lookupEnv, s.Env.Overlay(os.Environ()))
	if err != nil {
		// The server, not the request, failed: a missing variable, a server
		// that does not start or does not answer.
		s.writeError(w, http.StatusBadGateway, err)

		return
	}

	s.writeJSON(w, http.StatusOK, tools)
}

// mcpTarget is what the server runs or reaches, without what may hold a
// secret: the command without its arguments, or the URL without its user,
// password and query.
func mcpTarget(cfg mcpservers.Config) string {
	if raw, ok := cfg["url"].(string); ok && raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return "(invalid URL)"
		}

		return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
	}

	command, _ := cfg["command"].(string)

	return command
}

func (s *server) envState(names []string) []envVarState {
	states := make([]envVarState, 0, len(names))
	for _, name := range names {
		value, ok := s.lookupEnv(name)
		states = append(states, envVarState{Name: name, Set: ok && value != ""})
	}

	return states
}

// excludersOf lists the agents that exclude the whole server.
func excludersOf(o *org.Org, server string) []string {
	agents := []string{}

	for _, m := range o.Members() {
		for _, entry := range m.Agent.MCPExclude {
			if name, tool, err := catalog.SplitExclusion(entry); err == nil && name == server && tool == "" && !slices.Contains(agents, m.Address) {
				agents = append(agents, m.Address)
			}
		}
	}

	return agents
}
