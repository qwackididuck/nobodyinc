package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/commands"
)

// Commands (ADR-0017): every agent gets the project's commands and the
// user's personal commands. The API lists them, with what the control room
// warns about (allowed tools, not committed, shadowed), and creates,
// changes, deletes and copies them (staffing.Service).

// routeClaudeConfig mounts the endpoints of what every agent gets from
// Claude Code's own configuration: MCP servers, skills and commands.
func (s *server) routeClaudeConfig(mux *http.ServeMux) {
	s.routeMCP(mux)
	s.routeSkills(mux)
	s.routeCommands(mux)
}

// routeCommands mounts the commands endpoints.
func (s *server) routeCommands(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/commands", s.listCommands)
	mux.HandleFunc("GET /api/commands/{scope}/{name}", s.getCommand)
	mux.HandleFunc("PUT /api/commands/{scope}/{name}", s.saveCommand)
	mux.HandleFunc("DELETE /api/commands/{scope}/{name}", s.deleteCommand)
	mux.HandleFunc("POST /api/commands/personal/{name}/copy-to-project", s.copyCommandToProject)
}

// commandView is one entry of the commands list.
type commandView struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	ArgumentHint string         `json:"argument_hint,omitempty"`
	AllowedTools string         `json:"allowed_tools,omitempty"`
	Scope        commands.Scope `json:"scope"`
	// Committed is set for project commands only.
	Committed *bool `json:"committed,omitempty"`
	// Shadowed is set for personal commands only.
	Shadowed *bool `json:"shadowed,omitempty"`
}

// commandList is GET /api/commands.
type commandList struct {
	Commands []commandView `json:"commands"`
}

// commandInput is the body of PUT /api/commands/{scope}/{name}.
type commandInput struct {
	Description string `json:"description"`
	Body        string `json:"body"`
}

// commandDetail is GET /api/commands/{scope}/{name}.
type commandDetail struct {
	Name         string         `json:"name"`
	Scope        commands.Scope `json:"scope"`
	Description  string         `json:"description"`
	Body         string         `json:"body"`
	OtherKeys    string         `json:"other_keys"`
	AllowedTools string         `json:"allowed_tools,omitempty"`
}

func (s *server) listCommands(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.commandList(r.Context()))
}

// commandList lists the project commands, then the personal ones. Whether a
// project command is committed is asked to git at each call: a commit made
// outside Nobody Inc does not reload the organization.
func (s *server) commandList(ctx context.Context) commandList {
	o := s.Org.Current()
	list := commandList{Commands: []commandView{}}

	uncommitted, err := commands.Uncommitted(ctx, o.ProjectDir, o.Commands)
	if err != nil {
		s.Logger.Warn("cannot tell which project commands are committed", "err", err)
	}

	for _, c := range o.Commands.Commands {
		v := commandView{
			Name: c.Name, Description: c.Description, ArgumentHint: c.ArgumentHint, AllowedTools: c.AllowedTools, Scope: c.Scope,
		}

		switch c.Scope {
		case commands.ScopeProject:
			if err == nil {
				committed := !slices.Contains(uncommitted, c.Name)
				v.Committed = &committed
			}
		case commands.ScopePersonal:
			v.Shadowed = &c.Shadowed
		}

		list.Commands = append(list.Commands, v)
	}

	return list
}

func (s *server) getCommand(w http.ResponseWriter, r *http.Request) {
	scope, name := commands.Scope(r.PathValue("scope")), r.PathValue("name")
	if !scope.Valid() {
		s.writeError(w, http.StatusBadRequest, errors.New("scope: use project or personal"))

		return
	}

	c, ok := s.Org.Current().Commands.Get(scope, name)
	if !ok {
		s.writeError(w, http.StatusNotFound, errors.New("no "+string(scope)+" command "+name))

		return
	}

	f, err := commands.ReadFile(c.Path)
	if errors.Is(err, fs.ErrNotExist) {
		s.writeError(w, http.StatusNotFound, err)

		return
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)

		return
	}

	other, err := f.OtherKeys()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)

		return
	}

	s.writeJSON(w, http.StatusOK, commandDetail{
		Name: name, Scope: scope, Description: f.Description, Body: f.Body, OtherKeys: other, AllowedTools: c.AllowedTools,
	})
}

func (s *server) saveCommand(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body commandInput
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	scope, name := commands.Scope(r.PathValue("scope")), r.PathValue("name")
	if err := s.Staff.SaveCommand(context.WithoutCancel(r.Context()), scope, name, body.Description, body.Body); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, s.commandList(r.Context()))
}

func (s *server) deleteCommand(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	scope, name := commands.Scope(r.PathValue("scope")), r.PathValue("name")
	if err := s.Staff.DeleteCommand(context.WithoutCancel(r.Context()), scope, name); err != nil {
		s.staffError(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) copyCommandToProject(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	if err := s.Staff.CopyCommandToProject(context.WithoutCancel(r.Context()), r.PathValue("name")); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, s.commandList(r.Context()))
}
