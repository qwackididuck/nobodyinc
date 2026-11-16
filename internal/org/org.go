// Package org describes who works in Nobody Inc and who may talk to whom.
//
// An organization belongs to a project and is stored in the project's
// .nobody/ directory (docs/agents.md, ADR-0004):
//
//   - teams group agents around a common goal;
//   - every agent has an address "<agent>@<team>", e.g. "archi@specs";
//   - agents of the same team can always talk to each other;
//   - a channel "from: dev, to: specs" lets the dev team ask the specs team
//     questions. Channels are one-way: specs can answer dev, but cannot start
//     a conversation with dev unless a "from: specs, to: dev" channel exists.
package org

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

// HumanAddress is the address of the human operator. The human can write to
// any agent, and any agent can write to the human (to answer or escalate).
const HumanAddress = "human"

// SystemAddress signs notices written by Nobody Inc itself (e.g. "a colleague
// was answered on your behalf"). It can write to anyone and receives nothing.
const SystemAddress = "nobody"

// Org is the whole organization.
//
// The YAML tags describe .nobody/org.yaml; agents are stored in their own
// files (see project.go).
type Org struct {
	// ProjectDir is the repository the agents work in: the directory holding
	// .nobody/.
	ProjectDir string    `yaml:"-"`
	Teams      []Team    `yaml:"teams"`
	Channels   []Channel `yaml:"channels,omitempty"`
	// MCP is the project's MCP registry, read from .nobody/mcp.yaml: the
	// servers agents may be given (see mcp.go).
	MCP map[string]MCPServer `yaml:"-"`
	// God is the director, the agent who talks with the human and leads the
	// teams; nil when the project has none (see director.go).
	God *Agent `yaml:"-"`
	// ProjectPrompt is what every agent of the project should know, read from
	// .nobody/project.md (docs/agents.md, "The prompt cascade").
	ProjectPrompt string `yaml:"-"`
}

// Team is a group of agents sharing a goal.
type Team struct {
	ID   string `yaml:"id"`
	Goal string `yaml:"goal,omitempty"`
	// Prompt is what every member of the team should know, read from
	// .nobody/teams/<id>/TEAM.md.
	Prompt string `yaml:"-"`
	// Agents are read from .nobody/teams/<id>/*.md.
	Agents []Agent `yaml:"-"`
}

// Agent is one member of a team: its name and its definition (role, prompt,
// model, permissions...), as written in .nobody/teams/<team>/<name>.md.
type Agent struct {
	catalog.Definition

	Name string
}

// Channel allows team From to start conversations with team To.
type Channel struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// Member is an agent together with its team, as seen from outside.
type Member struct {
	Address string
	Team    Team
	Agent   Agent
}

// Validate checks names, uniqueness and that channels point to real teams.
func (o *Org) Validate() error {
	teams, errs := o.validateTeams()
	errs = append(errs, o.validateGod()...)
	errs = append(errs, o.validateChannels(teams)...)
	errs = append(errs, o.validateMCP()...)

	return errors.Join(errs...)
}

// Members lists every agent of the organization.
func (o *Org) Members() []Member {
	var members []Member

	if o.God != nil {
		members = append(members, Member{Address: o.God.Name, Agent: *o.God})
	}

	for _, t := range o.Teams {
		for _, a := range t.Agents {
			members = append(members, Member{Address: Address(a.Name, t.ID), Team: t, Agent: a})
		}
	}

	return members
}

// Member finds an agent by address.
func (o *Org) Member(address string) (Member, bool) {
	for _, m := range o.Members() {
		if m.Address == address {
			return m, true
		}
	}

	return Member{}, false
}

// HasChannel reports whether team from may start conversations with team to.
func (o *Org) HasChannel(from, to string) bool {
	for _, c := range o.Channels {
		if c.From == from && c.To == to {
			return true
		}
	}

	return false
}

// ErrUnknownTeam is returned when a team is not declared in org.yaml.
var ErrUnknownTeam = errors.New("unknown team")

