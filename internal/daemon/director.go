package daemon

import (
	"context"
	"errors"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/staffing"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// errFixedOrg answers the director's staffing tools when the organization
// cannot change (a fixed *org.Org, as in some tests).
var errFixedOrg = errors.New("the organization cannot be changed by this daemon")

// directorOffice backs the director's office tools (officemcp.Director):
// the overview of the organization, and changes to its agents through the
// same staffing service as the API, so the daemon remains the only writer of
// .nobody/ (ADR-0004).
//
// The agents it starts outlive the MCP request that asked for them, hence
// the contexts detached from it (context.WithoutCancel), as in the API.
type directorOffice struct {
	org      org.Source
	staff    *staffing.Service // nil when the organization is fixed
	catalog  *catalog.Catalog
	tickets  *work.Service
	statuses *runtime.Statuses
}

// Overview gathers the active feature, and each team's agents and tickets.
func (d *directorOffice) Overview(ctx context.Context) (officemcp.Overview, error) {
	o := d.org.Current()

	var overview officemcp.Overview

	features, err := d.tickets.Store.ListFeatures(ctx)
	if err != nil {
		return overview, err
	}

	for _, f := range features {
		if f.Active {
			overview.ActiveFeature = &officemcp.FeatureSummary{ID: f.ID, Title: f.Title, Branch: f.Branch}
		}
	}

	tickets, err := d.tickets.Store.ListTickets(ctx, work.Filter{})
	if err != nil {
		return overview, err
	}

	for _, t := range o.Teams {
		overview.Teams = append(overview.Teams, d.teamOverview(t, tickets))
	}

	return overview, nil
}

// Templates lists the catalog.
func (d *directorOffice) Templates() ([]catalog.Template, error) {
	return d.catalog.List()
}

// CreateTeam declares a new team.
func (d *directorOffice) CreateTeam(ctx context.Context, id, goal, prompt string) (org.Team, error) {
	if d.staff == nil {
		return org.Team{}, errFixedOrg
	}

	return d.staff.CreateTeam(context.WithoutCancel(ctx), id, goal, prompt)
}

// DeleteTeam deletes a team and dismisses its members.
func (d *directorOffice) DeleteTeam(ctx context.Context, id string) error {
	if d.staff == nil {
		return errFixedOrg
	}

	return d.staff.DeleteTeam(context.WithoutCancel(ctx), id, runtime.ApplyOptions{})
}

// Definition reads an agent's definition.
func (d *directorOffice) Definition(address string) (catalog.Definition, error) {
	if d.staff == nil {
		return catalog.Definition{}, errFixedOrg
	}

	return d.staff.Definition(address)
}

// Recruit hires an agent from a template. The director cannot recruit a
// second director: it recruits into teams.
func (d *directorOffice) Recruit(ctx context.Context, team, name, template string) (org.Member, error) {
	if d.staff == nil {
		return org.Member{}, errFixedOrg
	}

	if team == org.DirectorTeam {
		return org.Member{}, errNoTeam
	}

	return d.staff.Recruit(context.WithoutCancel(ctx), team, name, template)
}

// Create adds an agent written from scratch to a team.
func (d *directorOffice) Create(ctx context.Context, team, name string, def catalog.Definition) (org.Member, error) {
	if d.staff == nil {
		return org.Member{}, errFixedOrg
	}

	if team == org.DirectorTeam {
		return org.Member{}, errNoTeam
	}

	return d.staff.Create(context.WithoutCancel(ctx), team, name, def)
}

// Edit replaces an agent's definition.
func (d *directorOffice) Edit(ctx context.Context, address string, def catalog.Definition, fresh bool) (catalog.Definition, error) {
	if d.staff == nil {
		return catalog.Definition{}, errFixedOrg
	}

	return d.staff.Edit(context.WithoutCancel(ctx), address, def, runtime.ApplyOptions{Fresh: fresh})
}

// Leave puts an agent on leave.
func (d *directorOffice) Leave(ctx context.Context, address string) error {
	if d.staff == nil {
		return errFixedOrg
	}

	return d.staff.Leave(context.WithoutCancel(ctx), address, runtime.ApplyOptions{})
}

// Return brings an agent back from leave.
func (d *directorOffice) Return(ctx context.Context, address string) error {
	if d.staff == nil {
		return errFixedOrg
	}

	return d.staff.Return(context.WithoutCancel(ctx), address, runtime.ApplyOptions{})
}

// Dismiss dismisses an agent.
func (d *directorOffice) Dismiss(ctx context.Context, address string) error {
	if d.staff == nil {
		return errFixedOrg
	}

	return d.staff.Dismiss(context.WithoutCancel(ctx), address, runtime.ApplyOptions{})
}

// errNoTeam refuses recruiting without a team: there is only one director.
var errNoTeam = errors.New("give the team to recruit into")

func (d *directorOffice) teamOverview(t org.Team, tickets []work.Ticket) officemcp.TeamOverview {
	team := officemcp.TeamOverview{
		Team: t.ID, Goal: t.Goal, Prompt: t.Prompt,
		Agents: []officemcp.AgentState{}, Open: []officemcp.TicketSummary{}, InProgress: []officemcp.TicketSummary{},
	}

	for _, a := range t.Agents {
		address := org.Address(a.Name, t.ID)
		team.Agents = append(team.Agents, officemcp.AgentState{
			Address: address, Role: a.Role, Expertise: a.Expertise, Status: string(d.statuses.Get(address)), OnLeave: a.OnLeave,
		})
	}

	for _, tk := range tickets {
		if tk.Team != t.ID {
			continue
		}

		summary := officemcp.TicketSummary{ID: tk.ID, Title: tk.Title, Assignee: tk.Assignee, BlockedBy: tk.BlockedBy}

		switch tk.Status {
		case work.StatusOpen:
			team.Open = append(team.Open, summary)
		case work.StatusInProgress:
			team.InProgress = append(team.InProgress, summary)
		case work.StatusDone:
			team.Done++
		case work.StatusCanceled:
			team.Canceled++
		}
	}

	return team
}
