// Package settings reads the user's global Nobody Inc settings, in
// ~/.nobody/settings.yaml. They apply to every project the user runs; the
// project's own configuration lives in its .nobody/ (ADR-0004).
//
// A missing file means the defaults. Settings are read when the daemon
// starts: restart it after changing the file by hand. The control room can
// also change them while the daemon runs (Live), which rewrites the file.
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
)

// FileName is the settings file, inside the user's ~/.nobody.
const FileName = "settings.yaml"

// What happens when a ticket delivery changes the project's .nobody/
// (ConfigProtection).
const (
	// ProtectBlock refuses the delivery: the agent must undo its change.
	ProtectBlock = "block"
	// ProtectWarn delivers, and warns the human, who reviews the change
	// before merging the feature.
	ProtectWarn = "warn"
)

// Settings are the user's global settings.
type Settings struct {
	// ConfigProtection is ProtectBlock (default) or ProtectWarn. Agents are
	// never allowed to change their own configuration (ADR-0004); this says
	// how strictly a delivery that does so anyway is treated.
	ConfigProtection string `yaml:"config_protection"`
	// CheckUpdates asks GitHub for a newer release when the launcher starts
	// (default true, docs/updates.md). The control room's "Check for
	// updates" works either way.
	CheckUpdates bool `yaml:"check_updates"`
}

// Defaults are the settings without a settings file.
func Defaults() Settings {
	return Settings{ConfigProtection: ProtectBlock, CheckUpdates: true}
}

// fileTemplate is the settings file: written with the defaults when the
// first project is set up (projects.SetUp), so that the settings are easy to find and to change, and
// rewritten when the control room changes a setting.
const fileTemplate = `# Nobody Inc settings for this user, used by every project.
# Restart the daemon after changing this file by hand.

# What happens when an agent delivers (closes a ticket with) changes to the
# project's .nobody/ directory, i.e. to the agents' own configuration:
#   block: the delivery is refused until the agent undoes the change;
#   warn:  the delivery goes through and the human is warned.
config_protection: %s

# Look for a newer Nobody Inc release when it starts (an anonymous request to
# GitHub; nothing is downloaded, a banner links to the release page):
#   true:  check at every start;
#   false: only when you ask ("Check for updates").
check_updates: %t
`

// render returns the content of the settings file for s.
func render(s Settings) []byte {
	return fmt.Appendf(nil, fileTemplate, s.ConfigProtection, s.CheckUpdates)
}

// Load reads home/settings.yaml. Unset values keep their default; unknown
// keys and invalid values are refused, so that a typo is noticed.
func Load(home string) (Settings, error) {
	path := filepath.Join(home, FileName)
	s := Defaults()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return Settings{}, err
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	// An empty (or comments-only) file decodes to io.EOF: the defaults.
	if err := dec.Decode(&s); err != nil && strings.TrimSpace(stripComments(string(data))) != "" {
		return Settings{}, fmt.Errorf("parse %s: %w", path, err)
	}

	if s.ConfigProtection != ProtectBlock && s.ConfigProtection != ProtectWarn {
		return Settings{}, fmt.Errorf("%s: config_protection %q: use %s or %s", path, s.ConfigProtection, ProtectBlock, ProtectWarn)
	}

	return s, nil
}

// WriteDefaultIfMissing writes a commented settings file with the defaults,
// unless one exists. It reports whether it wrote one.
func WriteDefaultIfMissing(home string) (bool, error) {
	path := filepath.Join(home, FileName)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}

	return true, atomicfile.Write(path, render(Defaults()))
}

// Live holds the settings while the daemon runs. It is safe for concurrent
// use.
type Live struct {
	mu   sync.Mutex
	home string // where settings.yaml is saved; "" keeps changes in memory
	s    Settings
}

// NewLive returns the running settings s, saved to home/settings.yaml when
// they change ("" for no file, e.g. in tests).
func NewLive(home string, s Settings) *Live {
	return &Live{home: home, s: s}
}

// Current returns the settings in force.
func (l *Live) Current() Settings {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.s
}

// SetConfigProtection changes ConfigProtection (ProtectBlock or ProtectWarn)
// and saves the settings file. It applies at once.
func (l *Live) SetConfigProtection(value string) (Settings, error) {
	if value != ProtectBlock && value != ProtectWarn {
		return Settings{}, fmt.Errorf("config_protection %q: use %s or %s", value, ProtectBlock, ProtectWarn)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	next := l.s
	next.ConfigProtection = value

	if l.home != "" {
		if err := atomicfile.Write(filepath.Join(l.home, FileName), render(next)); err != nil {
			return Settings{}, fmt.Errorf("save the settings: %w", err)
		}
	}

	l.s = next

	return next, nil
}

func stripComments(s string) string {
	var b strings.Builder

	for line := range strings.SplitSeq(s, "\n") {
		if code, _, _ := strings.Cut(line, "#"); strings.TrimSpace(code) != "" {
			b.WriteString(code)
		}
	}

	return b.String()
}
