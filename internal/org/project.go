package org

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

// ConfigDirName is the directory, at the root of a project, that holds its
// Nobody Inc configuration. It is committed with the project (ADR-0004).
const ConfigDirName = ".nobody"

// Files and directories inside ConfigDirName.
const (
	orgFile  = "org.yaml"
	teamsDir = "teams"
	agentExt = ".md"
)

// orgFileHeader opens the org.yaml written by Nobody Inc.
const orgFileHeader = "# Nobody Inc organization: teams and channels.\n" +
	"# Each agent is a file in teams/<team>/<agent>.md.\n"

// ErrNoConfig means the project has no .nobody/org.yaml yet.
var ErrNoConfig = errors.New("no Nobody Inc configuration")

// ConfigDir is the .nobody/ directory of a project.
func ConfigDir(projectDir string) string {
	return filepath.Join(projectDir, ConfigDirName)
}

// HasConfig reports whether a project already has a .nobody/org.yaml.
func HasConfig(projectDir string) bool {
	_, err := os.Stat(filepath.Join(ConfigDir(projectDir), orgFile))

	return err == nil
}

// LoadProject reads the organization of a project from its .nobody/
// directory: org.yaml for teams and channels, one file per agent in
// teams/<team>/, and the MCP registry mcp.yaml. It returns ErrNoConfig when
// org.yaml is missing.
func LoadProject(projectDir string) (*Org, error) {
	dir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	o, err := readOrgFile(dir)
	if err != nil {
		return nil, err
	}

	for i := range o.Teams {
		o.Teams[i].Agents, err = readAgents(filepath.Join(ConfigDir(dir), teamsDir, o.Teams[i].ID))
		if err != nil {
			return nil, err
		}
	}

	if o.God, err = readGod(dir); err != nil {
		return nil, err
	}

	if err := checkTeamDirs(dir, o); err != nil {
		return nil, err
	}

	if o.MCP, err = readMCPFile(dir); err != nil {
		return nil, err
	}

	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("invalid organization in %s: %w", ConfigDir(dir), err)
	}

	return o, nil
}

// WriteProject writes the whole organization into o.ProjectDir/.nobody/:
// org.yaml and every agent file.
// Agent files that are no longer part of the organization are left alone.
func WriteProject(o *Org) error {
	if err := o.Validate(); err != nil {
		return err
	}

	header, err := catalog.MarshalYAML(o)
	if err != nil {
		return fmt.Errorf("encode %s: %w", orgFile, err)
	}

	dir := ConfigDir(o.ProjectDir)

	if err := atomicfile.Write(filepath.Join(dir, orgFile), append([]byte(orgFileHeader), header...)); err != nil {
		return err
	}

	for _, t := range o.Teams {
		for _, a := range t.Agents {
			if err := WriteAgent(o.ProjectDir, t.ID, a); err != nil {
				return err
			}
		}
	}

	if o.God != nil {
		return WriteAgent(o.ProjectDir, DirectorTeam, *o.God)
	}

	return nil
}

// WriteAgent writes one agent file: .nobody/teams/<team>/<name>.md.
func WriteAgent(projectDir, team string, a Agent) error {
	data, err := catalog.Format(a.Definition)
	if err != nil {
		return err
	}

	return atomicfile.Write(agentPath(projectDir, team, a.Name), data)
}

// AgentFileExists reports whether an agent file is on disk, whether or not
// the organization in memory knows it yet.
func AgentFileExists(projectDir, team, name string) bool {
	_, err := os.Stat(agentPath(projectDir, team, name))

	return err == nil
}

// RemoveAgent deletes an agent file, e.g. when the agent is dismissed. Its
// history (messages, tickets, commits) is kept elsewhere.
func RemoveAgent(projectDir, team, name string) error {
	if err := os.Remove(agentPath(projectDir, team, name)); err != nil {
		return fmt.Errorf("remove agent %s: %w", Address(name, team), err)
	}

	return nil
}

// agentPath is the file of an agent: teams/<team>/<name>.md, or
// director/<name>.md for the director (DirectorTeam).
func agentPath(projectDir, team, name string) string {
	if team == DirectorTeam {
		return filepath.Join(ConfigDir(projectDir), directorDir, name+agentExt)
	}

	return filepath.Join(ConfigDir(projectDir), teamsDir, team, name+agentExt)
}

// readGod reads the director from director/: no file means no director, and
// there can be only one.
func readGod(dir string) (*Agent, error) {
	agents, err := readAgents(filepath.Join(ConfigDir(dir), directorDir))
	if err != nil || len(agents) == 0 {
		return nil, err
	}

	if len(agents) > 1 {
		return nil, fmt.Errorf("%s/: %w (%d files: keep only one)", directorDir, ErrDirectorExists, len(agents))
	}

	return &agents[0], nil
}

func readOrgFile(dir string) (*Org, error) {
	path := filepath.Join(ConfigDir(dir), orgFile)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w in %s (create one with `nobodyd init`)", ErrNoConfig, dir)
	}

	if err != nil {
		return nil, err
	}

	o := &Org{ProjectDir: dir}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	// An empty file decodes to io.EOF: an organization without teams yet.
	if err := dec.Decode(o); err != nil && strings.TrimSpace(string(data)) != "" {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return o, nil
}

// readAgents reads the agent files (*.md) of a directory; a missing
// directory has no agents yet.
func readAgents(teamDir string) ([]Agent, error) {
	entries, err := os.ReadDir(teamDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var agents []Agent

	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), agentExt)
		if e.IsDir() || !ok {
			continue
		}

		path := filepath.Join(teamDir, e.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		d, err := catalog.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		agents = append(agents, Agent{Name: name, Definition: d})
	}

	return agents, nil
}

// checkTeamDirs refuses agent directories of teams org.yaml does not declare:
// their agents would otherwise silently never start.
func checkTeamDirs(dir string, o *Org) error {
	entries, err := os.ReadDir(filepath.Join(ConfigDir(dir), teamsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	declared := map[string]bool{}
	for _, t := range o.Teams {
		declared[t.ID] = true
	}

	var errs []error

	for _, e := range entries {
		if e.IsDir() && !declared[e.Name()] {
			errs = append(errs, fmt.Errorf("%s/%s: team %q is not declared in %s", teamsDir, e.Name(), e.Name(), orgFile))
		}
	}

	return errors.Join(errs...)
}
