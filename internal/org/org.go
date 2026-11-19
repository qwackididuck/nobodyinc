// Package org describes who works in Nobody Inc and who may talk to whom.
//
// An organization belongs to a project and is stored in the project's
// .nobody/ directory (docs/agents.md, ADR-0004):
//
//   - teams group agents around a common goal;
//   - every agent has an address "<agent>@<team>", e.g. "archi@specs";
//   - any agent may start a conversation with any team (ADR-0009), unless
//     its team blocks that team: "block: [qa]" under the dev team stops dev's
//     members from starting conversations with qa. A block is one-way (qa may
//     still write to dev) and never stops an answer (see CanStart).
package org

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/commands"
	"github.com/qwackididuck/nobodyinc/internal/kb"
	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// OfficeServer is the name of the office MCP server every agent gets
// (internal/officemcp); a server of this name in Claude Code's scopes is
// ignored.
const OfficeServer = mcpservers.OfficeServer

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
	ProjectDir string `yaml:"-"`
	Teams      []Team `yaml:"teams"`
	// IgnoredSubagents are the names of the project's Claude Code subagents
	// the human chose not to import as agents (ADR-0018, internal/subagents):
	// they are no longer offered.
	IgnoredSubagents []string `yaml:"ignored_subagents,omitempty"`
	// Knowledge is the project's knowledge base, a folder of the project
	// (ADR-0019, internal/kb); nil when it has none.
	Knowledge *Knowledge `yaml:"knowledge,omitempty"`
	// IgnoredChannels counts the "channels:" entries read from an org.yaml
	// written before ADR-0009. They are ignored (flows are open by default)
	// and dropped by the next write of org.yaml; the daemon warns about them.
	IgnoredChannels int `yaml:"-"`
	// MCP are the MCP servers every agent gets, read from Claude Code's
	// three scopes: the project's .mcp.json, then the user's on this project
	// and the user's (ADR-0010, internal/mcpservers).
	MCP map[string]mcpservers.Server `yaml:"-"`
	// IgnoredMCP are servers left out (a name conflict lost, or the office
	// server's name), as "<name> (<scope>): <why>"; the daemon warns about
	// them.
	IgnoredMCP []string `yaml:"-"`
	// Skills are the project's skills and the user's personal skills
	// (ADR-0011, internal/skills); Found.Ignored lists those that could not
	// be read, for a warning. Claude Code's built-in skills are not part of
	// the organization: the daemon discovers them (skills.Builtin).
	Skills skills.Found `yaml:"-"`
	// Commands are the project's commands and the user's personal commands
	// (ADR-0017, internal/commands); Found.Ignored lists those that could not
	// be read, for a warning.
	Commands commands.Found `yaml:"-"`
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
	// Block lists the teams this team's members may not start a conversation
	// with (ADR-0009). Answers and tickets are not affected.
	Block []string `yaml:"block,omitempty"`
	// Prompt is what every member of the team should know, read from
	// .nobody/teams/<id>/TEAM.md.
	Prompt string `yaml:"-"`
	// Agents are read from .nobody/teams/<id>/*.md.
	Agents []Agent `yaml:"-"`
}

// Knowledge declares the project's knowledge base (ADR-0019): "knowledge:
// {path: docs}" in org.yaml.
type Knowledge struct {
	// Path is the folder, relative to the project (kb.CheckPath).
	Path string `yaml:"path"`
}

// KnowledgePath is the folder of the knowledge base, "" when the project
// has none.
func (o *Org) KnowledgePath() string {
	if o.Knowledge == nil {
		return ""
	}

	return o.Knowledge.Path
}

// WithKnowledge returns a copy of the organization with its knowledge base
// at path ("" removes it). The receiver is left untouched; validate the
// copy before using it.
func (o *Org) WithKnowledge(path string) *Org {
	c := o.copyTeams()
	c.Knowledge = nil

	if path != "" {
		c.Knowledge = &Knowledge{Path: path}
	}

	return c
}

