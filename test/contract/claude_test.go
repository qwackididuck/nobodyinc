//go:build contract

// Package contract checks, against the real Claude Code CLI, the behaviors
// Nobody Inc relies on. If one of these tests breaks after a Claude Code
// upgrade, the matching mechanism in Nobody Inc must be revisited.
//
// They call the real API (Haiku, a few cents per run), so they only run with:
//
//	task contract
package contract

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/qwackididuck/nobodyinc/internal/approval"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/server"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// testAgent is the address the claude sessions under test play.
const testAgent = "tester@contract"

// hookBinary is the nobody-hook binary built once for the whole suite.
var hookBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "nobody-contract-*")
	if err != nil {
		panic(err)
	}

	hookBinary = filepath.Join(dir, "nobody-hook")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", hookBinary, "github.com/qwackididuck/nobodyinc/cmd/nobody-hook")

	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build nobody-hook: " + err.Error())
	}

	code := m.Run()

	os.RemoveAll(dir)
	os.Exit(code)
}

// The Stop hook can refuse the end of a turn and hand the agent its mail:
// the agent must act on it before stopping.
func TestStopHookDeliversMail(t *testing.T) {
	t.Parallel()

	withModelRetry(t, func() string {
		d := startDaemon(t)
		d.post(t, messaging.Normal, "Reply to this message by writing the exact word PINEAPPLE.")

		result := runClaude(t, claudeRun{
			prompt: "Say hello in one short sentence.",
			hooks:  []string{"Stop"},
			daemon: d,
		})

		d.assertTrayEmpty(t, messaging.Normal)

		if !strings.Contains(result.Result, "PINEAPPLE") {
			return fmt.Sprintf("agent did not act on its mail, final answer: %q", result.Result)
		}

		return ""
	})
}

// The UserPromptSubmit hook can attach mail to the prompt that starts a turn:
// this is how an idle agent woken up by the daemon reads its mail in the same
// turn. Only this hook is configured, so the mail can only come from it.
func TestUserPromptSubmitAttachesMail(t *testing.T) {
	t.Parallel()

	withModelRetry(t, func() string {
		d := startDaemon(t)
		d.post(t, messaging.Normal, "Include the exact word KIWI in your answer.")

		result := runClaude(t, claudeRun{
			prompt: "You have new mail: it is attached to this message. Read it and act on it.",
			hooks:  []string{"UserPromptSubmit"},
			daemon: d,
		})

		d.assertTrayEmpty(t, messaging.Normal)

		if !strings.Contains(result.Result, "KIWI") {
			return fmt.Sprintf("mail attached to the prompt was not taken into account, final answer: %q", result.Result)
		}

		return ""
	})
}

// The PostToolUse hook can inject urgent mail in the middle of a turn,
// without stopping the agent. Only this hook is configured, so the mail can
// only have reached the agent mid-turn.
func TestPostToolUseInjectsUrgentMail(t *testing.T) {
	t.Parallel()

	withModelRetry(t, func() string {
		d := startDaemon(t)
		d.post(t, messaging.Urgent, "Include the exact word MANGO in your final answer.")

		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "note.txt"), []byte("The sky is green here.\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		result := runClaude(t, claudeRun{
			prompt:    "Read the file note.txt with the Read tool and tell me in one sentence what it says.",
			hooks:     []string{"PostToolUse"},
			daemon:    d,
			workDir:   workDir,
			extraArgs: []string{"--allowedTools", "Read"},
		})

		d.assertTrayEmpty(t, messaging.Urgent)

		if !strings.Contains(result.Result, "MANGO") {
			return fmt.Sprintf("urgent mail was not taken into account, final answer: %q", result.Result)
		}

		return ""
	})
}

