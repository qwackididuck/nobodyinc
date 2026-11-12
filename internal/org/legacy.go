package org

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// legacyOrg is the former single-file organization, nobody.yaml, replaced by
// the project's .nobody/ directory (ADR-0004). It is only read to migrate.
type legacyOrg struct {
	ProjectDir string       `yaml:"project_dir"`
	Teams      []legacyTeam `yaml:"teams"`
	Channels   []Channel    `yaml:"channels"`
}

type legacyTeam struct {
	ID     string        `yaml:"id"`
	Goal   string        `yaml:"goal"`
	Agents []legacyAgent `yaml:"agents"`
}

type legacyAgent struct {
	Name   string `yaml:"name"`
	Role   string `yaml:"role"`
	Prompt string `yaml:"prompt"`
	Model  string `yaml:"model"`
}

// MigrateLegacy turns a nobody.yaml into the .nobody/ directory of the
// project it points to, and returns that project directory.
//
// If the project already has a .nobody/org.yaml, nothing is written and
// migrated is false: the existing configuration wins.
func MigrateLegacy(path string) (string, bool, error) {
	o, err := loadLegacy(path)
	if err != nil {
		return "", false, err
	}

	if HasConfig(o.ProjectDir) {
		return o.ProjectDir, false, nil
	}

	if err := WriteProject(o); err != nil {
		return "", false, fmt.Errorf("migrate %s: %w", path, err)
	}

	return o.ProjectDir, true, nil
}

// loadLegacy reads a nobody.yaml into the current model.
func loadLegacy(path string) (*Org, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var legacy legacyOrg
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if legacy.ProjectDir == "" {
		return nil, fmt.Errorf("%s: project_dir is missing", path)
	}

	projectDir, err := filepath.Abs(legacy.ProjectDir)
	if err != nil {
		return nil, err
	}

	o := &Org{ProjectDir: projectDir, Channels: legacy.Channels}

	for _, lt := range legacy.Teams {
		t := Team{ID: lt.ID, Goal: lt.Goal}
		for _, la := range lt.Agents {
			t.Agents = append(t.Agents, Agent{
				Name: la.Name,
				Role: la.Role, Model: la.Model, Prompt: la.Prompt,
			})
		}

		o.Teams = append(o.Teams, t)
	}

	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}

	return o, nil
}