// Agent is one member of a team: its name and its definition (role, prompt,
// model, permissions...), as written in .nobody/teams/<team>/<name>.md.
type Agent struct {
	catalog.Definition

	Name string
}

// Member is an agent together with its team, as seen from outside.
type Member struct {
	Address string
	Team    Team
	Agent   Agent
}

// Validate checks names, uniqueness and that block lists name real teams.
func (o *Org) Validate() error {
	teams, errs := o.validateTeams()
	errs = append(errs, o.validateGod()...)
	errs = append(errs, o.validateBlocks(teams)...)
	errs = append(errs, o.validateKnowledge()...)

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

// CanStart reports whether a member of team from may start a conversation
// with team to: always, unless team from blocks team to. A team never blocks
// itself, and "" (the human, nobody, the director) is never blocked; answers
// are not this method's business (messaging.Policy lets them through).
func (o *Org) CanStart(from, to string) bool {
	if from == "" || to == "" || from == to {
		return true
	}

	t, ok := o.Team(from)

	return !ok || !slices.Contains(t.Block, to)
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
// team with this id exists, with its goal, block list and prompt replaced:
// its agents stay. The receiver is left untouched; validate the copy before
// using it.
func (o *Org) WithTeam(t Team) *Org {
	c := o.copyTeams()

	if i := slices.IndexFunc(c.Teams, func(x Team) bool { return x.ID == t.ID }); i >= 0 {
		c.Teams[i].Goal = t.Goal
		c.Teams[i].Block = slices.Clone(t.Block)
		c.Teams[i].Prompt = t.Prompt

		return c
	}

	t.Agents = nil
	t.Block = slices.Clone(t.Block)
	c.Teams = append(c.Teams, t)

	return c
}

// WithIgnoredSubagents returns a copy of the organization with names added
// to its dismissed subagents, each once, in order. The receiver is left
// untouched.
func (o *Org) WithIgnoredSubagents(names ...string) *Org {
	c := o.copyTeams()
	c.IgnoredSubagents = slices.Clone(o.IgnoredSubagents)

	for _, name := range names {
		if !slices.Contains(c.IgnoredSubagents, name) {
			c.IgnoredSubagents = append(c.IgnoredSubagents, name)
		}
	}

	return c
}

// WithoutTeam returns a copy of the organization without team id: its
// members go with it, and it leaves the other teams' block lists. The
// receiver is left untouched.
func (o *Org) WithoutTeam(id string) *Org {
	c := o.copyTeams()
	c.Teams = slices.DeleteFunc(c.Teams, func(t Team) bool { return t.ID == id })

	for i := range c.Teams {
		c.Teams[i].Block = slices.DeleteFunc(c.Teams[i].Block, func(b string) bool { return b == id })
	}

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

// validateBlocks checks that every team blocks known teams other than
// itself, each once.
func (o *Org) validateBlocks(teams map[string]bool) []error {
	var errs []error

	for _, t := range o.Teams {
		seen := map[string]bool{}

		for _, b := range t.Block {
			switch {
			case b == t.ID:
				errs = append(errs, fmt.Errorf("team %q cannot block itself: a team always talks to itself", t.ID))
			case !teams[b]:
				errs = append(errs, fmt.Errorf("team %q blocks %q: %w", t.ID, b, ErrUnknownTeam))
			case seen[b]:
				errs = append(errs, fmt.Errorf("team %q blocks %q twice", t.ID, b))
			}

			seen[b] = true
		}
	}

	return errs
}

// validateKnowledge checks the path of the knowledge base, as written
// (symbolic links are followed by kb.Resolve, when it is read).
func (o *Org) validateKnowledge() []error {
	if o.Knowledge == nil {
		return nil
	}

	if _, err := kb.CheckPath(o.Knowledge.Path); err != nil {
		return []error{fmt.Errorf("knowledge: %w", err)}
	}

	return nil
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
		t.Block = slices.Clone(t.Block)
		c.Teams[i] = t
	}

	return &c
}
