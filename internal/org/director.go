package org

import (
	"errors"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

// The director, "god", talks with the human, turns requests into features and
// tickets, and leads the teams (docs/god.md, ADR-0005). There is at most one
// per project. It is an agent like the others (a definition file, a Claude
// Code session), with three differences:
//
//   - it has no team: its file is .nobody/director/<name>.md and its address
//     is its name alone, like "human";
//   - like the human, it may write to anyone and anyone may write to it;
//   - it gets office tools the teams do not have (internal/officemcp).

// DirectorTeam is the team value that designates the director in WithAgent,
// WriteAgent, RemoveAgent, AgentFileExists and Address: it has no team.
const DirectorTeam = ""

// directorDir holds the director's file, inside ConfigDirName.
const directorDir = "director"

// ErrDirectorExists is returned when a second director would be added.
var ErrDirectorExists = errors.New("the project already has a director")

// IsGod reports whether address is the director's.
func (o *Org) IsGod(address string) bool {
	return o.God != nil && o.God.Name == address
}

// setGod makes a the director, unless another one exists.
func (o *Org) setGod(a Agent) error {
	if o.God != nil && o.God.Name != a.Name {
		return fmt.Errorf("%w: %s", ErrDirectorExists, o.God.Name)
	}

	o.God = &a

	return nil
}

// validateGod checks the director's name, which is also its address.
func (o *Org) validateGod() []error {
	if o.God == nil {
		return nil
	}

	var errs []error

	name := o.God.Name
	if !catalog.ValidName(name) {
		errs = append(errs, fmt.Errorf("director name %q: use lowercase letters, digits and dashes", name))
	}

	if name == HumanAddress || name == SystemAddress {
		errs = append(errs, fmt.Errorf("director name %q is reserved", name))
	}

	if err := o.God.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("director %s: %w", name, err))
	}

	return errs
}

// EnsureDirector gives the project its director when it has none: it writes
// .nobody/director/<name>.md from definition d. The director is always
// present (it is not in the catalog): the project daemon calls this when it
// starts (a new project gets its director from projects.SetUp). It reports whether it created the file; an existing
// director, whatever its name, is left alone.
func EnsureDirector(projectDir, name string, d catalog.Definition) (bool, error) {
	god, err := readGod(projectDir)
	if err != nil {
		return false, err
	}

	if god != nil {
		return false, nil
	}

	if err := WriteAgent(projectDir, DirectorTeam, Agent{Name: name, Definition: d}); err != nil {
		return false, fmt.Errorf("write the director: %w", err)
	}

	return true, nil
}
