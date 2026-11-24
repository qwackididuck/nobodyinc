package server

import (
	"errors"
	"net/http"
	"slices"
)

// routeSettings mounts how the daemon runs: its settings, and the project's
// environment variables (env.go).
func (s *server) routeSettings(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.updateSettings)
	s.routeEnv(mux)
}

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
