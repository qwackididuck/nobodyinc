package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// block is an environment as env -0 prints it: each variable ends with a NUL.
func block(entries ...string) string {
	var b strings.Builder

	b.WriteString(envMarker)

	for _, e := range entries {
		b.WriteString(e)
		b.WriteByte(0)
	}

	b.WriteString(envMarker)

	return b.String()
}

func TestEnvFromOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		out     string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "credentials and the PATH come through",
			out:  block("PATH=/a:/b", "ANTHROPIC_AUTH_TOKEN=secret", "GITLAB_TOKEN=glpat-x", "HOME=/Users/me"),
			want: map[string]string{"PATH": "/a:/b", "ANTHROPIC_AUTH_TOKEN": "secret", "GITLAB_TOKEN": "glpat-x", "HOME": "/Users/me"},
		},
		{
			name: "after what the startup files print",
			out:  "Welcome!\n" + block("PATH=/a") + "\nbye\n",
			want: map[string]string{"PATH": "/a"},
		},
		{
			name: "a value with a newline and an equal sign",
			out:  block("PATH=/a", "PS1=line one\nline=two"),
			want: map[string]string{"PATH": "/a", "PS1": "line one\nline=two"},
		},
		{
			name: "the app keeps what describes its process and its terminal",
			out: block("PATH=/a", "PWD=/tmp", "OLDPWD=/", "SHLVL=2", "TERM=xterm", "TERM_PROGRAM=iTerm.app", "ITERM_SESSION_ID=w0",
				"TMPDIR=/var/x", "XPC_FLAGS=0x0", "_=/usr/bin/env", "P9K_TTY=old", "LC_TERMINAL=iTerm2", "EDITOR=vim"),
			want: map[string]string{"PATH": "/a", "EDITOR": "vim"},
		},
		{
			name: "the markers of a parent Claude Code session are left out",
			out:  block("PATH=/a", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=abc", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_USE_VERTEX=1"),
			want: map[string]string{"PATH": "/a", "CLAUDE_CODE_USE_VERTEX": "1"},
		},
		{
			name: "an entry without a name or an equal sign is ignored",
			out:  block("PATH=/a", "=orphan", "garbage"),
			want: map[string]string{"PATH": "/a"},
		},
		{name: "no marker", out: "PATH=/a\x00", wantErr: true},
		{name: "marker never closed", out: envMarker + "PATH=/a\x00", wantErr: true},
		{name: "no PATH", out: block("HOME=/Users/me"), wantErr: true},
		{name: "empty PATH", out: block("PATH="), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := envFromOutput(tt.out)
			if (err != nil) != tt.wantErr || !maps.Equal(got, tt.want) {
				t.Errorf("envFromOutput = %v, %v; want %v, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// A shell that prints its environment between the markers, as the real ones
// do through env -0, is read through the whole plumbing (arguments, markers,
// parsing), whatever the machine's own environment.
func TestLoginShellEnvWithAFakeShell(t *testing.T) {
	t.Parallel()

	shell := filepath.Join(t.TempDir(), "fakeshell")
	script := "#!/bin/sh\nprintf '" + envMarker + "PATH=/fake/bin\\0ANTHROPIC_BASE_URL=https://gateway.example\\0" + envMarker + "'\n"

	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}

	got, err := loginShellEnv(shell)
	if err != nil {
		t.Fatal(err)
	}

	if want := map[string]string{"PATH": "/fake/bin", "ANTHROPIC_BASE_URL": "https://gateway.example"}; !maps.Equal(got, want) {
		t.Errorf("loginShellEnv = %v, want %v", got, want)
	}
}

// The real login shell, with the real env -0, hands back the variables it
// was started with. Not parallel: t.Setenv.
func TestLoginShellEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "token-for-the-test")

	got, err := loginShellEnv("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}

	if got["PATH"] == "" || got["ANTHROPIC_AUTH_TOKEN"] != "token-for-the-test" {
		t.Errorf("the login shell's environment lacks PATH or the variable it was started with: PATH %q, %d variables", got["PATH"], len(got))
	}
}
