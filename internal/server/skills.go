package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// Skills (ADR-0011): every agent gets the project's skills, the user's
// personal skills and Claude Code's built-in skills, but those it excludes.
// The API lists them, with what the control room warns about (hooks, not
// committed, shadowed), and creates, changes, deletes and copies project and
// personal skills (staffing.Service). Built-in skills come from the daemon's
// discovery (skills.Builtin).

// routeSkills mounts the skills endpoints.
func (s *server) routeSkills(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/skills", s.listSkills)
	mux.HandleFunc("POST /api/skills/builtin/refresh", s.refreshBuiltinSkills)
	mux.HandleFunc("GET /api/skills/{scope}/{name}", s.getSkill)
	mux.HandleFunc("PUT /api/skills/{scope}/{name}", s.saveSkill)
	mux.HandleFunc("DELETE /api/skills/{scope}/{name}", s.deleteSkill)
	mux.HandleFunc("POST /api/skills/personal/{name}/copy-to-project", s.copySkillToProject)
}

// skillView is one entry of the skills list.
type skillView struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scope       skills.Scope `json:"scope"`
	HasHooks    bool         `json:"has_hooks"`
	// Committed is set for project skills only.
	Committed *bool `json:"committed,omitempty"`
	// Shadowed is set for personal skills only.
	Shadowed *bool `json:"shadowed,omitempty"`
}

// skillList is GET /api/skills.
type skillList struct {
	Skills       []skillView `json:"skills"`
	BuiltinError string      `json:"builtin_error,omitempty"`
}

// skillInput is the body of PUT /api/skills/{scope}/{name}.
type skillInput struct {
	Description string `json:"description"`
	Body        string `json:"body"`
}

// skillDetail is GET /api/skills/{scope}/{name}.
type skillDetail struct {
	Name        string       `json:"name"`
	Scope       skills.Scope `json:"scope"`
	Description string       `json:"description"`
	Body        string       `json:"body"`
	OtherKeys   string       `json:"other_keys"`
	HasHooks    bool         `json:"has_hooks"`
}

func (s *server) listSkills(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.skillList(r.Context()))
}

func (s *server) refreshBuiltinSkills(w http.ResponseWriter, r *http.Request) {
	if s.BuiltinSkills != nil {
		s.BuiltinSkills.Refresh(r.Context())
	}

	s.writeJSON(w, http.StatusOK, s.skillList(r.Context()))
}

// skillList lists the project and personal skills, then the built-in ones.
// Whether a project skill is committed is asked to git at each call: a
// commit made outside Nobody Inc does not reload the organization.
func (s *server) skillList(ctx context.Context) skillList {
	o := s.Org.Current()
	list := skillList{Skills: []skillView{}}

	uncommitted, err := skills.Uncommitted(ctx, o.ProjectDir, o.Skills)
	if err != nil {
		s.Logger.Warn("cannot tell which project skills are committed", "err", err)
	}

	for _, sk := range o.Skills.Skills {
		v := skillView{Name: sk.Name, Description: sk.Description, Scope: sk.Scope, HasHooks: sk.HasHooks}

		switch sk.Scope {
		case skills.ScopeProject:
			if err == nil {
				committed := !slices.Contains(uncommitted, sk.Name)
				v.Committed = &committed
			}
		case skills.ScopePersonal:
			v.Shadowed = &sk.Shadowed
		case skills.ScopeBuiltin:
			// not part of the organization: listed below
		}

		list.Skills = append(list.Skills, v)
	}

	builtin, builtinErr := s.builtinSkills()
	if builtinErr != nil {
		list.BuiltinError = builtinErr.Error()
	}

	for _, name := range builtin {
		list.Skills = append(list.Skills, skillView{Name: name, Scope: skills.ScopeBuiltin})
	}

	return list
}

func (s *server) builtinSkills() ([]string, error) {
	if s.BuiltinSkills == nil {
		return nil, errors.New("this daemon does not discover the built-in skills")
	}

	return s.BuiltinSkills.Names()
}

func (s *server) getSkill(w http.ResponseWriter, r *http.Request) {
	scope, name := skills.Scope(r.PathValue("scope")), r.PathValue("name")
	if !scope.Writable() {
		s.writeError(w, http.StatusBadRequest, errors.New("scope: use project or personal"))

		return
	}

	sk, ok := s.Org.Current().Skills.Get(scope, name)
	if !ok {
		s.writeError(w, http.StatusNotFound, errors.New("no "+string(scope)+" skill "+name))

		return
	}

	f, err := skills.ReadFile(sk.Dir)
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

	s.writeJSON(w, http.StatusOK, skillDetail{
		Name: name, Scope: scope, Description: f.Description, Body: f.Body, OtherKeys: other, HasHooks: f.HasHooks(),
	})
}

func (s *server) saveSkill(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body skillInput
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	scope, name := skills.Scope(r.PathValue("scope")), r.PathValue("name")
	if err := s.Staff.SaveSkill(context.WithoutCancel(r.Context()), scope, name, body.Description, body.Body); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, s.skillList(r.Context()))
}

func (s *server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	scope, name := skills.Scope(r.PathValue("scope")), r.PathValue("name")
	if err := s.Staff.DeleteSkill(context.WithoutCancel(r.Context()), scope, name); err != nil {
		s.staffError(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) copySkillToProject(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	if err := s.Staff.CopySkillToProject(context.WithoutCancel(r.Context()), r.PathValue("name")); err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, s.skillList(r.Context()))
}
