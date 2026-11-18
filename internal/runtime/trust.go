package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
)

// Claude Code asks whether to trust a folder the first time it starts there,
// and an agent waiting on that question never starts working
// (docs/workspaces.md). The answer is kept in Claude Code's global config,
// ~/.claude.json (or $CLAUDE_CONFIG_DIR/.claude.json):
//
//	{"projects": {"/real/path/of/project": {"hasTrustDialogAccepted": true}}}
//
// Trusting a folder covers its subfolders, so trusting the project covers the
// team worktrees under <project>/.nobody/worktrees/. Paths are the real ones,
// symbolic links resolved (e.g. /private/var on macOS). These rules are
// checked against the real CLI by task contract (TestTrustRules).

// claudeConfig is the part of Claude Code's global config we read: only the
// trust flags, nothing else.
type claudeConfig struct {
	Projects map[string]struct {
		HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"` //nolint:tagliatelle // Claude Code's format
	} `json:"projects"`
}

// ProjectTrusted reports whether Claude Code trusts dir, or one of its
// parents. A missing Claude Code config means nothing is trusted yet.
func ProjectTrusted(dir string) (bool, error) {
	path, err := claudeConfigPath()
	if err != nil {
		return false, err
	}

	return trustedIn(path, dir)
}

// claudeConfigPath is Claude Code's global config file.
func claudeConfigPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find Claude Code's config: %w", err)
	}

	return filepath.Join(home, ".claude.json"), nil
}

// trustedIn applies the trust rules to the config file at configPath.
func trustedIn(configPath, dir string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	var config claudeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return false, fmt.Errorf("read Claude Code's config %s: %w", configPath, err)
	}

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}

	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return false, err
	}

	for p := resolved; ; p = filepath.Dir(p) {
		if config.Projects[p].HasTrustDialogAccepted {
			return true, nil
		}

		if parent := filepath.Dir(p); parent == p {
			return false, nil
		}
	}
}

// ErrNoClaudeConfig is returned by Trust when Claude Code has no global
// config yet: it has never been set up on this machine.
var ErrNoClaudeConfig = errors.New("no Claude Code config yet: run claude once")

// Trust makes Claude Code trust dir, as accepting its trust question would,
// and reports whether it had to: nothing is written when dir or a parent is
// already trusted.
//
// The daemon trusts its own directory (~/.nobody) this way, before starting
// any agent (ADR-0011): every agent gets the staging directory of the
// personal skills with --add-dir, and Claude Code would otherwise ask, at
// every session start, whether to trust it. Claude Code owns the file and
// rewrites it while it runs, so the write is kept short: the file is read
// again just before, every other key is kept, and the new file replaces the
// old one atomically. A change made by a running Claude Code in between could
// still be lost; the next start trusts the directory again if needed.
func Trust(dir string) (bool, error) {
	path, err := claudeConfigPath()
	if err != nil {
		return false, err
	}

	return trustIn(path, dir)
}

// trustIn is Trust on the config file at configPath.
func trustIn(configPath, dir string) (bool, error) {
	if trusted, err := trustedIn(configPath, dir); err != nil || trusted {
		return false, err
	}

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}

	data, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, ErrNoClaudeConfig
	}

	if err != nil {
		return false, err
	}

	updated, err := withTrustedProject(data, resolved)
	if err != nil {
		return false, fmt.Errorf("trust %s in Claude Code's config %s: %w", dir, configPath, err)
	}

	if err := atomicfile.WritePrivate(configPath, updated); err != nil {
		return false, fmt.Errorf("trust %s in Claude Code's config: %w", dir, err)
	}

	return true, nil
}

// withTrustedProject returns Claude Code's config with
// projects.<dir>.hasTrustDialogAccepted set, every other key kept as it was
// (numbers included).
func withTrustedProject(data []byte, dir string) ([]byte, error) {
	var config map[string]any

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	if err := dec.Decode(&config); err != nil {
		return nil, err
	}

	projects, _ := config["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}

	project, _ := projects[dir].(map[string]any)
	if project == nil {
		project = map[string]any{}
	}

	project["hasTrustDialogAccepted"] = true
	projects[dir] = project
	config["projects"] = projects

	return json.MarshalIndent(config, "", "  ")
}
