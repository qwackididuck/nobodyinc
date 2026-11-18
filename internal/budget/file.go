package budget

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
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// FileName is the budgets' file inside the project's .nobody/.
const FileName = "budgets.yaml"

// fileHeader opens the file written by Nobody Inc.
const fileHeader = "# Nobody Inc budgets (ADR-0013, docs/budgets.md), written by the daemon.\n" +
	"# Amounts are Claude Code's estimate at list price, in USD: a guard, not an invoice.\n"

// Path is a project's budgets file.
func Path(projectDir string) string {
	return filepath.Join(org.ConfigDir(projectDir), FileName)
}

// file is the YAML form of budgets.yaml. Enabled and Threshold are pointers
// so that a file written by hand without them keeps the defaults.
type file struct {
	Enabled   *bool          `yaml:"enabled,omitempty"`
	Threshold *int           `yaml:"threshold,omitempty"`
	Staff     *Cap           `yaml:"staff,omitempty"`
	Teams     map[string]Cap `yaml:"teams,omitempty"`
	Agents    map[string]Cap `yaml:"agents,omitempty"`
}

// Load reads a project's budget settings and checks them on their own. A
// missing or empty file gives the defaults (Defaults). Whether the capped
// teams and agents exist is not checked here: a cap left by a dismissed agent
// is dropped at the next write (Book).
func Load(projectDir string) (Settings, error) {
	path := Path(projectDir)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}

	if err != nil {
		return Settings{}, err
	}

	var f file

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	// An empty file decodes to io.EOF: the defaults.
	if err := dec.Decode(&f); err != nil && strings.TrimSpace(string(data)) != "" {
		return Settings{}, fmt.Errorf("parse %s: %w", path, err)
	}

	s := Defaults()
	if f.Enabled != nil {
		s.Enabled = *f.Enabled
	}

	if f.Threshold != nil {
		s.Threshold = *f.Threshold
	}

	s.Staff, s.Teams, s.Agents = f.Staff, f.Teams, f.Agents

	if err := s.Check(); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}

	return s, nil
}

// Save checks and writes a project's budget settings, replacing the file
// atomically.
func Save(projectDir string, s Settings) error {
	if err := s.Check(); err != nil {
		return err
	}

	f := file{Enabled: &s.Enabled, Threshold: &s.Threshold, Staff: s.Staff, Teams: s.Teams, Agents: s.Agents}

	body, err := catalog.MarshalYAML(f)
	if err != nil {
		return fmt.Errorf("encode %s: %w", FileName, err)
	}

	return atomicfile.Write(Path(projectDir), append([]byte(fileHeader), body...))
}
