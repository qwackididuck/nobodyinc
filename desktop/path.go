package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// An app started from the Finder or the Dock does not get the PATH of the
// user's shell (macOS gives it /usr/bin:/bin:/usr/sbin:/sbin), so claude,
// usually in ~/.local/bin or Homebrew's directory, would not be found. The
// app reads the PATH of the user's login shell instead, as an interactive
// shell sets it, and gives it to the launcher and every project daemon.

// pathMarker frames the PATH in the shell's output, so that whatever the
// shell's startup files print is ignored.
const pathMarker = "__NOBODY_PATH__"

// shellTimeout bounds the login shell's start-up.
const shellTimeout = 10 * time.Second

// useLoginShellPath sets PATH to the login shell's, and returns where claude
// is, or "" if it is still not found.
func useLoginShellPath(logger *slog.Logger) string {
	path, err := loginShellPath(os.Getenv("SHELL"))
	if err != nil {
		logger.Warn("read the login shell's PATH: keeping the app's", "err", err, "path", os.Getenv("PATH"))
	} else if err := os.Setenv("PATH", path); err != nil {
		logger.Warn("set PATH", "err", err)
	}

	claude, err := exec.LookPath("claude")
	if err != nil {
		logger.Error("claude not found in PATH", "path", os.Getenv("PATH"))

		return ""
	}

	logger.Info("claude found", "path", claude)

	return claude
}

// loginShellPath asks the user's shell (zsh if unknown) for its PATH, as an
// interactive login shell sets it.
func loginShellPath(shell string) (string, error) {
	if shell == "" {
		shell = "/bin/zsh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	script := `printf '` + pathMarker + `%s` + pathMarker + `' "$PATH"`

	out, err := exec.CommandContext(ctx, shell, "-ilc", script).Output() //nolint:gosec // the user's own shell
	if err != nil {
		return "", err
	}

	return pathFromOutput(string(out))
}

// pathFromOutput finds the PATH between the markers in the shell's output.
func pathFromOutput(out string) (string, error) {
	_, rest, found := strings.Cut(out, pathMarker)
	if !found {
		return "", errors.New("no PATH in the shell's output")
	}

	path, _, found := strings.Cut(rest, pathMarker)
	if !found || path == "" {
		return "", errors.New("no PATH in the shell's output")
	}

	return path, nil
}
