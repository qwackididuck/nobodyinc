package runtime

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/atomicfile"
	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/commands"
	"github.com/qwackididuck/nobodyinc/internal/mcpservers"
	"github.com/qwackididuck/nobodyinc/internal/officemcp"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

//go:embed prompt.tmpl
var promptTemplate string

var prompt = template.Must(template.New("prompt").Parse(promptTemplate))

// onboardingPrompt is the agent's first turn. When it ends, the agent becomes
// idle, which is the state in which incoming mail wakes it up. Mail waiting
// since before the start is attached to this very prompt, so the prompt must
// not tell the agent to ignore it.
const onboardingPrompt = `You just joined Nobody Inc. If mail is attached to this message, act on it now. ` +
	`Otherwise reply only "Ready." and end your turn; your mail will reach you automatically.`

// resumePrompt opens the first turn of a resumed session: the agent keeps its
// memory and its system prompt (Claude Code ignores a new
// --append-system-prompt on --resume), while its model, permissions, tools
// and MCP servers follow the new launch flags. A new role or new instructions
// never go through a resume: they restart the agent with a fresh session
// (staffing.needsFreshSession).
//
// What may have changed is the organization (new colleagues, leaves): the
// current organization travels in this message, as information (see
// resumeMessage).
const resumePrompt = `Nobody Inc restarted your session: your model, permissions or tools may have changed, ` +
	`and the organization may have changed too; its current state is below, for your information. ` +
	`If mail is attached to this message, act on it now. ` +
	`Otherwise reply only "Ready." and end your turn; your mail will reach you automatically.`

// resumeMessage is the first prompt of a resumed session: resumePrompt, then
// the agent's current system prompt, for its up-to-date organization chart.
func resumeMessage(systemPrompt string) string {
	return resumePrompt + "\n\n" + currentOrganization(systemPrompt)
}

// interruptedPrompt opens a session resumed after the daemon stopped in the
// middle of the agent's turn (ResumeInterrupted): the turn is work it had
// not finished. The current organization comes from its SessionStart hook
// (ReopenContext).
const interruptedPrompt = `Nobody Inc stopped while you were in the middle of a turn, and has just resumed your session: ` +
	`carry on with what you were doing. If mail is attached to this message, take it into account.`

// reopenIntro introduces the context a session resumed when the project
// opens again gets from its SessionStart hook (ReopenContext).
const reopenIntro = `Nobody Inc reopened the project and resumed your session. ` +
	`The organization may have changed while the project was closed; its current state is below, for your information. ` +
	`Your mail will reach you automatically.`

func currentOrganization(systemPrompt string) string {
	return "<current_organization>\n" + systemPrompt + "\n</current_organization>"
}

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
	// WorkDir returns the directory an agent works in: its team's worktree
	// for the active feature, or the project itself (see docs/workspaces.md).
	WorkDir func(address string) string
	// UserInstructions returns the text of the user's own CLAUDE.md, read at
	// each launch and given to every agent as information (ADR-0008); nil or
	// "" for none.
	UserInstructions func() string
	// MCPConfigDir holds each agent's MCP configuration, <address>.json,
	// readable by the user only (~/.nobody/projects/<id>/mcp).
	MCPConfigDir string
	// SkillsStagingDir links the user's personal skills (skills.Stage) and
	// commands (commands.Stage) for the agents, given to every agent with
	// --add-dir (~/.nobody/projects/<id>/skills, ADR-0011 and ADR-0017); ""
	// gives no personal skill or command.
	SkillsStagingDir string
	// PersonalSkillsDir is where the user's personal skills live
	// (skills.PersonalDir): agents may read them, not write them.
	PersonalSkillsDir string
	// PersonalCommandsDir is where the user's personal commands live
	// (commands.PersonalDir): agents may read them, not write them.
	PersonalCommandsDir string
}

