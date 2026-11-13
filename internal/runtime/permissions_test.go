package runtime

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// launched is what an agent's claude command says, decoded.
type launched struct {
	args           []string
	permissionMode string
	allowedTools   []string
	allow, deny    []string
	mcpServers     map[string]map[string]any
}

func launch(t *testing.T, o *org.Org, address string) launched {
	t.Helper()

	m, ok := o.Member(address)
	if !ok {
		t.Fatalf("no member %s", address)
	}

	l := Launch{
		ClaudePath: "/bin/claude", HookPath: "/opt/nobody-hook", DaemonURL: "http://127.0.0.1:7420",
		WorkDir: func(string) string { return o.ProjectDir },
	}

	cmd, err := l.Command(context.Background(), o, m, "")
	if err != nil {
		t.Fatal(err)
	}

	got := launched{args: cmd.Args}
	got.permissionMode = argValue(t, cmd.Args, "--permission-mode")
	got.allowedTools = strings.Split(argValue(t, cmd.Args, "--allowedTools"), ",")

	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	decode(t, argValue(t, cmd.Args, "--settings"), &settings)
	got.allow, got.deny = settings.Permissions.Allow, settings.Permissions.Deny

	var mcp struct {
		Servers map[string]map[string]any `json:"mcpServers"` //nolint:tagliatelle // Claude Code's format
	}
	decode(t, argValue(t, cmd.Args, "--mcp-config"), &mcp)
	got.mcpServers = mcp.Servers

	return got
}

func TestLaunchFollowsTheAgentDefinition(t *testing.T) {
	t.Parallel()

	registry := map[string]org.MCPServer{
		"gitlab":  {"type": "stdio", "command": "gitlab-mcp", "env": map[string]any{"GITLAB_TOKEN": "${GITLAB_TOKEN}"}}, //nolint:gosec // a ${VAR} reference, not a credential
		"ragflow": {"type": "http", "url": "http://localhost:9380/mcp"},
	}

	agent := func(name string, d catalog.Definition) org.Agent { return org.Agent{Name: name, Definition: d} }
	o := &org.Org{
		ProjectDir: "/tmp/project",
		MCP:        registry,
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
			agent("plain", catalog.Definition{}),
			agent("listed", catalog.Definition{
				Permissions: catalog.Permissions{Profile: catalog.ProfileAllowlist, Allow: []string{"Bash(go test *)", "WebFetch"}, Deny: []string{"Read(./.env)"}},
				Tools:       []string{"Read", "Grep"},
				MCP:         []string{"gitlab"},
			}),
			agent("auto", catalog.Definition{Permissions: catalog.Permissions{Profile: catalog.ProfileAuto}}),
		}}},
	}

	t.Run("default profile", func(t *testing.T) {
		t.Parallel()

		got := launch(t, o, "plain@dev")

		if got.permissionMode != modeDefault {
			t.Fatalf("permission mode = %q, want %q", got.permissionMode, modeDefault)
		}

		if len(got.allow) != 0 {
			t.Fatalf("default profile allows %v, want nothing", got.allow)
		}

		if len(got.mcpServers) != 1 || got.mcpServers[org.OfficeServer]["url"] != "http://127.0.0.1:7420/mcp/plain@dev" {
			t.Fatalf("MCP servers = %v, want only the office", got.mcpServers)
		}

		if !slices.Equal(got.allowedTools, strings.Split(officeTools, ",")) {
			t.Fatalf("allowed tools = %v, want the office tools only", got.allowedTools)
		}
	})

	t.Run("allowlist profile with its own rules, tools and MCP", func(t *testing.T) {
		t.Parallel()

		got := launch(t, o, "listed@dev")

		if got.permissionMode != modeAcceptEdits {
			t.Fatalf("permission mode = %q, want %q", got.permissionMode, modeAcceptEdits)
		}

		for _, rule := range []string{"Bash(git commit *)", "Bash(go test *)", "WebFetch"} {
			if !slices.Contains(got.allow, rule) {
				t.Errorf("allow misses %q: %v", rule, got.allow)
			}
		}

		if n := strings.Count(strings.Join(got.allow, "\n"), "Bash(go test *)"); n != 1 {
			t.Errorf("Bash(go test *) appears %d times, want once", n)
		}

		for _, rule := range []string{"Bash(git push *)", "Read(./.env)"} {
			if !slices.Contains(got.deny, rule) {
				t.Errorf("deny misses %q: %v", rule, got.deny)
			}
		}

		if slices.Contains(got.allow, "Bash(git *)") {
			t.Error("allowlist must not allow every git command: checkout or apply could write .nobody/")
		}

		if !slices.Contains(got.allowedTools, "Read") || !slices.Contains(got.allowedTools, "mcp__nobody__send_message") {
			t.Errorf("allowed tools = %v, want office tools and Read", got.allowedTools)
		}

		gitlab := got.mcpServers["gitlab"]

		env, _ := gitlab["env"].(map[string]any)
		if gitlab["command"] != "gitlab-mcp" || env["GITLAB_TOKEN"] != "${GITLAB_TOKEN}" {
			t.Errorf("gitlab server = %v, want the registry entry with its ${VAR} left as is", gitlab)
		}

		if _, ok := got.mcpServers["ragflow"]; ok {
			t.Error("ragflow is in the registry but not listed by the agent: it must not be loaded")
		}
	})

	t.Run("auto profile", func(t *testing.T) {
		t.Parallel()

		if got := launch(t, o, "auto@dev"); got.permissionMode != modeAuto {
			t.Fatalf("permission mode = %q, want %q", got.permissionMode, modeAuto)
		}
	})

	t.Run("every agent: .nobody/ and .claude/ denied, strict MCP", func(t *testing.T) {
		t.Parallel()

		want := []string{
			"Edit(./.nobody/**)", "Write(./.nobody/**)", "Edit(//tmp/project/.nobody/**)", "Write(//tmp/project/.nobody/**)",
			"Edit(./.claude/**)", "Write(./.claude/**)", "Edit(//tmp/project/.claude/**)", "Write(//tmp/project/.claude/**)",
		}

		for _, address := range []string{"plain@dev", "listed@dev", "auto@dev"} {
			got := launch(t, o, address)

			for _, rule := range want {
				if !slices.Contains(got.deny, rule) {
					t.Errorf("%s: deny misses %q", address, rule)
				}
			}

			if !slices.Contains(got.args, "--strict-mcp-config") {
				t.Errorf("%s: --strict-mcp-config missing", address)
			}
		}
	})
}

