package schedule

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

// FileName is the schedules' file inside the project's .nobody/.
const FileName = "schedules.yaml"

// fileHeader opens the file written by Nobody Inc.
const fileHeader = "# Nobody Inc scheduled tasks (ADR-0012, docs/schedules.md), written by the daemon.\n" +
	"# Each one runs only while the daemon runs; missed occurrences are not caught up.\n"

// Path is a project's schedules file.
func Path(projectDir string) string {
	return filepath.Join(org.ConfigDir(projectDir), FileName)
}

// file is the YAML form of schedules.yaml. Enabled is a pointer so that a
// schedule written by hand without "enabled" is enabled.
type file struct {
	Schedules []entry `yaml:"schedules"`
}

type entry struct {
	ID           string `yaml:"id"`
	Title        string `yaml:"title"`
	Every        string `yaml:"every,omitempty"`
	Cron         string `yaml:"cron,omitempty"`
	Target       string `yaml:"target"`
	Mode         Mode   `yaml:"mode"`
	Enabled      *bool  `yaml:"enabled,omitempty"`
	Instructions string `yaml:"instructions"`
}

// Load reads a project's schedules. A missing file means no schedules. Every
// schedule is checked on its own (Check) and ids must be unique; whether the
// targets exist is checked when a schedule falls due.
func Load(projectDir string) ([]Schedule, error) {
	path := Path(projectDir)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var f file

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	// An empty file decodes to io.EOF: no schedules.
	if err := dec.Decode(&f); err != nil && strings.TrimSpace(string(data)) != "" {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	schedules := make([]Schedule, 0, len(f.Schedules))
	for _, e := range f.Schedules {
		schedules = append(schedules, Schedule{
			ID: e.ID, Title: e.Title, Every: e.Every, Cron: e.Cron, Target: e.Target, Mode: e.Mode,
			Enabled: e.Enabled == nil || *e.Enabled, Instructions: e.Instructions,
		})
	}

	if err := CheckAll(schedules); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return schedules, nil
}

// Save checks and writes a project's schedules, replacing the file
// atomically.
func Save(projectDir string, schedules []Schedule) error {
	if err := CheckAll(schedules); err != nil {
		return err
	}

	f := file{Schedules: make([]entry, 0, len(schedules))}

	for _, s := range schedules {
		enabled := s.Enabled
		f.Schedules = append(f.Schedules, entry{
			ID: s.ID, Title: s.Title, Every: s.Every, Cron: s.Cron, Target: s.Target, Mode: s.Mode,
			Enabled: &enabled, Instructions: s.Instructions,
		})
	}

	body, err := catalog.MarshalYAML(f)
	if err != nil {
		return fmt.Errorf("encode %s: %w", FileName, err)
	}

	return atomicfile.Write(Path(projectDir), append([]byte(fileHeader), body...))
}

// CheckAll checks every schedule and that no id is used twice.
func CheckAll(schedules []Schedule) error {
	var errs []error

	seen := map[string]bool{}

	for _, s := range schedules {
		if err := s.Check(); err != nil {
			errs = append(errs, err)
		}

		if seen[s.ID] {
			errs = append(errs, fmt.Errorf("%w: %q", ErrExists, s.ID))
		}

		seen[s.ID] = true
	}

	return errors.Join(errs...)
}
