package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

// shell runs a command line through /bin/sh, as Claude Code runs hooks, and
// returns what it printed.
func shell(t *testing.T, command string) string {
	t.Helper()

	out, err := exec.CommandContext(context.Background(), "/bin/sh", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c %q: %v\n%s", command, err, out)
	}

	return string(out)
}

func TestShellWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		// quoted is whether the word needs quotes.
		quoted bool
	}{
		{name: "plain path", in: "/opt/nobody/bin/nobody-hook"},
		{name: "relative path", in: "bin/nobody-hook"},
		{name: "space (the macOS app bundle)", in: "/Users/me/Nobody Inc.app/Contents/MacOS/nobody-hook", quoted: true},
		{name: "apostrophe", in: "/Users/o'brien/bin/nobody-hook", quoted: true},
		{name: "dollar and backtick", in: "/tmp/$HOME/`id`/nobody-hook", quoted: true},
		{name: "semicolon", in: "/tmp/a;b/nobody-hook", quoted: true},
		{name: "empty", in: "", quoted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			word := shellWord(tt.in)

			if quoted := word != tt.in; quoted != tt.quoted {
				t.Errorf("shellWord(%q) = %q, quoted = %v, want %v", tt.in, word, quoted, tt.quoted)
			}

			// The shell reads it back as the one word it started from.
			if got := shell(t, "printf %s "+word); got != tt.in {
				t.Errorf("sh read %q from %q, want %q", got, word, tt.in)
			}
		})
	}
}

// The hooks and the statusLine run from a path with spaces and an apostrophe
// (the desktop app's bundle is "Nobody Inc.app"): Claude Code runs them
// through /bin/sh, which cut an unquoted path at its first space.
func TestAgentSettingsRunFromAPathNeedingQuotes(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "Nobody Inc.app", "it's", "MacOS")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	hook := filepath.Join(dir, "nobody-hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'ran:%s' \"$1\"\n"), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}

	settings, err := agentSettings(hook, catalog.Definition{}, "/tmp/project", nil)
	if err != nil {
		t.Fatal(err)
	}

	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"` //nolint:tagliatelle // Claude Code's format
	}
	decode(t, settings, &s)

	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "PermissionRequest"} {
		groups := s.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("hooks of %s = %+v", event, groups)
		}

		if got := shell(t, groups[0].Hooks[0].Command); got != "ran:" {
			t.Errorf("%s hook printed %q, want the script to run without arguments", event, got)
		}
	}

	if got := shell(t, s.StatusLine.Command); !strings.HasSuffix(got, "ran:statusline") {
		t.Errorf("statusLine printed %q, want the script to run with the argument statusline", got)
	}
}
