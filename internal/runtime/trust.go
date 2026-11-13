package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
