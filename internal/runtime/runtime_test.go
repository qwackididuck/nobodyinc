package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

func testOrg() *org.Org {
	return &org.Org{
		ProjectDir: "/tmp/project",
		Teams: []org.Team{
			{ID: "specs", Goal: "Write the specs", Agents: []org.Agent{{Name: "archi", Role: "Architect", Prompt: "Design things.", Model: "sonnet"}}},
			{ID: "dev", Goal: "Build it", Agents: []org.Agent{{Name: "dev1", Role: "Developer"}}},
		},
		Channels: []org.Channel{{From: "dev", To: "specs"}},
	}
}

func TestSystemPrompt(t *testing.T) {
	t.Parallel()

	o := testOrg()
	m, _ := o.Member("dev1@dev")

	got, err := SystemPrompt(o, m, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"dev1@dev", "Build it", "Developer", "archi@specs (Architect)", "send_message", "legitimate",
		"### Team dev (your team)", "you can start conversations with them", "call `list_contacts`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt misses %q:\n%s", want, got)
		}
	}
}

func TestLaunchCommand(t *testing.T) {
	t.Parallel()

	o := testOrg()
	m, _ := o.Member("archi@specs")
	l := Launch{
		ClaudePath: "/bin/claude", HookPath: "/opt/nobody-hook", DaemonURL: "http://127.0.0.1:7420",
		WorkDir: func(string) string { return o.ProjectDir },
	}

	cmd, err := l.Command(context.Background(), o, m, "")
	if err != nil {
		t.Fatal(err)
	}

	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"/bin/claude", "--setting-sources project,local", "--append-system-prompt", "--model sonnet",
		`"command":"/opt/nobody-hook"`, `"UserPromptSubmit"`, `"PostToolUse"`, `"Stop"`, `"PermissionRequest"`, `"timeout":600`,
		`"url":"http://127.0.0.1:7420/mcp/archi@specs"`, officeTools, onboardingPrompt,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("command misses %q:\n%s", want, args)
		}
	}

	if strings.Contains(args, "--bare") {
		t.Fatal("--bare disables hooks and must never be used")
	}

	if cmd.Dir != "/tmp/project" {
		t.Fatalf("dir = %s", cmd.Dir)
	}

	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "NOBODY_AGENT=archi@specs") || !strings.Contains(env, "NOBODY_DAEMON_URL=http://127.0.0.1:7420") ||
		!strings.Contains(env, "GIT_AUTHOR_NAME=archi@specs (Nobody Inc)") || !strings.Contains(env, "GIT_COMMITTER_EMAIL=archi.specs@nobody.local") {
		t.Fatal("agent environment is missing")
	}
}

func TestAgentEnvironmentDropsParentSession(t *testing.T) {
	t.Parallel()

	base := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_AUTH_TOKEN=secret",
		"CLAUDE_CODE_ENABLE_1M_CONTEXT=1", // user configuration: kept
		"CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=abc",
		"CLAUDE_CODE_MESSAGING_TOKEN=parent-token",
		"CLAUDE_CODE_CHILD_SESSION=1",
	}
	env := strings.Join(agentEnvironment(base, "archi@specs", "http://d"), "\n")

	for _, kept := range []string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=secret", "CLAUDE_CODE_ENABLE_1M_CONTEXT=1", "NOBODY_AGENT=archi@specs", "NOBODY_DAEMON_URL=http://d"} {
		if !strings.Contains(env, kept) {
			t.Fatalf("%s should be kept:\n%s", kept, env)
		}
	}

	for _, dropped := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_CHILD_SESSION"} {
		if strings.Contains(env, dropped+"=") {
			t.Fatalf("%s must not reach agents:\n%s", dropped, env)
		}
	}
}

func TestOutputBacklogAndViewers(t *testing.T) {
	t.Parallel()

	out := NewOutput()
	out.Write([]byte("hello ")) //nolint:errcheck // Output.Write never fails

	backlog, updates, cancel := out.Subscribe()
	defer cancel()

	if string(backlog) != "hello " {
		t.Fatalf("backlog = %q", backlog)
	}

	out.Write([]byte("world")) //nolint:errcheck // Output.Write never fails

	if got := string(<-updates); got != "world" {
		t.Fatalf("update = %q", got)
	}
}