// WithAgent returns a copy of the organization where the team has agent a,
// added or replacing the agent with the same name. The receiver, a snapshot
// others may be reading, is left untouched (see Source).
func (o *Org) WithAgent(team string, a Agent) (*Org, error) {
	c := o.copyTeams()

	if team == DirectorTeam {
		if err := c.setGod(a); err != nil {
			return nil, err
		}

		return c, nil
	}

	for i := range c.Teams {
		if c.Teams[i].ID != team {
			continue
		}

		if j := slices.IndexFunc(c.Teams[i].Agents, func(x Agent) bool { return x.Name == a.Name }); j >= 0 {
			c.Teams[i].Agents[j] = a
		} else {
			c.Teams[i].Agents = append(c.Teams[i].Agents, a)
		}

		return c, nil
	}

	return nil, fmt.Errorf("%w: %q (declare it in .nobody/%s)", ErrUnknownTeam, team, orgFile)
}

// Team finds a team by id.
func (o *Org) Team(id string) (Team, bool) {
	i := slices.IndexFunc(o.Teams, func(t Team) bool { return t.ID == id })
	if i < 0 {
		return Team{}, false
	}

	return o.Teams[i], true
}

// WithTeam returns a copy of the organization with team t added, or, when a
// team with this id exists, with its goal and prompt replaced: its agents
// stay. The receiver is left untouched; validate the copy before using it.
func (o *Org) WithTeam(t Team) *Org {
	c := o.copyTeams()

	if i := slices.IndexFunc(c.Teams, func(x Team) bool { return x.ID == t.ID }); i >= 0 {
		c.Teams[i].Goal = t.Goal
		c.Teams[i].Prompt = t.Prompt

		return c
	}

	t.Agents = nil
	c.Teams = append(c.Teams, t)

	return c
}

// WithoutAgent returns a copy of the organization without the agent at
// address. The receiver is left untouched.
func (o *Org) WithoutAgent(address string) *Org {
	c := o.copyTeams()

	if c.IsGod(address) {
		c.God = nil
	}

	for i := range c.Teams {
		c.Teams[i].Agents = slices.DeleteFunc(c.Teams[i].Agents, func(a Agent) bool {
			return Address(a.Name, c.Teams[i].ID) == address
		})
	}

	return c
}

// validateTeams checks team and agent names, and returns the known team IDs.
func (o *Org) validateTeams() (map[string]bool, []error) {
	var errs []error

	teams := map[string]bool{}

	for _, t := range o.Teams {
		if !catalog.ValidName(t.ID) {
			errs = append(errs, fmt.Errorf("team id %q: use lowercase letters, digits and dashes", t.ID))
		}

		if teams[t.ID] {
			errs = append(errs, fmt.Errorf("team %q is declared twice", t.ID))
		}

		teams[t.ID] = true

		errs = append(errs, validateAgents(t)...)
	}

	return teams, errs
}

func validateAgents(t Team) []error {
	var errs []error

	agents := map[string]bool{}

	for _, a := range t.Agents {
		if !catalog.ValidName(a.Name) {
			errs = append(errs, fmt.Errorf("agent name %q in team %q: use lowercase letters, digits and dashes", a.Name, t.ID))
		}

		if agents[a.Name] {
			errs = append(errs, fmt.Errorf("agent %q is declared twice in team %q", a.Name, t.ID))
		}

		agents[a.Name] = true

		if err := a.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("agent %s: %w", Address(a.Name, t.ID), err))
		}
	}

	return errs
}

// validateChannels checks that channels join two different, known teams.
func (o *Org) validateChannels(teams map[string]bool) []error {
	var errs []error

	for _, c := range o.Channels {
		if !teams[c.From] || !teams[c.To] {
			errs = append(errs, fmt.Errorf("channel %s -> %s refers to an unknown team", c.From, c.To))
		}

		if c.From == c.To {
			errs = append(errs, fmt.Errorf("channel %s -> %s is useless: a team can always talk to itself", c.From, c.To))
		}
	}

	return errs
}

// Address builds the address of an agent: "<agent>@<team>", or just the
// name for the director, who has no team (DirectorTeam).
func Address(agent, team string) string {
	if team == DirectorTeam {
		return agent
	}

	return agent + "@" + team
}

// TeamOf returns the team part of an address ("" for the human).
func TeamOf(address string) string {
	_, team, found := strings.Cut(address, "@")
	if !found {
		return ""
	}

	return team
}

// copyTeams returns a copy whose team and agent slices can be changed
// without touching the receiver's.
func (o *Org) copyTeams() *Org {
	c := *o
	c.Teams = make([]Team, len(o.Teams))

	if o.God != nil {
		god := *o.God
		c.God = &god
	}

	for i, t := range o.Teams {
		t.Agents = slices.Clone(t.Agents)
		c.Teams[i] = t
	}

	return &c
}
