package runtime

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

//go:embed prompt.tmpl
var promptTemplate string

var prompt = template.Must(template.New("prompt").Parse(promptTemplate))

// onboardingPrompt is the agent's first turn. When it ends, the agent becomes
// idle, which is the state in which incoming mail wakes it up.
const onboardingPrompt = `You just joined Nobody Inc. Reply only "Ready." and end your turn; your mail will reach you automatically.`

// officeTools are the office MCP tools every agent may use without being
// asked for permission.
var officeTools = func() string {
	names := officemcp.ToolNames()
	for i, n := range names {
		names[i] = "mcp__nobody__" + n
	}

	return strings.Join(names, ",")
}()

// Launch holds what is needed to start an agent's Claude Code session.
type Launch struct {
	// ClaudePath is the claude executable; defaults to "claude" from PATH.
	ClaudePath string
	// HookPath is the nobody-hook binary wired to the agent's hooks.
	HookPath string
	// DaemonURL is how the agent's hooks and MCP client reach the daemon.
	DaemonURL string
	// ProjectDir is the repository the agent works in.
	ProjectDir string
}

// Command builds the claude command for one agent:
//
//   - its system prompt explains the organization, its role and how mail works;
//   - its UserPromptSubmit, PostToolUse and Stop hooks run nobody-hook to
//     receive mail (docs/hooks.md), and its PermissionRequest hook sends
//     permission requests to the human's approval queue (docs/approvals.md);
//   - the office MCP server is added at /mcp/{address}, keeping the user's own
//     MCP servers available;
//   - NOBODY_AGENT and NOBODY_DAEMON_URL tell nobody-hook who and where it is;
//   - a short onboarding turn brings the agent to the idle state.
func (l Launch) Command(ctx context.Context, o *org.Org, m org.Member) (*exec.Cmd, error) {
	systemPrompt, err := SystemPrompt(o, m)
	if err != nil {
		return nil, err
	}

	settings, err := hookSettings(l.HookPath)
	if err != nil {
		return nil, err
	}

	mcpConfig, err := officeMCPConfig(l.DaemonURL, m.Address)
	if err != nil {
		return nil, err
	}

	args := []string{
		// Only project and local settings: the user's global settings (plugins,
		// hooks, permission mode) must not leak into agents. See ADR-0003.
		"--setting-sources", "project,local",
		"--append-system-prompt", systemPrompt,
		"--settings", settings,
		"--mcp-config", mcpConfig,
		"--allowedTools", officeTools,
	}
	if m.Agent.Model != "" {
		args = append(args, "--model", m.Agent.Model)
	}

	args = append(args, onboardingPrompt)

	claude := l.ClaudePath
	if claude == "" {
		claude = "claude"
	}

	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = l.ProjectDir
	cmd.Env = agentEnvironment(cmd.Environ(), m.Address, l.DaemonURL)

	return cmd, nil
}

// parentSessionVars identify the Claude Code session the daemon may have been
// started from. An agent inheriting them would believe it is a child of that
// session (no transcript, messaging access to the parent), so they are removed.
var parentSessionVars = []string{
	"CLAUDECODE", "CLAUDE_PID", "CLAUDE_JOB_DIR", "CLAUDE_EFFORT",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SSE_PORT", "CLAUDE_CODE_HOST_SHORT_TEMP_DIRS",
}

var parentSessionPrefixes = []string{"CLAUDE_CODE_SESSION_", "CLAUDE_CODE_MESSAGING_"}

// agentEnvironment is the daemon's environment without the parent session
// markers, plus the variables nobody-hook needs.
func agentEnvironment(base []string, address, daemonURL string) []string {
	return append(withoutParentSession(base), "NOBODY_AGENT="+address, "NOBODY_DAEMON_URL="+daemonURL)
}

// withoutParentSession removes the parent session markers from an environment.
func withoutParentSession(base []string) []string {
	env := make([]string, 0, len(base))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !isParentSessionVar(name) {
			env = append(env, kv)
		}
	}

	return env
}

func isParentSessionVar(name string) bool {
	if slices.Contains(parentSessionVars, name) {
		return true
	}

	for _, prefix := range parentSessionPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// SystemPrompt renders the system prompt appended to an agent's session.
func SystemPrompt(o *org.Org, m org.Member) (string, error) {
	var b bytes.Buffer

	err := prompt.Execute(&b, map[string]any{
		"Address":  m.Address,
		"Team":     m.Team,
		"Agent":    m.Agent,
		"Contacts": officemcp.Contacts(o, m.Address),
	})
	if err != nil {
		return "", fmt.Errorf("render system prompt of %s: %w", m.Address, err)
	}

	return b.String(), nil
}

// PermissionHookTimeout is how long Claude Code lets the PermissionRequest
// hook wait for the human. The daemon gives up a little earlier (see
// server.defaultApprovalWait) so that it always answers before being cut off.
const PermissionHookTimeout = 10 * time.Minute

// hookSettings wires the mail hooks and the permission hook to nobody-hook
// (see docs/hooks.md and docs/approvals.md).
func hookSettings(hookPath string) (string, error) {
	run := []map[string]any{{"hooks": []map[string]any{{"type": "command", "command": hookPath}}}}
	permission := []map[string]any{{"hooks": []map[string]any{{
		"type": "command", "command": hookPath, "timeout": int(PermissionHookTimeout.Seconds()),
	}}}}

	return toJSON(map[string]any{"hooks": map[string]any{
		"UserPromptSubmit":  run,
		"PostToolUse":       run,
		"Stop":              run,
		"PermissionRequest": permission,
	}})
}

// officeMCPConfig declares the office MCP server of an agent.
func officeMCPConfig(daemonURL, address string) (string, error) {
	return toJSON(map[string]any{"mcpServers": map[string]any{
		"nobody": map[string]string{"type": "http", "url": daemonURL + "/mcp/" + address},
	}})
}

func toJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode launch config: %w", err)
	}

	return string(data), nil
}
