package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/staffing"
)

// Agents and templates: recruitment, edition, leave and dismissal
// (docs/agents.md). Changes are made by staffing.Service; the agent's process
// outlives the request, hence the contexts detached from it.

var errNoStaff = errors.New("agents cannot be changed by this daemon")

// recruitment is the body of POST /api/teams/{team}/agents: exactly one of
// Template (recruit) and Definition (create directly).
type recruitment struct {
	Name       string              `json:"name"`
	Template   string              `json:"template"`
	Definition *catalog.Definition `json:"definition"`
}

func (s *server) recruitAgent(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body recruitment
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	if (body.Template == "") == (body.Definition == nil) {
		s.writeError(w, http.StatusBadRequest, errors.New("give either a template to recruit from, or a definition"))

		return
	}

	ctx, team := context.WithoutCancel(r.Context()), r.PathValue("team")

	var (
		m   org.Member
		err error
	)

	if body.Template != "" {
		m, err = s.Staff.Recruit(ctx, team, body.Name, body.Template)
	} else {
		m, err = s.Staff.Create(ctx, team, body.Name, *body.Definition)
	}

	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusCreated, s.agentView(m))
}

func (s *server) getAgentDefinition(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	d, err := s.Staff.Definition(r.PathValue("address"))
	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, d)
}

func (s *server) editAgentDefinition(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	opts, ok := s.applyOptions(w, r)
	if !ok {
		return
	}

	var d catalog.Definition
	if err := decodeJSON(w, r, &d, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	saved, err := s.Staff.Edit(context.WithoutCancel(r.Context()), r.PathValue("address"), d, opts)
	if err != nil {
		s.staffError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, saved)
}

func (s *server) putAgentOnLeave(w http.ResponseWriter, r *http.Request) {
	s.changeAgent(w, r, s.Staff.Leave)
}

func (s *server) returnAgentFromLeave(w http.ResponseWriter, r *http.Request) {
	s.changeAgent(w, r, s.Staff.Return)
}

func (s *server) dismissAgent(w http.ResponseWriter, r *http.Request) {
	s.changeAgent(w, r, s.Staff.Dismiss)
}

// changeAgent runs a change that takes only the agent and the apply options,
// and answers 202: the change is applied once the agent is idle.
func (s *server) changeAgent(w http.ResponseWriter, r *http.Request,
	change func(ctx context.Context, address string, opts runtime.ApplyOptions) error,
) {
	if !s.hasStaff(w) {
		return
	}

	opts, ok := s.applyOptions(w, r)
	if !ok {
		return
	}

	if err := change(context.WithoutCancel(r.Context()), r.PathValue("address"), opts); err != nil {
		s.staffError(w, err)

		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *server) listTemplates(w http.ResponseWriter, _ *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	templates, err := s.Catalog.List()
	if err != nil {
		s.Logger.Error("list templates", "err", err)
		s.writeError(w, http.StatusInternalServerError, err) // names the broken file

		return
	}

	s.writeJSON(w, http.StatusOK, append([]catalog.Template{}, templates...))
}

func (s *server) getTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	t, err := s.Catalog.Get(r.PathValue("name"))
	if err != nil {
		s.templateError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, t)
}

func (s *server) saveTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var d catalog.Definition
	if err := decodeJSON(w, r, &d, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	// A template is not an agent: it has no origin and is never on leave.
	d.From, d.OnLeave = "", false

	name := r.PathValue("name")
	if err := s.Catalog.Save(name, d); err != nil {
		if errors.Is(err, catalog.ErrInvalid) {
			s.writeError(w, http.StatusBadRequest, err)
		} else {
			s.templateError(w, err)
		}

		return
	}

	t, err := s.Catalog.Get(name)
	if err != nil {
		s.templateError(w, err)

		return
	}

	s.writeJSON(w, http.StatusOK, t)
}

func (s *server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	err := s.Catalog.Delete(r.PathValue("name"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, catalog.ErrBuiltin):
		s.writeError(w, http.StatusConflict, err)
	default:
		s.templateError(w, err)
	}
}

// applyOptions reads the fresh and now query parameters.
func (s *server) applyOptions(w http.ResponseWriter, r *http.Request) (runtime.ApplyOptions, bool) {
	var opts runtime.ApplyOptions

	for name, target := range map[string]*bool{"fresh": &opts.Fresh, "now": &opts.Now} {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}

		v, err := strconv.ParseBool(raw)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, errors.New(name+" must be true or false"))

			return opts, false
		}

		*target = v
	}

	return opts, true
}

func (s *server) hasStaff(w http.ResponseWriter) bool {
	if s.Staff == nil || s.Catalog == nil {
		s.writeError(w, http.StatusNotImplemented, errNoStaff)

		return false
	}

	return true
}

func (s *server) staffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, staffing.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err)
	case errors.Is(err, staffing.ErrExists):
		s.writeError(w, http.StatusConflict, err)
	case errors.Is(err, staffing.ErrInvalid):
		s.writeError(w, http.StatusBadRequest, err)
	default:
		s.Logger.Error("change agent", "err", err)
		s.writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *server) templateError(w http.ResponseWriter, err error) {
	if errors.Is(err, catalog.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, err)

		return
	}

	s.Logger.Error("read template", "err", err)
	s.writeError(w, http.StatusInternalServerError, err)
}
