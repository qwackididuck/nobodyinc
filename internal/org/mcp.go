package org

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// OfficeServer is the name of the office MCP server every agent gets
// (internal/officemcp). A project cannot declare a server with this name.
const OfficeServer = "nobody"

// mcpFile is the project's MCP registry, inside ConfigDirName.
const mcpFile = "mcp.yaml"

// MCPServer is the configuration of one MCP server, in Claude Code's own
// format (type, command, args, env, url, headers...). It is passed to Claude
// Code as is, so any field Claude Code supports can be used.
//
// The file is committed: secrets are written as ${VARIABLE} references, which
// Claude Code expands from the agent's environment (ADR-0004).
//
// It is an alias, not a named type, so that nested objects (env, headers)
// decode as plain maps too.
type MCPServer = map[string]any

// mcpRegistry is the content of .nobody/mcp.yaml.
type mcpRegistry struct {
	Servers map[string]MCPServer `yaml:"servers"`
}

// readMCPFile reads .nobody/mcp.yaml; a project without it has an empty
// registry.
func readMCPFile(dir string) (map[string]MCPServer, error) {
	path := filepath.Join(ConfigDir(dir), mcpFile)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]MCPServer{}, nil
	}

	if err != nil {
		return nil, err
	}

	var r mcpRegistry

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	// An empty file decodes to io.EOF: a registry without servers.
	if err := dec.Decode(&r); err != nil && strings.TrimSpace(string(data)) != "" {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return r.Servers, nil
}

// validateMCP checks server names and that agents only use declared servers.
func (o *Org) validateMCP() []error {
	var errs []error

	for name := range o.MCP {
		if name == OfficeServer {
			errs = append(errs, fmt.Errorf("%s: the server name %q is reserved for the office server", mcpFile, OfficeServer))
		}
	}

	for _, m := range o.Members() {
		for _, name := range m.Agent.MCP {
			if _, ok := o.MCP[name]; !ok {
				errs = append(errs, fmt.Errorf("agent %s: MCP server %q is not declared in %s (declared: %s)",
					m.Address, name, mcpFile, strings.Join(o.mcpNames(), ", ")))
			}
		}
	}

	return errs
}

func (o *Org) mcpNames() []string {
	names := make([]string, 0, len(o.MCP))
	for name := range o.MCP {
		names = append(names, name)
	}

	slices.Sort(names)

	if len(names) == 0 {
		return []string{"none"}
	}

	return names
}