func TestLaunchRefusesAnUndeclaredMCPServer(t *testing.T) {
	t.Parallel()

	o := &org.Org{Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
		{Name: "dev1", MCP: []string{"ghost"}},
	}}}}
	m, _ := o.Member("dev1@dev")

	_, err := Launch{WorkDir: func(string) string { return "" }}.Command(context.Background(), o, m, "")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error = %v, want one naming the undeclared server", err)
	}
}

func argValue(t *testing.T, args []string, flag string) string {
	t.Helper()

	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("%s missing from %v", flag, args)
	}

	return args[i+1]
}

func decode(t *testing.T, s string, v any) {
	t.Helper()

	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
}

func TestLaunchResumesASession(t *testing.T) {
	t.Parallel()

	o := testOrg()
	m, _ := o.Member("archi@specs")
	l := Launch{WorkDir: func(string) string { return o.ProjectDir }}

	fresh, err := l.Command(context.Background(), o, m, "")
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(fresh.Args, "--resume") || fresh.Args[len(fresh.Args)-1] != onboardingPrompt {
		t.Fatalf("a fresh start must not resume and must onboard: %v", fresh.Args)
	}

	resumed, err := l.Command(context.Background(), o, m, "s-42")
	if err != nil {
		t.Fatal(err)
	}

	first := resumed.Args[len(resumed.Args)-1]
	if argValue(t, resumed.Args, "--resume") != "s-42" || !strings.HasPrefix(first, resumePrompt) {
		t.Fatalf("a resume must continue s-42 with the resume prompt: %v", resumed.Args)
	}

	// The current organization travels in the first message, for information.
	if !strings.Contains(first, "<current_organization>") || !strings.Contains(first, "archi@specs") {
		t.Fatalf("the resume message does not carry the current organization:\n%s", first)
	}
}
