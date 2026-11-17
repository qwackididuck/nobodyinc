//go:build contract

package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
)

// What an agent gets from the project it works in, with the flags it is
// started with (runtime.Launch.Command): --setting-sources project,local
// keeps the user's own settings, hooks and plugins out (ADR-0003), and
// --strict-mcp-config gives it only the MCP servers Nobody Inc hands over.

// agentSources are the settings sources an agent is started with.
var agentSources = []string{"--setting-sources", "project,local"}

// projectInitEvent is the part of the stream-json init event that says what
// a session loaded.
type projectInitEvent struct {
	Type          string   `json:"type"`
	Subtype       string   `json:"subtype"`
	Skills        []string `json:"skills"`
	SlashCommands []string `json:"slash_commands"`
	Agents        []string `json:"agents"`
	Tools         []string `json:"tools"`
	MCPServers    []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"mcp_servers"`
}

// The project's Claude Code configuration reaches the agents: its CLAUDE.md,
// and its skills, commands and subagents in .claude/.
func TestProjectConfigurationReachesAgents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProjectFile(t, dir, "CLAUDE.md", "The project codeword is PELICAN.\n")
	writeProjectFile(t, dir, ".claude/skills/probe-skill/SKILL.md", "---\nname: probe-skill\ndescription: A probe skill.\n---\nSay PROBE.\n")
	writeProjectFile(t, dir, ".claude/commands/probe-command.md", "---\ndescription: A probe command.\n---\nSay PROBE.\n")
	writeProjectFile(t, dir, ".claude/agents/probe-agent.md", "---\nname: probe-agent\ndescription: A probe subagent.\n---\nSay PROBE.\n")

	out := askProject(t, dir, "What is the project codeword? Answer with the codeword only.")
	init := projectInit(t, out)

	for _, check := range []struct {
		what   string
		loaded []string
		name   string
	}{
		{"skill", init.Skills, "probe-skill"},
		{"command", init.SlashCommands, "probe-command"},
		{"subagent", init.Agents, "probe-agent"},
	} {
		if !slices.Contains(check.loaded, check.name) {
			t.Errorf("the project's %s %q is not loaded: %v", check.what, check.name, check.loaded)
		}
	}

	if answer := strings.ToUpper(streamResult(t, out)); !strings.Contains(answer, "PELICAN") {
		t.Errorf("the project's CLAUDE.md should reach the session, got %q", answer)
	}
}

// AGENTS.md stands in for a missing CLAUDE.md: it is read when the project
// has no CLAUDE.md, and left out when it has one.
func TestAgentsMDIsReadWithoutCLAUDEMD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		claudeMD  bool
		wantHeron bool
	}{
		{name: "AGENTS.md alone: read", wantHeron: true},
		{name: "with a CLAUDE.md: left out", claudeMD: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeProjectFile(t, dir, "AGENTS.md", "The second codeword is HERON.\n")

			if tt.claudeMD {
				writeProjectFile(t, dir, "CLAUDE.md", "The first codeword is PELICAN.\n")
			}

			out := askProject(t, dir, "What is the second codeword? Answer with the codeword only, or NONE if you were not given one.")

			if got := strings.Contains(strings.ToUpper(streamResult(t, out)), "HERON"); got != tt.wantHeron {
				t.Errorf("AGENTS.md read = %v, want %v (answer %q)", got, tt.wantHeron, streamResult(t, out))
			}
		})
	}
}

// askProject runs one headless turn in dir with the agents' flags, and
// returns its stream-json output.
func askProject(t *testing.T, dir, prompt string) []byte {
	t.Helper()

	args := append([]string{
		"-p", prompt, "--model", "haiku", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
	}, agentSources...)

	return runCLI(t, dir, nil, args)
}

// Where Claude Code finds MCP servers, with the agents' settings sources.
// Nobody Inc relies on this to build each agent's --mcp-config itself:
//   - the project's .mcp.json is ignored with --strict-mcp-config;
//   - the user's own servers (user scope, in .claude.json) are not loaded
//     without the "user" settings source, which would also bring back the
//     user's hooks and plugins (ADR-0003), strict or not;
//   - a server handed over with --mcp-config is started.
func TestMCPServerSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// strict: start with --strict-mcp-config and an empty --mcp-config.
		strict bool
		// handOver: pass the project's .mcp.json with --mcp-config.
		handOver    bool
		wantProject bool
		wantUser    bool
	}{
		{name: "strict: the project's .mcp.json is ignored", strict: true},
		{name: "not strict: the project's .mcp.json is loaded, the user's servers are not", wantProject: true},
		{name: "handed over with --mcp-config: started", strict: true, handOver: true, wantProject: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			configDir := t.TempDir()
			project := probeServer(t, dir, "project")
			user := probeServer(t, dir, "user")

			writeProjectFile(t, dir, ".mcp.json", mcpServers(t, "projprobe", project.command))
			writeProjectFile(t, configDir, ".claude.json", mcpServers(t, "userprobe", user.command))

			args := append([]string{"-p", "Answer OK.", "--model", "haiku", "--output-format", "json"}, agentSources...)

			switch {
			case tt.handOver:
				args = append(args, "--strict-mcp-config", "--mcp-config", filepath.Join(dir, ".mcp.json"))
			case tt.strict:
				args = append(args, "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
			}

			// CLAUDE_CONFIG_DIR stands for the user's ~/.claude: their
			// .claude.json, with a user-scope server, is never touched.
			runCLI(t, dir, []string{"CLAUDE_CONFIG_DIR=" + configDir}, args)

			if got := project.started(); got != tt.wantProject {
				t.Errorf("project server started = %v, want %v", got, tt.wantProject)
			}

			if got := user.started(); got != tt.wantUser {
				t.Errorf("user server started = %v, want %v", got, tt.wantUser)
			}
		})
	}
}

// probe is a fake MCP server: a script that only records it was started.
type probe struct {
	command string
	marker  string
}

func (p probe) started() bool {
	_, err := os.Stat(p.marker)

	return err == nil
}

func probeServer(t *testing.T, dir, name string) probe {
	t.Helper()

	p := probe{command: filepath.Join(dir, name+"-probe.sh"), marker: filepath.Join(dir, name+"-started")}
	script := "#!/bin/sh\ntouch '" + p.marker + "'\n"

	if err := os.WriteFile(p.command, []byte(script), 0o700); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}

	return p
}

// mcpServers is a .mcp.json (or .claude.json) declaring one stdio server.
func mcpServers(t *testing.T, name, command string) string {
	t.Helper()

	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{name: map[string]string{"type": "stdio", "command": command}}})
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func writeProjectFile(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// projectInit returns what the init event of a stream-json session says it
// loaded.
func projectInit(t *testing.T, out []byte) projectInitEvent {
	t.Helper()

	var found projectInitEvent

	eachEvent(out, func(line []byte) bool {
		var ev projectInitEvent
		if json.Unmarshal(line, &ev) == nil && ev.Type == "system" && ev.Subtype == "init" {
			found = ev

			return false
		}

		return true
	})

	if found.Type == "" {
		t.Fatalf("no init event in claude output:\n%s", out)
	}

	return found
}

// --disallowedTools takes an MCP server away with mcp__<server>, or one of its
// tools with mcp__<server>__<tool>: how an agent's mcp_exclude is applied
// (runtime.disallowedTools, ADR-0010).
func TestDisallowedToolsExcludeMCPServersAndTools(t *testing.T) {
	t.Parallel()

	url := probeMCPServer(t, "search", "delete_document")
	config := `{"mcpServers":{"probe":{"type":"http","url":"` + url + `"}}}`

	tests := []struct {
		name       string
		disallowed string
		want       []string
	}{
		{name: "nothing excluded", want: []string{"mcp__probe__delete_document", "mcp__probe__search"}},
		{name: "one tool", disallowed: "mcp__probe__delete_document", want: []string{"mcp__probe__search"}},
		{name: "the whole server", disallowed: "mcp__probe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{
				"-p", "Answer OK.", "--model", "haiku", "--output-format", "stream-json", "--verbose",
				"--strict-mcp-config", "--mcp-config", config,
			}, agentSources...)
			if tt.disallowed != "" {
				args = append(args, "--disallowedTools", tt.disallowed)
			}

			var got []string

			for _, tool := range projectInit(t, runCLI(t, "", nil, args)).Tools {
				if strings.HasPrefix(tool, "mcp__probe") {
					got = append(got, tool)
				}
			}

			slices.Sort(got)

			if !slices.Equal(got, tt.want) {
				t.Fatalf("probe tools = %v, want %v", got, tt.want)
			}
		})
	}
}

// `claude mcp add-json` and `claude mcp remove` write each scope where
// mcpservers.Load reads it: how Nobody Inc changes MCP servers (ADR-0010).
// Run in a throwaway project with a throwaway CLAUDE_CONFIG_DIR; no model
// is called.
func TestMCPCommandsWriteWhereNobodyReads(t *testing.T) {
	t.Parallel()

	for _, scope := range mcpservers.Scopes {
		t.Run(string(scope), func(t *testing.T) {
			t.Parallel()

			project := t.TempDir()
			configDir := t.TempDir()
			env := []string{"CLAUDE_CONFIG_DIR=" + configDir}
			server := `{"type":"http","url":"http://localhost:9380/mcp","headers":{"Authorization":"Bearer ${RAGFLOW_KEY}"}}`

			runCLI(t, project, env, []string{"mcp", "add-json", "--scope", string(scope), "ragflow", server})

			found, err := mcpservers.Load(project, filepath.Join(configDir, ".claude.json"))
			if err != nil {
				t.Fatal(err)
			}

			got := found.Servers["ragflow"]
			headers, _ := got.Config["headers"].(map[string]any)

			if got.Scope != scope || headers["Authorization"] != "Bearer ${RAGFLOW_KEY}" {
				t.Fatalf("after add-json: %+v, want ragflow in %s with its variable unexpanded", found.Servers, scope)
			}

			runCLI(t, project, env, []string{"mcp", "remove", "--scope", string(scope), "ragflow"})

			if found, err := mcpservers.Load(project, filepath.Join(configDir, ".claude.json")); err != nil || len(found.Servers) != 0 {
				t.Fatalf("after remove: %+v, %v", found.Servers, err)
			}
		})
	}
}

// probeMCPServer serves an MCP server offering the given tools, and returns
// its URL.
func probeMCPServer(t *testing.T, tools ...string) string {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "probe"}, nil)
	for _, name := range tools {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Does " + name + "."},
			func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
				return nil, struct{}{}, nil
			})
	}

	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(srv.Close)

	return srv.URL
}
