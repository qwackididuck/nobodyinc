//go:build contract

package contract

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// The trust rules runtime.ProjectTrusted applies are Claude Code's: a folder
// is trusted when it, or one of its parents, has hasTrustDialogAccepted in
// Claude Code's global config, under its real path (symlinks resolved).
//
// Each case runs the interactive CLI with its own CLAUDE_CONFIG_DIR, so the
// user's config is neither read nor changed, and compares what Claude Code
// shows (its trust question, or its prompt) with ProjectTrusted.
// Not parallel: t.Setenv(CLAUDE_CONFIG_DIR) points ProjectTrusted at each
// case's config.
func TestTrustRules(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	child := filepath.Join(base, "child")
	if err := os.MkdirAll(child, 0o750); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		projects map[string]bool
		want     bool
	}{
		{name: "nothing trusted", projects: map[string]bool{}, want: false},
		{name: "the folder trusted", projects: map[string]bool{child: true}, want: true},
		{name: "a parent trusted", projects: map[string]bool{base: true}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			writeClaudeConfig(t, configDir, tt.projects)
			t.Setenv("CLAUDE_CONFIG_DIR", configDir)

			predicted, err := runtime.ProjectTrusted(child)
			if err != nil {
				t.Fatal(err)
			}

			if shown := !trustQuestionShown(t, configDir, child); shown != tt.want || predicted != tt.want {
				t.Fatalf("Claude Code trusts the folder: %v; ProjectTrusted says %v; want %v", shown, predicted, tt.want)
			}
		})
	}
}

func writeClaudeConfig(t *testing.T, dir string, projects map[string]bool) {
	t.Helper()

	entries := map[string]any{}
	for p, trusted := range projects {
		entries[p] = map[string]bool{"hasTrustDialogAccepted": trusted}
	}

	// Onboarding done, so the first screen is the trust question or the prompt.
	data, err := json.Marshal(map[string]any{
		"hasCompletedOnboarding": true, "lastOnboardingVersion": "2.1.322", "theme": "dark", "projects": entries,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// trustQuestionShown starts claude in dir and reports whether its first
// screen is the trust question ("Quick safety check") rather than the prompt.
func trustQuestionShown(t *testing.T, configDir, dir string) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--model", "haiku", "--setting-sources", "local")
	cmd.Dir = dir

	cmd.Env = append(cleanEnv(), "CLAUDE_CONFIG_DIR="+configDir)

	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill() //nolint:errcheck // the process may already be gone
		terminal.Close()
	}()

	var screen strings.Builder

	buf := make([]byte, 64*1024)

	for ctx.Err() == nil {
		n, err := terminal.Read(buf)
		screen.Write(buf[:n])

		switch text := visibleText(screen.String()); {
		case strings.Contains(text, "Quick safety check"):
			return true
		case strings.Contains(text, "❯") && strings.Contains(text, "Claude Code"):
			return false // the prompt is ready: no question was asked
		}

		if err != nil {
			break
		}
	}

	t.Fatalf("claude showed neither its trust question nor its prompt:\n%s", visibleText(screen.String()))

	return false
}

// ansiSequence matches terminal control sequences: the TUI moves the cursor
// between words, so text only reads as such once they are removed.
var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;?<>]*[a-zA-Z~]|\x1b\][^\x07]*\x07`)

// visibleText is what the screen shows, roughly: no control sequences, single
// spaces.
func visibleText(raw string) string {
	return strings.Join(strings.Fields(ansiSequence.ReplaceAllString(raw, " ")), " ")
}

// cleanEnv is the environment without the markers of a parent Claude Code
// session (see runtime.agentEnvironment).
func cleanEnv() []string {
	var env []string

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "CLAUDECODE" || name == "CLAUDE_CONFIG_DIR" || strings.HasPrefix(name, "CLAUDE_CODE_") && name != "CLAUDE_CODE_USE_VERTEX" {
			continue
		}

		env = append(env, kv)
	}

	return env
}