// The PermissionRequest hook lets the human allow or deny a tool call from
// the approval queue. The daemon under test answers the first request on its
// own, as the human would from the UI.
func TestPermissionRequestUsesTheApprovalQueue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		allow       bool
		wantCreated bool
	}{
		{"allowed: the command runs", true, true},
		{"denied: the command does not run", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := startDaemon(t)

			decided := make(chan bool, 1)
			go func() { decided <- d.decideFirstApproval(tt.allow) }()

			// Creating a file needs a permission; read-only commands such as
			// echo are allowed by Claude Code without asking anyone.
			workDir := t.TempDir()
			runClaude(t, claudeRun{
				prompt:  "Run this exact shell command with the Bash tool: touch approved.txt. If you cannot, just say so.",
				hooks:   []string{"PermissionRequest"},
				daemon:  d,
				workDir: workDir,
			})

			if !<-decided {
				t.Fatal("the permission request never reached the approval queue")
			}

			_, err := os.Stat(filepath.Join(workDir, "approved.txt"))
			if created := err == nil; created != tt.wantCreated {
				t.Fatalf("file created = %v, want %v", created, tt.wantCreated)
			}
		})
	}
}

// A session can be forked to answer a quick question from its context,
// without tools, and without touching the original session.
func TestForkSessionAnswersWithoutTouchingOriginal(t *testing.T) {
	t.Parallel()

	original := runClaude(t, claudeRun{
		prompt: "Remember this fact for later: our database is PostgreSQL 16. Answer only OK.",
	})

	const question = "Which database do we use? Answer in a few words."

	withModelRetry(t, func() string {
		fork := runClaude(t, claudeRun{
			prompt:    question,
			extraArgs: []string{"--resume", original.SessionID, "--fork-session", "--tools", ""},
		})
		if fork.SessionID == original.SessionID {
			t.Fatal("fork reused the original session id")
		}

		if !strings.Contains(fork.Result, "PostgreSQL") {
			return fmt.Sprintf("fork did not answer from the original context: %q", fork.Result)
		}

		return ""
	})

	// The transcript format is internal to Claude Code; it is only read here,
	// in a test, to prove the original session did not receive the question.
	transcript := findTranscript(t, original.SessionID)

	data, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(data, []byte(question)) {
		t.Fatal("the forked question leaked into the original session")
	}
}

// --strict-mcp-config limits an agent to the MCP servers we give it.
func TestStrictMCPConfigOnlyLoadsGivenServers(t *testing.T) {
	t.Parallel()

	config := `{"mcpServers":{"only-this-one":{"command":"false"}}}`

	init := claudeInit(t, "--strict-mcp-config", "--mcp-config", config)

	var names []string
	for _, s := range init.MCPServers {
		names = append(names, s.Name)
	}

	if len(names) != 1 || names[0] != "only-this-one" {
		t.Fatalf("MCP servers loaded = %v, want only [only-this-one]", names)
	}
}

// Claude Code runs interactively inside a pseudo-terminal started from Go.
func TestClaudeStartsInPTY(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--model", "haiku", "--setting-sources", "local")

	cmd.Dir = t.TempDir()

	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill() //nolint:errcheck // the process may already be gone
		terminal.Close()
	}()

	output := make(chan int, 1)
	go func() {
		buf := make([]byte, 4096)

		n, _ := terminal.Read(buf) // any screen output proves the TUI started
		output <- n
	}()

	select {
	case n := <-output:
		if n == 0 {
			t.Fatal("claude produced no output in the PTY")
		}
	case <-ctx.Done():
		t.Fatal("claude did not draw anything in the PTY within 30s")
	}
}

// --- helpers ---

// withModelRetry runs a check whose outcome depends on what the model chose to
// write. Deterministic assertions (was the mail delivered?) fail immediately
// inside check; the model's reaction gets one more attempt, because a small
// model occasionally ignores an instruction. check returns "" on success.
func withModelRetry(t *testing.T, check func() string) {
	t.Helper()

	const attempts = 2

	var failure string
	for i := 1; i <= attempts; i++ {
		if failure = check(); failure == "" {
			return
		}

		t.Logf("attempt %d/%d: %s", i, attempts, failure)
	}

	t.Fatal(failure)
}

type claudeRun struct {
	prompt    string
	hooks     []string // hook events wired to nobody-hook
	daemon    *daemon  // daemon nobody-hook talks to
	workDir   string
	extraArgs []string
}

type claudeResult struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
}

