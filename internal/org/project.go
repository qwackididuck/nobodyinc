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
	"github.com/qwackididuck/nobodyinc/internal/commands"
	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// ConfigDirName is the directory, at the root of a project, that holds its
// Nobody Inc configuration. It is committed with the project (ADR-0004).
const ConfigDirName = ".nobody"

// Files and directories inside ConfigDirName.
const (
	orgFile  = "org.yaml"
	teamsDir = "teams"
	agentExt = ".md"
	// projectPromptFile holds the project's shared instructions, given to
	// every agent (docs/agents.md, "The prompt cascade").
	projectPromptFile = "project.md"
	// teamPromptFile, in teams/<team>/, holds the team's shared instructions.
	// It is not an agent: readAgents skips it.
	teamPromptFile = "TEAM.md"
)

// orgFileHeader opens the org.yaml written by Nobody Inc.
const orgFileHeader = "# Nobody Inc organization: teams and the teams they block (ADR-0009).\n" +
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
// directory: org.yaml for teams and their block lists, one file per agent in
// teams/<team>/, the team prompts (teams/<team>/TEAM.md) and the project
// prompt (project.md); the MCP servers of Claude Code's three scopes
// (ADR-0010); and the project's and the user's skills (ADR-0011). It returns
// ErrNoConfig when org.yaml is missing.
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
		if err := readTeamFiles(dir, &o.Teams[i]); err != nil {
			return nil, err
		}
	}

	if o.ProjectPrompt, err = readPrompt(filepath.Join(ConfigDir(dir), projectPromptFile)); err != nil {
		return nil, err
	}

	if o.God, err = readGod(dir); err != nil {
		return nil, err
	}

	if err := checkTeamDirs(dir, o); err != nil {
		return nil, err
	}

	if err := readClaudeConfig(o); err != nil {
		return nil, err
	}

	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("invalid organization in %s: %w", ConfigDir(dir), err)
	}

	return o, nil
}

// WriteProject writes the whole organization into o.ProjectDir/.nobody/:
// org.yaml, the project and team prompts, and every agent file.
// Agent files that are no longer part of the organization are left alone.
func WriteProject(o *Org) error {
	if err := WriteOrgFile(o); err != nil {
		return err
	}

	if err := WriteProjectPrompt(o.ProjectDir, o.ProjectPrompt); err != nil {
		return err
	}

	for _, t := range o.Teams {
		if err := WriteTeamPrompt(o.ProjectDir, t.ID, t.Prompt); err != nil {
			return err
		}

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

// WriteOrgFile validates the organization and writes .nobody/org.yaml alone:
// teams (id, goal, block list) and the dismissed subagents. Agent files and
// prompts are untouched.
func WriteOrgFile(o *Org) error {
	if err := o.Validate(); err != nil {
		return err
	}

	body, err := catalog.MarshalYAML(o)
	if err != nil {
		return fmt.Errorf("encode %s: %w", orgFile, err)
	}

	return atomicfile.Write(filepath.Join(ConfigDir(o.ProjectDir), orgFile), append([]byte(orgFileHeader), body...))
}

// WriteProjectPrompt writes .nobody/project.md; an empty text removes it.
func WriteProjectPrompt(projectDir, text string) error {
	return writePrompt(filepath.Join(ConfigDir(projectDir), projectPromptFile), text)
}

// WriteTeamPrompt writes .nobody/teams/<team>/TEAM.md; an empty text removes
// it.
func WriteTeamPrompt(projectDir, team, text string) error {
	return writePrompt(filepath.Join(ConfigDir(projectDir), teamsDir, team, teamPromptFile), text)
}

// writePrompt writes a markdown prompt file, or removes it when the text is
// empty, so that an absent file and an empty prompt mean the same thing.
func writePrompt(path, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		return nil
	}

	return atomicfile.Write(path, []byte(text+"\n"))
}

// readPrompt reads a prompt file, trimmed; a missing file is an empty prompt.
func readPrompt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

// readTeamFiles reads what teams/<team>/ holds: the team prompt and the
// agents.
func readTeamFiles(dir string, t *Team) error {
	teamDir := filepath.Join(ConfigDir(dir), teamsDir, t.ID)

	var err error
	if t.Prompt, err = readPrompt(filepath.Join(teamDir, teamPromptFile)); err != nil {
		return err
	}

	t.Agents, err = readAgents(teamDir)

	return err
}

// WriteAgent writes one agent file: .nobody/teams/<team>/<name>.md.
func WriteAgent(projectDir, team string, a Agent) error {
	data, err := catalog.Format(a.Definition)
	if err != nil {
		return err
	}

	return atomicfile.Write(agentPath(projectDir, team, a.Name), data)
}

// RemoveTeamDir deletes .nobody/teams/<team>/: the team's agent files and
// its TEAM.md. Remove it before writing an org.yaml without the team: a team
// directory that org.yaml does not declare stops the project from loading.
func RemoveTeamDir(projectDir, team string) error {
	if !catalog.ValidName(team) {
		return fmt.Errorf("team id %q: use lowercase letters, digits and dashes", team)
	}

	if err := os.RemoveAll(filepath.Join(ConfigDir(projectDir), teamsDir, team)); err != nil {
		return fmt.Errorf("remove team %s: %w", team, err)
	}

	return nil
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
		return nil, fmt.Errorf("%w in %s (open the folder from the control room to set it up)", ErrNoConfig, dir)
	}

	if err != nil {
		return nil, err
	}

	// file is org.yaml with the "channels:" of before ADR-0009, still read so
	// that an old project loads; they are counted, never written back.
	var file struct {
		Org `yaml:",inline"`

		Channels []any `yaml:"channels"`
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	// An empty file decodes to io.EOF: an organization without teams yet.
	if err := dec.Decode(&file); err != nil && strings.TrimSpace(string(data)) != "" {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	o := &file.Org
	o.ProjectDir = dir
	o.IgnoredChannels = len(file.Channels)

	return o, nil
}

// readClaudeConfig reads what every agent gets from Claude Code's own
// configuration: the MCP servers (readMCP), the skills (readSkills) and the
// commands (readCommands).
func readClaudeConfig(o *Org) error {
	if err := readMCP(o); err != nil {
		return err
	}

	if err := readSkills(o); err != nil {
		return err
	}

	return readCommands(o)
}

// readMCP reads the MCP servers every agent gets: the project's .mcp.json
// and the local and user scopes of ~/.claude.json (internal/mcpservers).
func readMCP(o *Org) error {
	claudeConfig, err := mcpservers.ClaudeConfigPath()
	if err != nil {
		return err
	}

	found, err := mcpservers.Load(o.ProjectDir, claudeConfig)
	if err != nil {
		return fmt.Errorf("MCP servers: %w", err)
	}

	o.MCP, o.IgnoredMCP = found.Servers, found.Ignored

	return nil
}

// readSkills reads the skills of the project's .claude/skills and the user's
// personal skills (internal/skills).
func readSkills(o *Org) error {
	personal, err := skills.PersonalDir()
	if err != nil {
		return err
	}

	if o.Skills, err = skills.Load(o.ProjectDir, personal); err != nil {
		return fmt.Errorf("skills: %w", err)
	}

	return nil
}

// readCommands reads the commands of the project's .claude/commands and the
// user's personal commands (internal/commands).
func readCommands(o *Org) error {
	personal, err := commands.PersonalDir()
	if err != nil {
		return err
	}

	if o.Commands, err = commands.Load(o.ProjectDir, personal); err != nil {
		return fmt.Errorf("commands: %w", err)
	}

	return nil
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
		if e.IsDir() || !ok || e.Name() == teamPromptFile {
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
