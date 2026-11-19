//go:build contract

package contract

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// How commands reach an agent, with the flags agents are started with.
// Checked on 2026-11-19 with Claude Code 2.1.328 (roadmap, "les commandes
// dans Settings"):
//   - the project's commands (.claude/commands) are loaded, a command in a
//     sub-directory being named "<dir>:<name>";
//   - the user's personal commands (<config dir>/commands) are not loaded
//     without the "user" settings source, but a directory given with
//     --add-dir brings its own .claude/commands, through symbolic links to
//     the command files, sub-directories included: how Nobody Inc stages
//     them (internal/commands, ADR-0017).

// The user's personal commands do not reach an agent; a staging directory
// given with --add-dir, holding symbolic links to them, brings them. No
// model is called.
func TestPersonalCommandsReachAgentsThroughAnAddedDirectory(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	writeProjectFile(t, project, ".claude/commands/front/project-probe.md", "---\ndescription: A project probe.\n---\nSay PROJECT.\n")

	configDir := t.TempDir()
	personal := []string{"personal-probe.md", "ns/nested-probe.md"}

	for _, rel := range personal {
		writeProjectFile(t, configDir, filepath.Join("commands", rel), "---\ndescription: A personal probe.\n---\nSay PERSONAL.\n")
	}

	staging := t.TempDir()
	for _, rel := range personal {
		link := filepath.Join(staging, ".claude", "commands", rel)
		if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(filepath.Join(configDir, "commands", rel), link); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name         string
		addDir       bool
		wantPersonal bool
	}{
		{name: "agent flags alone: personal commands not loaded"},
		{name: "with --add-dir on a staging directory: loaded", addDir: true, wantPersonal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var extra []string
			if tt.addDir {
				extra = []string{"--add-dir", staging}
			}

			commands := sessionInit(t, project, []string{"CLAUDE_CONFIG_DIR=" + configDir}, extra...).SlashCommands

			if !slices.Contains(commands, "front:project-probe") {
				t.Errorf("the project's command front:project-probe is not loaded: %v", commands)
			}

			for _, name := range []string{"personal-probe", "ns:nested-probe"} {
				if got := slices.Contains(commands, name); got != tt.wantPersonal {
					t.Errorf("personal command %s loaded = %v, want %v: %v", name, got, tt.wantPersonal, commands)
				}
			}
		})
	}
}
