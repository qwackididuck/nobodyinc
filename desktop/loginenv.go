package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// An app started from the Finder or the Dock does not get the environment of
// the user's shell: macOS gives it a minimal one, with a PATH of
// /usr/bin:/bin:/usr/sbin:/sbin. Without the shell's PATH, claude (usually in
// ~/.local/bin or Homebrew's directory) is not found; without the rest,
// Claude Code does not find its credentials (ANTHROPIC_AUTH_TOKEN,
// ANTHROPIC_BASE_URL...), and the MCP servers whose configuration refers to a
// variable (${GITLAB_TOKEN}, expanded by Claude Code from the daemon's
// environment, docs/agents.md) get nothing. So the app reads the environment
// of the user's login shell, as an interactive shell sets it, and takes it as
// its own: the launcher, every project daemon and every agent inherit it, as
// they do when the launcher is started from a terminal (ADR-0016).

// envMarker frames the environment in the shell's output, so that whatever
// the shell's startup files print is ignored.
const envMarker = "__NOBODY_ENV__"

// shellTimeout bounds the login shell's start-up.
const shellTimeout = 10 * time.Second

// keptByTheApp are the variables the app keeps as macOS gave them: they
// describe the process or the terminal the shell runs in, not the user.
var (
	keptByTheApp = []string{
		"_", "PWD", "OLDPWD", "SHLVL", "TERM", "TMPDIR", "COMMAND_MODE", "XPC_FLAGS", "XPC_SERVICE_NAME",
		"__CF_USER_TEXT_ENCODING", "__CFBundleIdentifier", "COLORTERM", "COLORFGBG",
	}
	keptByTheAppPrefixes = []string{"TERM_", "ITERM_", "LC_TERMINAL", "P9K_", "_P9K_"}
)

// useLoginShellEnv gives the app the login shell's environment, and returns
// where claude is, or "" if it is still not found. The values are never
// logged: some are credentials.
func useLoginShellEnv(logger *slog.Logger) string {
	vars, err := loginShellEnv(os.Getenv("SHELL"))
	if err != nil {
		logger.Warn("read the login shell's environment: keeping the app's", "err", err, "path", os.Getenv("PATH"))
	} else {
		imported := 0

		for name, value := range vars {
			if err := os.Setenv(name, value); err != nil {
				logger.Warn("set an environment variable", "name", name, "err", err)

				continue
			}

			imported++
		}

		logger.Info("environment imported from the login shell", "variables", imported)
	}

	claude, err := exec.LookPath("claude")
	if err != nil {
		logger.Error("claude not found in PATH", "path", os.Getenv("PATH"))

		return ""
	}

	logger.Info("claude found", "path", claude)

	return claude
}

// loginShellEnv asks the user's shell (zsh if unknown) for its environment,
// as an interactive login shell sets it.
func loginShellEnv(shell string) (map[string]string, error) {
	if shell == "" {
		shell = "/bin/zsh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	// env -0 ends each variable with a NUL: a value may hold a newline.
	script := `printf '` + envMarker + `'; env -0; printf '` + envMarker + `'`

	out, err := exec.CommandContext(ctx, shell, "-ilc", script).Output() //nolint:gosec // the user's own shell
	if err != nil {
		return nil, err
	}

	return envFromOutput(string(out))
}

// envFromOutput reads the environment between the markers in the shell's
// output, NUL-separated. It leaves out what the session of a parent Claude
// Code would give agents (runtime.CleanEnvironment) and what the app keeps
// (keptByTheApp). A PATH is required: without one, the output is not an
// environment.
func envFromOutput(out string) (map[string]string, error) {
	_, rest, found := strings.Cut(out, envMarker)
	if !found {
		return nil, errors.New("no environment in the shell's output")
	}

	block, _, found := strings.Cut(rest, envMarker)
	if !found {
		return nil, errors.New("no environment in the shell's output")
	}

	vars := map[string]string{}

	for _, entry := range runtime.CleanEnvironment(strings.Split(block, "\x00")) {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" || keptByApp(name) {
			continue
		}

		vars[name] = value
	}

	if vars["PATH"] == "" {
		return nil, errors.New("no PATH in the shell's environment")
	}

	return vars, nil
}

// keptByApp reports whether the app keeps its own value of name.
func keptByApp(name string) bool {
	if slices.Contains(keptByTheApp, name) {
		return true
	}

	return slices.ContainsFunc(keptByTheAppPrefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}