// Command builds the claude command for one agent, from its definition
// (docs/agents.md):
//
//   - its system prompt explains the organization, its role and how mail
//     works, followed by the agent's own prompt;
//   - its UserPromptSubmit, PostToolUse and Stop hooks run nobody-hook to
//     receive mail (docs/hooks.md), and its PermissionRequest hook sends
//     permission requests to the human's approval queue (docs/approvals.md);
//   - its permission profile sets --permission-mode, and its allow and deny
//     rules go into the generated settings, with .nobody/ always denied
//     (permissions.go, ADR-0004);
//   - its tools are allowed without asking, next to the office tools;
//   - it gets the office MCP server (at /mcp/{address}) and every server of
//     Claude Code's three scopes (org.Org.MCP), written to a private file
//     (--strict-mcp-config: Claude Code loads nothing else, ADR-0010); the
//     servers and tools it excludes (mcp_exclude) are disallowed;
//   - it gets every command (ADR-0017): the project's, and the user's
//     personal commands, linked in the same staging directory as the
//     personal skills;
//   - it gets every skill (ADR-0011): the project's, Claude Code's built-in
//     ones, and the user's personal skills, linked in a staging directory
//     given with --add-dir; the skills it excludes (skills_exclude) are
//     turned off with skillOverrides; its SessionStart hook hands it the
//     skills it preloads (skills:), at every start, resumed sessions
//     included;
//   - NOBODY_AGENT and NOBODY_DAEMON_URL tell nobody-hook who and where it is;
//   - a short onboarding turn brings the agent to the idle state.
//
// With a resume session, the agent continues that Claude Code session
// (claude --resume): it keeps its memory and system prompt, and gets its
// current launch flags; how the session opens depends on the resume mode
// (sessions.go).
func (l Launch) Command(ctx context.Context, o *org.Org, m org.Member, resume Resume) (*exec.Cmd, error) {
	systemPrompt, err := SystemPrompt(o, m, l.userInstructions())
	if err != nil {
		return nil, err
	}

	if err := l.stagePersonal(o); err != nil {
		return nil, err
	}

	outside := []string{l.SkillsStagingDir, l.PersonalSkillsDir, l.PersonalCommandsDir}

	settings, err := agentSettings(l.HookPath, m.Agent.Definition, o.ProjectDir, outside)
	if err != nil {
		return nil, err
	}

	mcpConfig, err := l.writeMCPConfig(m.Address, o.MCP)
	if err != nil {
		return nil, err
	}

	args := append(l.flags(m.Agent.Definition, systemPrompt, settings, mcpConfig), firstPrompt(systemPrompt, resume)...)

	claude := l.ClaudePath
	if claude == "" {
		claude = "claude"
	}

	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = l.WorkDir(m.Address)
	cmd.Env = agentEnvironment(cmd.Environ(), m.Address, l.DaemonURL)

	return cmd, nil
}

// ReopenContext is what the SessionStart hook of a session resumed when the
// project opens again hands the agent (ADR-0015): its current system prompt,
// for its up-to-date organization chart, since Claude Code keeps the system
// prompt of the session's first start.
func (l Launch) ReopenContext(o *org.Org, m org.Member) (string, error) {
	systemPrompt, err := SystemPrompt(o, m, l.userInstructions())
	if err != nil {
		return "", err
	}

	return reopenIntro + "\n\n" + currentOrganization(systemPrompt), nil
}

// userInstructions is the text of the user's own CLAUDE.md, "" for none.
func (l Launch) userInstructions() string {
	if l.UserInstructions == nil {
		return ""
	}

	return l.UserInstructions()
}

// SettingSources are the settings sources agents are started with: only
// project and local settings, so that the user's global settings (plugins,
// hooks, permission mode) do not leak into agents (ADR-0003). Anything that
// must see what an agent sees uses them too (skills.Discovery).
var SettingSources = []string{"--setting-sources", "project,local"}

// CleanEnvironment is base without the markers of a Claude Code session the
// daemon may have been started from (see parentSessionVars): what any claude
// the daemon starts must get.
func CleanEnvironment(base []string) []string {
	return withoutParentSession(base)
}

