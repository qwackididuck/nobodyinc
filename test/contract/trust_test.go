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

			if shown := !questionShown(t, configDir, child, trustQuestion); shown != tt.want || predicted != tt.want {
				t.Fatalf("Claude Code trusts the folder: %v; ProjectTrusted says %v; want %v", shown, predicted, tt.want)
			}
		})
	}
}

// A directory given with --add-dir asks again at every session start unless
// Claude Code trusts it, or one of its parents: that is why the daemon trusts
// ~/.nobody, where the staging directory of the personal skills lives
// (runtime.Trust, ADR-0011).
func TestAddedDirectoryNeedsTrust(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	project := filepath.Join(base, "project")
	home := filepath.Join(base, "nobody")
	staging := filepath.Join(home, "projects", "p", "skills")

	for _, dir := range []string{project, staging} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name         string
		trustHome    bool
		wantQuestion bool
	}{
		{name: "untrusted: asked", wantQuestion: true},
		{name: "a parent trusted: not asked", trustHome: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			configDir := t.TempDir()
			writeClaudeConfig(t, configDir, map[string]bool{project: true, home: tt.trustHome})

			if got := questionShown(t, configDir, project, addDirQuestion, "--add-dir", staging); got != tt.wantQuestion {
				t.Fatalf("asked about the added directory: %v, want %v", got, tt.wantQuestion)
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

// The questions Claude Code asks before its prompt.
const (
	// trustQuestion is asked in a folder Claude Code does not trust.
	trustQuestion = "Quick safety check"
	// addDirQuestion is asked for an untrusted directory given with
	// --add-dir; the answer only holds for the session.
	addDirQuestion = "Adding a new directory"
)

// questionShown starts claude in dir, with extraArgs, and reports whether its
// first screen is the given question rather than the prompt.
func questionShown(t *testing.T, configDir, dir, question string, extraArgs ...string) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := append([]string{"--model", "haiku", "--setting-sources", "local"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "claude", args...)
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
		case strings.Contains(text, question):
			return true
		case strings.Contains(text, "❯") && strings.Contains(text, "Claude Code"):
			return false // the prompt is ready: no question was asked
		}

		if err != nil {
			break
		}
	}

	t.Fatalf("claude showed neither %q nor its prompt:\n%s", question, visibleText(screen.String()))

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
