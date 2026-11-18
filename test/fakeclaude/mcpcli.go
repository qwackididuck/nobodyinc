package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// mcpCommand plays "claude mcp add-json --scope <scope> <name> <json>" and
// "claude mcp remove --scope <scope> <name>", as the daemon runs them to
// change MCP servers (ADR-0010), with the same files and the same answers:
// the project scope is .mcp.json in the working directory, the local and
// user scopes are in $CLAUDE_CONFIG_DIR/.claude.json. Checked against the
// real CLI by test/contract.
func mcpCommand(args []string) error {
	if len(args) < 4 || args[1] != "--scope" {
		return fmt.Errorf("usage: mcp add-json|remove --scope <scope> <name> [json]: %v", args)
	}

	action, scope, name := args[0], args[2], args[3]

	file, servers, save, err := scopeServers(scope)
	if err != nil {
		return err
	}

	switch action {
	case "add-json":
		err = addServer(servers, scope, name, args[4:])
	case "remove":
		err = removeServer(servers, file, name)
	default:
		err = fmt.Errorf("unknown mcp command %q", action)
	}

	if err != nil {
		return err
	}

	return save()
}

// addServer adds the server whose JSON is rest[0], refusing a name taken.
func addServer(servers map[string]any, scope, name string, rest []string) error {
	if len(rest) == 0 {
		return errors.New("add-json needs the server's JSON")
	}

	if _, exists := servers[name]; exists {
		return fmt.Errorf("MCP server %s already exists in %s config", name, scope)
	}

	var cfg map[string]any
	if err := json.Unmarshal([]byte(rest[0]), &cfg); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	servers[name] = cfg

	return nil
}

func removeServer(servers map[string]any, file, name string) error {
	if _, exists := servers[name]; !exists {
		return fmt.Errorf("No MCP server named %q in %s", name, file) //nolint:staticcheck // the real CLI's message
	}

	delete(servers, name)

	return nil
}

// scopeServers loads the servers of a scope, and returns a function saving
// them back.
func scopeServers(scope string) (string, map[string]any, func() error, error) {
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	if scope == "project" {
		path = ".mcp.json"
	}

	doc, err := readDoc(path)
	if err != nil {
		return "", nil, nil, err
	}

	save := func() error { return writeDoc(path, doc) }

	switch scope {
	case "project":
		return ".mcp.json", child(doc, "mcpServers"), save, nil
	case "user":
		return "user scope", child(doc, "mcpServers"), save, nil
	case "local":
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, nil, err
		}

		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}

		return "local scope", child(child(child(doc, "projects"), cwd), "mcpServers"), save, nil
	default:
		return "", nil, nil, fmt.Errorf("unknown scope %q", scope)
	}
}

func readDoc(path string) (map[string]any, error) {
	doc := map[string]any{}

	data, err := os.ReadFile(path) //nolint:gosec // the test's own files
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	return doc, nil
}

func writeDoc(path string, doc map[string]any) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	// path is .mcp.json, or the .claude.json of the test's CLAUDE_CONFIG_DIR.
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil { //nolint:gosec // see above
		return err
	}

	return os.WriteFile(path, data, fileMode) //nolint:gosec // see above
}

// Modes of the files written, private like Claude Code's own.
const (
	dirMode  = 0o750
	fileMode = 0o600
)

// child returns the object under key, creating it.
func child(doc map[string]any, key string) map[string]any {
	if m, ok := doc[key].(map[string]any); ok {
		return m
	}

	m := map[string]any{}
	doc[key] = m

	return m
}