// flags are the launch flags of an agent, from its definition and what
// Command prepared.
func (l Launch) flags(d catalog.Definition, systemPrompt, settings, mcpConfig string) []string {
	args := append(slices.Clone(SettingSources),
		"--append-system-prompt", systemPrompt,
		"--settings", settings,
		"--permission-mode", permissionMode(d.Permissions),
		"--mcp-config", mcpConfig,
		"--strict-mcp-config",
		"--allowedTools", allowedTools(d.Tools),
	)

	if l.SkillsStagingDir != "" {
		args = append(args, "--add-dir", l.SkillsStagingDir)
	}

	if disallowed := disallowedTools(d.MCPExclude); disallowed != "" {
		args = append(args, "--disallowedTools", disallowed)
	}

	if d.Model != "" {
		args = append(args, "--model", d.Model)
	}

	return args
}

// firstPrompt opens the session: the onboarding turn of a new session, or
// the resume of an existing one (sessions.go): with its current system
// prompt after a change (resumePrompt), with nothing when the project opens
// again while the agent was idle, or with a prompt to carry on when its turn
// was interrupted.
func firstPrompt(systemPrompt string, resume Resume) []string {
	if resume.Session == "" {
		return []string{onboardingPrompt}
	}

	switch resume.Mode {
	case ResumeSilently:
		return []string{"--resume", resume.Session}
	case ResumeInterrupted:
		return []string{"--resume", resume.Session, interruptedPrompt}
	case ResumeAfterChange: // below
	}

	return []string{"--resume", resume.Session, resumeMessage(systemPrompt)}
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
//
// It also gives the agent its own git identity, so the history shows which
// agent wrote each commit.
func agentEnvironment(base []string, address, daemonURL string) []string {
	return append(withoutParentSession(base),
		"NOBODY_AGENT="+address,
		"NOBODY_DAEMON_URL="+daemonURL,
		"GIT_AUTHOR_NAME="+GitName(address),
		"GIT_AUTHOR_EMAIL="+GitEmail(address),
		"GIT_COMMITTER_NAME="+GitName(address),
		"GIT_COMMITTER_EMAIL="+GitEmail(address),
	)
}

// GitName is the git author name of an agent: "dev1@dev (Nobody Inc)".
func GitName(address string) string {
	return address + " (Nobody Inc)"
}

// GitEmail is the git author email of an agent: "dev1.dev@nobody.local".
func GitEmail(address string) string {
	return strings.ReplaceAll(address, "@", ".") + "@nobody.local"
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

// SystemPrompt renders the system prompt appended to an agent's session. It
// goes from the general to the particular (docs/agents.md, "The prompt
// cascade"): Nobody Inc and who the agent is, the user's own instructions
// (userInstructions, ADR-0008), the project prompt, the team's goal and
// prompt, the agent's role and instructions, then the organization chart and
// how to communicate, including whom to ask. Empty sections are left out.
func SystemPrompt(o *org.Org, m org.Member, userInstructions string) (string, error) {
	var b bytes.Buffer

	err := prompt.Execute(&b, map[string]any{
		"Address": m.Address,
		"User":    strings.TrimSpace(userInstructions),
		"Project": strings.TrimSpace(o.ProjectPrompt),
		"Team":    m.Team,
		"Agent":   m.Agent,
		"Chart":   officemcp.Chart(o, m.Address),
		"IsGod":   o.IsGod(m.Address),
		"God":     officemcp.DirectorOf(o),
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

// allowedTools is the --allowedTools value: the office tools, then the
// agent's own tools. Claude Code reads it as a comma-separated list.
func allowedTools(tools []string) string {
	if len(tools) == 0 {
		return officeTools
	}

	return officeTools + "," + strings.Join(tools, ",")
}

// agentSettings is the --settings value: the mail, permission and
// preloaded-skills hooks wired to nobody-hook (docs/hooks.md,
// docs/approvals.md), its statusLine mode, which reports what the session
// spends (ADR-0013), the agent's permission rules (permissions.go), with
// the outside directories it may not write, and the skills it excludes,
// turned off (ADR-0011).
func agentSettings(hookPath string, d catalog.Definition, projectDir string, outside []string) (string, error) {
	run := []map[string]any{{"hooks": []map[string]any{shellCommand(hookPath)}}}

	waiting := shellCommand(hookPath)
	waiting["timeout"] = int(PermissionHookTimeout.Seconds())
	permission := []map[string]any{{"hooks": []map[string]any{waiting}}}

	allow, deny := permissionRules(d.Permissions, projectDir, outside)

	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart":      run,
			"UserPromptSubmit":  run,
			"PostToolUse":       run,
			"Stop":              run,
			"PermissionRequest": permission,
		},
		"permissions": map[string]any{"allow": allow, "deny": deny},
		"statusLine":  shellCommand(hookPath + " statusline"),
	}

	if len(d.SkillsExclude) > 0 {
		off := make(map[string]string, len(d.SkillsExclude))
		for _, name := range d.SkillsExclude {
			off[name] = "off"
		}

		settings["skillOverrides"] = off
	}

	return toJSON(settings)
}

// shellCommand is a hook or statusLine entry of Claude Code's settings that
// runs a command.
func shellCommand(command string) map[string]any {
	return map[string]any{"type": "command", "command": command}
}

// stageMu keeps agents started at the same time from staging the personal
// skills and commands over each other.
var stageMu sync.Mutex

// stagePersonal brings the staging directory in line with the personal
// skills (skills.Stage) and commands (commands.Stage) agents get, before an
// agent starts. It is cheap and idempotent: links already in place are kept.
func (l Launch) stagePersonal(o *org.Org) error {
	if l.SkillsStagingDir == "" {
		return nil
	}

	stageMu.Lock()
	defer stageMu.Unlock()

	if err := skills.Stage(l.SkillsStagingDir, o.Skills.Personal()); err != nil {
		return err
	}

	if l.PersonalCommandsDir == "" {
		return nil
	}

	return commands.Stage(l.SkillsStagingDir, l.PersonalCommandsDir, o.Commands.Personal())
}

// writeMCPConfig writes the agent's MCP configuration and returns its path,
// the --mcp-config value: its office server, plus every server of Claude
// Code's scopes. The file is private: a server of the user's own
// configuration may hold a secret in clear, which must not show on a command
// line (ADR-0010). ${VARIABLE} references are left for Claude Code to expand.
func (l Launch) writeMCPConfig(address string, servers map[string]mcpservers.Server) (string, error) {
	if l.MCPConfigDir == "" {
		return "", fmt.Errorf("agent %s: no directory for its MCP configuration (Launch.MCPConfigDir)", address)
	}

	config, err := agentMCPConfig(l.DaemonURL, address, servers)
	if err != nil {
		return "", err
	}

	path := filepath.Join(l.MCPConfigDir, address+".json")
	if err := atomicfile.WritePrivate(path, []byte(config)); err != nil {
		return "", fmt.Errorf("agent %s: MCP configuration: %w", address, err)
	}

	return path, nil
}

// agentMCPConfig is the content of the agent's MCP configuration.
func agentMCPConfig(daemonURL, address string, servers map[string]mcpservers.Server) (string, error) {
	all := map[string]any{}
	for name, s := range servers {
		all[name] = s.Config
	}

	all[org.OfficeServer] = map[string]string{"type": "http", "url": daemonURL + "/mcp/" + address}

	return toJSON(map[string]any{"mcpServers": all})
}

// disallowedTools is the --disallowedTools value for an agent's exclusions:
// mcp__<server> for a whole server, mcp__<server>__<tool> for one tool. An
// exclusion of a server absent from this machine matches nothing.
func disallowedTools(exclude []string) string {
	rules := make([]string, 0, len(exclude))

	for _, entry := range exclude {
		server, tool, err := catalog.SplitExclusion(entry)
		if err != nil {
			continue // refused when the definition is validated
		}

		rule := "mcp__" + server
		if tool != "" {
			rule += "__" + tool
		}

		rules = append(rules, rule)
	}

	return strings.Join(rules, ",")
}

func toJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode launch config: %w", err)
	}

	return string(data), nil
}