// newCatSupervisor runs `cat` instead of claude: whatever is typed is echoed
// back by the terminal, which is enough to test the plumbing.
func newCatSupervisor(t *testing.T) *Supervisor {
	t.Helper()

	s := &Supervisor{
		Org:      testOrg(),
		Statuses: NewStatuses(),
		Logger:   slog.New(slog.DiscardHandler),
		NewCommand: func(ctx context.Context, _ *org.Org, _ org.Member, _ string) (*exec.Cmd, error) {
			return exec.CommandContext(ctx, "cat"), nil
		},
		HasMail: func(context.Context, string) (bool, error) { return true, nil },
	}
	t.Cleanup(s.StopAll)

	return s
}

func TestSupervisorInputAndOutput(t *testing.T) {
	t.Parallel()

	s := newCatSupervisor(t)
	if err := s.Start(context.Background(), "archi@specs"); err != nil {
		t.Fatal(err)
	}

	if got := s.Statuses.Get("archi@specs"); got != StatusStarting {
		t.Fatalf("status = %s, want starting", got)
	}

	out, ok := s.Output("archi@specs")
	if !ok {
		t.Fatal("no output for a running agent")
	}

	_, updates, cancel := out.Subscribe()
	defer cancel()

	if err := s.Input("archi@specs", []byte("ping\r")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, updates, "ping")

	if err := s.Input("ghost@dev", []byte("x")); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("input to a stopped agent: err = %v", err)
	}
}

func TestRestartAll(t *testing.T) {
	t.Parallel()

	s := newCatSupervisor(t)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	before, _ := s.Output("archi@specs")

	if err := s.RestartAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	after, ok := s.Output("archi@specs")
	if !ok || after == before {
		t.Fatal("RestartAll should run a new process for every agent")
	}

	if err := s.Stop("ghost@dev"); err != nil {
		t.Fatalf("stopping an agent that is not running: %v", err)
	}
}

func TestIdleAgentWithMailIsWokenUp(t *testing.T) {
	t.Parallel()

	s := newCatSupervisor(t)
	if err := s.Start(context.Background(), "archi@specs"); err != nil {
		t.Fatal(err)
	}

	out, _ := s.Output("archi@specs")

	_, updates, cancel := out.Subscribe()
	defer cancel()

	s.Statuses.OnIdle = s.OnIdle
	s.Statuses.Set("archi@specs", StatusIdle) // HasMail says yes: the agent must be woken up

	waitFor(t, updates, "You have new mail")

	if got := s.Statuses.Get("archi@specs"); got != StatusWorking {
		t.Fatalf("status after wake = %s, want working", got)
	}
}

func TestBusyAgentIsNotWokenUp(t *testing.T) {
	t.Parallel()

	s := newCatSupervisor(t)
	if err := s.Start(context.Background(), "archi@specs"); err != nil {
		t.Fatal(err)
	}

	out, _ := s.Output("archi@specs")

	_, updates, cancel := out.Subscribe()
	defer cancel()

	s.Statuses.Set("archi@specs", StatusWorking)
	s.NotifyMail("archi@specs")

	select {
	case chunk := <-updates:
		t.Fatalf("a busy agent must not be disturbed, got %q typed", chunk)
	case <-time.After(enterDelay + 200*time.Millisecond):
	}
}

func TestTerminalWebSocket(t *testing.T) {
	t.Parallel()

	s := newCatSupervisor(t)
	if err := s.Start(context.Background(), "archi@specs"); err != nil {
		t.Fatal(err)
	}

	mux := newTerminalMux(s)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/terminal/archi@specs", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow() //nolint:errcheck // test cleanup

	input, err := json.Marshal(control{Type: "input", Data: "hello-ws\r"})
	if err != nil {
		t.Fatal(err)
	}

	if err := conn.Write(ctx, websocket.MessageText, input); err != nil {
		t.Fatal(err)
	}

	var seen strings.Builder
	for !strings.Contains(seen.String(), "hello-ws") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read terminal: %v (seen %q)", err, seen.String())
		}

		seen.Write(data)
	}
}

func waitFor(t *testing.T, updates <-chan []byte, want string) {
	t.Helper()

	timeout := time.After(5 * time.Second)

	var seen strings.Builder
	for !strings.Contains(seen.String(), want) {
		select {
		case chunk := <-updates:
			seen.Write(chunk)
		case <-timeout:
			t.Fatalf("did not see %q in terminal output, got %q", want, seen.String())
		}
	}
}
