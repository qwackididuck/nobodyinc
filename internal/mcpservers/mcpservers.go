// Package mcpservers finds the MCP servers the agents get, in Claude Code's
// own files (ADR-0010).
//
// Claude Code has three MCP scopes:
//
//   - project: .mcp.json at the project's root, committed, for everyone;
//   - local: the user on this project only, in ~/.claude.json under
//     projects.<project path>.mcpServers;
//   - user: the user on all their projects, in ~/.claude.json under
//     mcpServers.
//
// Agents are started with --strict-mcp-config, and their settings sources
// leave the user's out, so Claude Code would load none of the user's servers
// (checked by test/contract). Nobody Inc reads the three scopes itself,
// merges them, the project winning on a name conflict, then local, then user,
// and hands the result to every agent (runtime.Launch).
//
// ~/.claude.json is found as Claude Code finds it: in $CLAUDE_CONFIG_DIR when
// set, else in the home directory. Nobody Inc only reads these files: changes
// go through Claude Code itself (writer.go).
package mcpservers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// OfficeServer is the name of the office MCP server every agent gets
// (internal/officemcp). A server with this name in any scope is ignored.
const OfficeServer = "nobody"

// Scope is one of Claude Code's MCP scopes.
type Scope string

// The scopes, from the one that wins a name conflict to the one that loses.
const (
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
	ScopeUser    Scope = "user"
)

// Scopes lists every scope, the winning one first.
var Scopes = []Scope{ScopeProject, ScopeLocal, ScopeUser}

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool {
	return s == ScopeProject || s == ScopeLocal || s == ScopeUser
}

// Config is one server in Claude Code's format (type, command, args, env,
// url, headers...), passed to the agents as is. Values may hold ${VARIABLE}
// references, which Claude Code expands when it starts the server.
type Config = map[string]any

// Server is a server and the scope it comes from.
type Server struct {
	Scope  Scope  `json:"scope"`
	Config Config `json:"config"`
}

// Found is what Load found.
type Found struct {
	// Servers are the merged servers, by name.
	Servers map[string]Server
	// Ignored are servers that lost, or that are named like the office
	// server, as "<name> (<scope>): <why>"; for a warning.
	Ignored []string
}

// projectFile is the project scope's file, at the project's root.
const projectFile = ".mcp.json"

// claudeFile is Claude Code's state file, holding the local and user scopes.
const claudeFile = ".claude.json"

// ClaudeConfigPath is where Claude Code keeps ~/.claude.json: in
// $CLAUDE_CONFIG_DIR when set, else in the home directory.
func ClaudeConfigPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, claudeFile), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find %s: %w", claudeFile, err)
	}

	return filepath.Join(home, claudeFile), nil
}

// ProjectFile is the project's .mcp.json.
func ProjectFile(projectDir string) string {
	return filepath.Join(projectDir, projectFile)
}

// Load reads and merges the three scopes of the project at projectDir (its
// main checkout), with ~/.claude.json at claudeConfig. Missing files have no
// servers; a file that cannot be parsed is an error, so that a file being
// rewritten never looks like servers that went away.
func Load(projectDir, claudeConfig string) (Found, error) {
	project, err := readServers(ProjectFile(projectDir))
	if err != nil {
		return Found{}, err
	}

	local, user, err := readClaudeConfig(claudeConfig, projectDir)
	if err != nil {
		return Found{}, err
	}

	return merge(map[Scope]map[string]Config{ScopeProject: project, ScopeLocal: local, ScopeUser: user}), nil
}

// merge keeps, for each name, the server of the winning scope.
func merge(byScope map[Scope]map[string]Config) Found {
	found := Found{Servers: map[string]Server{}}

	for _, scope := range Scopes {
		for name, cfg := range byScope[scope] {
			switch winner, taken := found.Servers[name]; {
			case name == OfficeServer:
				found.Ignored = append(found.Ignored, fmt.Sprintf("%s (%s): the name is reserved for the office server", name, scope))
			case taken:
				found.Ignored = append(found.Ignored, fmt.Sprintf("%s (%s): replaced by the %s server of the same name", name, scope, winner.Scope))
			default:
				found.Servers[name] = Server{Scope: scope, Config: cfg}
			}
		}
	}

	slices.Sort(found.Ignored)

	return found
}

// readServers reads the mcpServers of a .mcp.json.
func readServers(path string) (map[string]Config, error) {
	var file struct {
		MCPServers map[string]Config `json:"mcpServers"` //nolint:tagliatelle // Claude Code's format
	}

	if err := readJSON(path, &file); err != nil {
		return nil, err
	}

	return file.MCPServers, nil
}

// readClaudeConfig reads the local servers (for projectDir) and the user
// servers of ~/.claude.json.
func readClaudeConfig(path, projectDir string) (map[string]Config, map[string]Config, error) {
	var file struct {
		MCPServers map[string]Config `json:"mcpServers"` //nolint:tagliatelle // Claude Code's format
		Projects   map[string]struct {
			MCPServers map[string]Config `json:"mcpServers"` //nolint:tagliatelle // Claude Code's format
		} `json:"projects"`
	}

	if err := readJSON(path, &file); err != nil {
		return nil, nil, err
	}

	var local map[string]Config

	for _, key := range projectKeys(projectDir) {
		if p, ok := file.Projects[key]; ok {
			local = p.MCPServers

			break
		}
	}

	return local, file.MCPServers, nil
}

// projectKeys are the keys a project may have in ~/.claude.json: its path as
// given, and with symbolic links resolved (on macOS, /var is /private/var).
func projectKeys(projectDir string) []string {
	keys := []string{projectDir}

	if resolved, err := filepath.EvalSymlinks(projectDir); err == nil && resolved != projectDir {
		keys = append(keys, resolved)
	}

	return keys
}

// readJSON decodes a JSON file into v; a missing file leaves v empty.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	return nil
}
