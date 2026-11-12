// Package org describes who works in Nobody Inc and who may talk to whom.
//
// An organization is loaded from a YAML file (see nobody.example.yaml):
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
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// HumanAddress is the address of the human operator. The human can write to
// any agent, and any agent can write to the human (to answer or escalate).
const HumanAddress = "human"

// SystemAddress signs notices written by Nobody Inc itself (e.g. "a colleague
// was answered on your behalf"). It can write to anyone and receives nothing.
const SystemAddress = "nobody"

// Org is the whole organization.
type Org struct {
	// ProjectDir is the repository the agents work in.
	ProjectDir string    `yaml:"project_dir"`
	Teams      []Team    `yaml:"teams"`
	Channels   []Channel `yaml:"channels"`
}

// Team is a group of agents sharing a goal.
type Team struct {
	ID     string  `yaml:"id"`
	Goal   string  `yaml:"goal"`
	Agents []Agent `yaml:"agents"`
}

// Agent is one member of a team.
type Agent struct {
	Name string `yaml:"name"`
	// Role is a one-line description, e.g. "Software architect".
	Role string `yaml:"role"`
	// Prompt holds the agent's specific instructions.
	Prompt string `yaml:"prompt"`
	// Model is the Claude model to use (e.g. "sonnet"); empty means the
	// Claude Code default.
	Model string `yaml:"model,omitempty"`
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

var validName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Load reads and validates an organization file.
func Load(path string) (*Org, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read org file: %w", err)
	}

	var o Org
	if err := yaml.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("parse org file %s: %w", path, err)
	}

	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("invalid org file %s: %w", path, err)
	}

	return &o, nil
}

// Validate checks names, uniqueness and that channels point to real teams.
func (o *Org) Validate() error {
	teams, errs := o.validateTeams()
	errs = append(errs, o.validateChannels(teams)...)

	return errors.Join(errs...)
}

// Members lists every agent of the organization.
func (o *Org) Members() []Member {
	var members []Member

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

// validateTeams checks team and agent names, and returns the known team IDs.
func (o *Org) validateTeams() (map[string]bool, []error) {
	var errs []error

	teams := map[string]bool{}

	for _, t := range o.Teams {
		if !validName.MatchString(t.ID) {
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
		if !validName.MatchString(a.Name) {
			errs = append(errs, fmt.Errorf("agent name %q in team %q: use lowercase letters, digits and dashes", a.Name, t.ID))
		}

		if agents[a.Name] {
			errs = append(errs, fmt.Errorf("agent %q is declared twice in team %q", a.Name, t.ID))
		}

		agents[a.Name] = true
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

// Address builds the address of an agent: "<agent>@<team>".
func Address(agent, team string) string {
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