// runClaude runs one headless turn. Only "local" settings are loaded, and the
// work dir is a fresh temp dir, so the user's own hooks and plugins stay out.
func runClaude(t *testing.T, r claudeRun) claudeResult {
	t.Helper()

	args := []string{"-p", r.prompt, "--model", "haiku", "--output-format", "json", "--setting-sources", "local"}
	if len(r.hooks) > 0 {
		args = append(args, "--settings", hookSettings(t, r.hooks))
	}

	args = append(args, r.extraArgs...)

	var env []string
	if r.daemon != nil {
		env = []string{"NOBODY_AGENT=" + testAgent, "NOBODY_DAEMON_URL=" + r.daemon.url}
	}

	out := runCLI(t, r.workDir, env, args)

	var res claudeResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode claude output: %v\n%s", err, out)
	}

	if res.IsError {
		t.Fatalf("claude reported an error: %s", out)
	}

	return res
}

type initEvent struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	MCPServers []struct {
		Name string `json:"name"`
	} `json:"mcp_servers"`
}

// claudeInit returns the "init" event Claude Code emits at the start of a
// stream-json session, which lists the tools and MCP servers it loaded.
func claudeInit(t *testing.T, extraArgs ...string) initEvent {
	t.Helper()

	args := append([]string{"-p", "Answer OK.", "--model", "haiku", "--output-format", "stream-json", "--verbose", "--setting-sources", "local"}, extraArgs...)
	out := runCLI(t, "", nil, args)

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var ev initEvent
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Type == "system" && ev.Subtype == "init" {
			return ev
		}
	}

	t.Fatalf("no init event in claude output:\n%s", out)

	return initEvent{}
}

func runCLI(t *testing.T, workDir string, env, args []string) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", args...)

	cmd.Dir = workDir
	if cmd.Dir == "" {
		cmd.Dir = t.TempDir()
	}

	cmd.Env = append(os.Environ(), env...)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("claude %v: %v\nstderr: %s\nstdout: %s", args, err, stderr.String(), out)
	}

	return out
}

// hookSettings builds the --settings JSON wiring the given events to nobody-hook.
func hookSettings(t *testing.T, events []string) string {
	t.Helper()

	command := []map[string]any{{"hooks": []map[string]string{{"type": "command", "command": hookBinary}}}}

	hooks := map[string]any{}
	for _, ev := range events {
		hooks[ev] = command
	}

	data, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// daemon is a real Nobody Inc daemon running in-process, with an in-memory
// database and an organization made of the agent under test.
type daemon struct {
	url       string
	store     *store.Store
	messages  *messaging.Service
	approvals *approval.Queue
}

func startDaemon(t *testing.T) *daemon {
	t.Helper()

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	o := &org.Org{Teams: []org.Team{{ID: "contract", Agents: []org.Agent{{Name: "tester"}}}}}
	messages := &messaging.Service{Store: st, Policy: messaging.Policy{Org: o}}
	approvals := approval.NewQueue()
	srv := httptest.NewServer(server.New(server.Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Org:       o,
		Store:     st,
		Messages:  messages,
		Statuses:  runtime.NewStatuses(),
		Approvals: approvals,
	}))
	t.Cleanup(srv.Close)

	return &daemon{url: srv.URL, store: st, messages: messages, approvals: approvals}
}

// post sends a message from the human to the agent under test.
func (d *daemon) post(t *testing.T, p messaging.Priority, body string) {
	t.Helper()

	_, err := d.messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: testAgent, Act: messaging.ActRequest, Priority: p,
		Subject: "contract test", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// decideFirstApproval waits for a permission request and answers it, as the
// human would from the UI. It reports whether a request was seen.
func (d *daemon) decideFirstApproval(allow bool) bool {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if pending := d.approvals.Pending(); len(pending) > 0 {
			return d.approvals.Decide(pending[0].ID, approval.Decision{Allow: allow, Message: "decided by the contract test"}) == nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return false
}

func (d *daemon) assertTrayEmpty(t *testing.T, p messaging.Priority) {
	t.Helper()

	pending, err := d.store.PendingMessages(context.Background(), testAgent, p)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 0 {
		t.Fatalf("%s tray still has %d message(s): the hook did not deliver", p, len(pending))
	}
}

func findTranscript(t *testing.T, sessionID string) string {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("transcript for session %s not found (matches: %v, err: %v)", sessionID, matches, err)
	}

	return matches[0]
}
