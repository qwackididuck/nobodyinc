package server

import (
	"context"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/staffing"
	"github.com/qwackididuck/nobodyinc/internal/subagents"
)

// Subagents (ADR-0018): the project's Claude Code subagents, read from
// .claude/agents at each call, and their import as agents into teams
// (staffing.Service.ImportSubagents).

// routeSubagents mounts the subagents endpoints.
func (s *server) routeSubagents(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/subagents", s.listSubagents)
	mux.HandleFunc("POST /api/subagents/import", s.importSubagents)
}

// subagentView is one entry of GET /api/subagents.
type subagentView struct {
	Name         string             `json:"name"`
	File         string             `json:"file"`
	Description  string             `json:"description"`
	Status       subagents.Status   `json:"status"`
	ImportedAs   string             `json:"imported_as,omitempty"`
	AgentName    string             `json:"agent_name,omitempty"`
	Agent        catalog.Definition `json:"agent,omitzero"`
	DroppedTools string             `json:"dropped_tools,omitempty"`
	IgnoredKeys  []string           `json:"ignored_keys,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// subagentList is GET /api/subagents.
type subagentList struct {
	Subagents []subagentView `json:"subagents"`
}

// subagentImport is the body of POST /api/subagents/import.
type subagentImport struct {
	Teams []struct {
		ID   string `json:"id"`
		Goal string `json:"goal"`
	} `json:"teams"`
	Agents []struct {
		Subagent string `json:"subagent"`
		Team     string `json:"team"`
		Name     string `json:"name"`
		Profile  string `json:"profile"`
	} `json:"agents"`
	Ignore []string `json:"ignore"`
}

func (s *server) listSubagents(w http.ResponseWriter, _ *http.Request) {
	list, err := s.subagentList()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)

		return
	}

	s.writeJSON(w, http.StatusOK, list)
}

// subagentList reads the project's subagents and tells where each stands.
func (s *server) subagentList() (subagentList, error) {
	o := s.Org.Current()

	all, err := subagents.Load(o.ProjectDir)
	if err != nil {
		return subagentList{}, err
	}

	list := subagentList{Subagents: []subagentView{}}

	for _, sub := range all {
		v := subagentView{
			Name: sub.Name, File: sub.File, Description: sub.Description, Error: sub.Err,
			AgentName: sub.AgentName, Agent: sub.Agent, DroppedTools: sub.DroppedTools, IgnoredKeys: sub.IgnoredKeys,
			Status: subagents.StatusPending,
		}

		if sub.Err == "" {
			v.Status, v.ImportedAs = subagents.StatusIn(o, sub.Name)
		}

		list.Subagents = append(list.Subagents, v)
	}

	return list, nil
}

func (s *server) importSubagents(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body subagentImport
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	imp := staffing.SubagentImport{Ignore: body.Ignore}
	for _, t := range body.Teams {
		imp.Teams = append(imp.Teams, org.Team{ID: t.ID, Goal: t.Goal})
	}

	for _, a := range body.Agents {
		imp.Agents = append(imp.Agents, staffing.ImportedAgent{Subagent: a.Subagent, Team: a.Team, Name: a.Name, Profile: a.Profile})
	}

	// The agents' processes must outlive this request.
	if _, err := s.Staff.ImportSubagents(context.WithoutCancel(r.Context()), imp); err != nil {
		s.staffError(w, err)

		return
	}

	list, err := s.subagentList()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)

		return
	}

	s.writeJSON(w, http.StatusOK, list)
}
