//go:build contract

package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Costs and budgets (roadmap, ADR-0013) read what an agent spends from Claude
// Code's statusLine: a command Claude Code runs with a JSON description of
// the session on stdin, cost included. These tests pin what Nobody Inc relies
// on, in the conditions of a real agent: an interactive session in a PTY
// driven by keystrokes, with --setting-sources project,local and the
// statusLine declared in --settings, as the hooks are.
//
// Each test uses its own CLAUDE_CONFIG_DIR, where the work directory is
// trusted, so the user's config is neither read nor changed.

// statusLineReport is the part of the statusLine's JSON Nobody Inc reads.
type statusLineReport struct {
	SessionID string `json:"session_id"`
	Cost      struct {
		TotalCostUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
}

// The statusLine runs in a session driven through a PTY, after every turn,
// with the session ID and the session's running cost; the cost of a turn adds
// up to the cost of the previous ones.
func TestStatusLineReportsTheSessionCost(t *testing.T) {
	t.Parallel()

	s := startInteractive(t, newInteractiveEnv(t))

	first := s.turn(t, "Without using any tool, answer only OK.")
	second := s.turn(t, "Without using any tool, answer only OK again.")

	if first.SessionID == "" || first.SessionID != second.SessionID {
		t.Fatalf("session_id: %q after the first turn, %q after the second; want the same, non-empty", first.SessionID, second.SessionID)
	}

	if cost(first) <= 0 {
		t.Fatalf("cost.total_cost_usd after a turn = %v, want > 0", cost(first))
	}

	if cost(second) <= cost(first) {
		t.Fatalf("cost.total_cost_usd: %v after the first turn, %v after the second; want a running total", cost(first), cost(second))
	}

	s.logShape(t)
}

// A resumed session (agents restart with --resume when their definition
// changes) keeps its session ID, but its running cost starts again from 0, as
// a fresh session's does: the daemon must take a lower total as a new start,
// not as a negative spend. An urgent question's fork (claude -p --resume …
// --fork-session, see runtime.Forker) is another process: its -p JSON output
// reports its own total_cost_usd.
func TestStatusLineCostAcrossResumeAndFork(t *testing.T) {
	t.Parallel()

	env := newInteractiveEnv(t)

	original := startInteractive(t, env)
	before := original.turn(t, "Remember: the fruit is MANGO. Without using any tool, answer only OK.")
	original.close()

	if forkCost := runFork(t, env, before.SessionID); forkCost <= 0 {
		t.Fatalf("the fork's total_cost_usd = %v, want > 0", forkCost)
	}

	resumed := startInteractive(t, env, "--resume", before.SessionID)
	atStart := resumed.reportBeforeAnyTurn(t)
	after := resumed.turn(t, "Without using any tool, which fruit did I mention? Answer with the word only.")

	t.Logf("original session %s: %.6f USD; resumed session %s: %v before its first turn, %.6f USD after one turn",
		before.SessionID, cost(before), after.SessionID, costOrNil(atStart), cost(after))

	if after.SessionID != before.SessionID {
		t.Errorf("session_id after --resume = %q, want the original %q", after.SessionID, before.SessionID)
	}

	if atStart == nil || cost(*atStart) != 0 {
		t.Errorf("resumed session, before its first turn: cost %v, want a report with 0", costOrNil(atStart))
	}

	fresh := startInteractive(t, newInteractiveEnv(t))
	if freshStart := fresh.reportBeforeAnyTurn(t); freshStart == nil || cost(*freshStart) != 0 {
		t.Errorf("fresh session, before its first turn: cost %v, want a report with 0", costOrNil(freshStart))
	}
}

// --- helpers ---

// interactiveEnv is a Claude Code configuration directory and a trusted work
// directory, shared by the sessions of one test so that --resume finds the
// transcript.
type interactiveEnv struct {
	configDir string
	workDir   string
}

func newInteractiveEnv(t *testing.T) interactiveEnv {
	t.Helper()

	workDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	configDir := t.TempDir()
	writeClaudeConfig(t, configDir, map[string]bool{workDir: true})

	return interactiveEnv{configDir: configDir, workDir: workDir}
}

func (e interactiveEnv) environ() []string {
	return append(runtime.CleanEnvironment(os.Environ()), "CLAUDE_CONFIG_DIR="+e.configDir)
}

// interactiveSession is claude running in a PTY, as the supervisor runs an
// agent. Its statusLine writes every JSON it receives to a file of
// reportsDir; its Stop hook writes a file to stopsDir at every end of turn.
type interactiveSession struct {
	cmd        *exec.Cmd
	terminal   *os.File
	reportsDir string
	stopsDir   string
	screen     *screenBuffer
	turns      int
}

func startInteractive(t *testing.T, env interactiveEnv, extraArgs ...string) *interactiveSession {
	t.Helper()

	dir := t.TempDir()
	s := &interactiveSession{
		reportsDir: filepath.Join(dir, "reports"),
		stopsDir:   filepath.Join(dir, "stops"),
		screen:     &screenBuffer{},
	}

	for _, d := range []string{s.reportsDir, s.stopsDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	settings := statusLineSettings(t, dir, s.reportsDir, s.stopsDir)
	args := append(slices.Clone(runtime.SettingSources), "--model", "haiku", "--settings", settings)
	args = append(args, extraArgs...)

	s.cmd = exec.CommandContext(context.Background(), "claude", args...)
	s.cmd.Dir = env.workDir
	s.cmd.Env = env.environ()

	terminal, err := pty.StartWithSize(s.cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}

	s.terminal = terminal
	t.Cleanup(s.close)

	go s.screen.drain(terminal)

	s.waitForPrompt(t)

	return s
}

func (s *interactiveSession) close() {
	if s.cmd.Process != nil {
		s.cmd.Process.Kill() //nolint:errcheck // the process may already be gone
		s.cmd.Wait()         //nolint:errcheck // killed on purpose
	}

	s.terminal.Close()
}

// waitForPrompt waits until the TUI shows its prompt.
func (s *interactiveSession) waitForPrompt(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		text := visibleText(s.screen.String())
		if strings.Contains(text, "❯") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("claude did not show its prompt:\n%s", visibleText(s.screen.String()))
}

// turn types a prompt the way the supervisor does (runtime.typePrompt), waits
// for the end of the turn, then for the statusLine's last report.
func (s *interactiveSession) turn(t *testing.T, prompt string) statusLineReport {
	t.Helper()

	before := len(s.reports(t))

	if _, err := s.terminal.WriteString(prompt); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)

	if _, err := s.terminal.WriteString("\r"); err != nil {
		t.Fatal(err)
	}

	s.turns++
	s.waitForStops(t, s.turns)

	// The statusLine is debounced (300 ms) and runs after the message: give
	// it time to report the end of the turn.
	time.Sleep(3 * time.Second)

	reports := s.reports(t)
	if len(reports) <= before {
		t.Fatalf("turn %d: the statusLine did not run (%d report(s) before, %d after)", s.turns, before, len(reports))
	}

	t.Logf("turn %d: %d statusLine report(s), last cost %v", s.turns, len(reports)-before, costOrNil(&reports[len(reports)-1]))

	return reports[len(reports)-1]
}

// reportBeforeAnyTurn returns the statusLine's last report once the session
// is ready, before any prompt, or nil if there was none.
func (s *interactiveSession) reportBeforeAnyTurn(t *testing.T) *statusLineReport {
	t.Helper()

	time.Sleep(3 * time.Second)

	reports := s.reports(t)
	if len(reports) == 0 {
		return nil
	}

	return &reports[len(reports)-1]
}

func (s *interactiveSession) waitForStops(t *testing.T, n int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(s.stopsDir)
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) >= n {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("turn %d did not end:\n%s", n, visibleText(s.screen.String()))
}

// reports returns the statusLine's reports, oldest first. The files are
// named by mktemp, so their order comes from their modification time.
func (s *interactiveSession) reports(t *testing.T) []statusLineReport {
	t.Helper()

	entries, err := os.ReadDir(s.reportsDir)
	if err != nil {
		t.Fatal(err)
	}

	type stamped struct {
		at     time.Time
		report statusLineReport
	}

	var all []stamped

	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(s.reportsDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}

		var r statusLineReport
		if err := json.Unmarshal(data, &r); err != nil {
			continue // written while we read: it will be complete next time
		}

		all = append(all, stamped{at: info.ModTime(), report: r})
	}

	slices.SortStableFunc(all, func(a, b stamped) int { return a.at.Compare(b.at) })

	out := make([]statusLineReport, len(all))
	for i, s := range all {
		out[i] = s.report
	}

	return out
}

// logShape logs the top-level fields of the last report, and its cost object.
func (s *interactiveSession) logShape(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(s.reportsDir)
	if err != nil || len(entries) == 0 {
		return
	}

	data, err := os.ReadFile(filepath.Join(s.reportsDir, entries[len(entries)-1].Name()))
	if err != nil {
		return
	}

	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}

	slices.Sort(keys)
	t.Logf("statusLine JSON fields: %v", keys)
	t.Logf("statusLine cost: %s", fields["cost"])
}

// statusLineSettings is a --settings value with a statusLine that saves each
// JSON it gets to its own file of reportsDir, and a Stop hook that drops a
// file into stopsDir.
func statusLineSettings(t *testing.T, dir, reportsDir, stopsDir string) string {
	t.Helper()

	statusLine := filepath.Join(dir, "statusline.sh")
	stop := filepath.Join(dir, "stop.sh")

	scripts := map[string]string{
		statusLine: "#!/bin/sh\ncat > \"$(mktemp '" + reportsDir + "/r.XXXXXX')\"\nprintf 'probe'\n",
		stop:       "#!/bin/sh\ncat > \"$(mktemp '" + stopsDir + "/s.XXXXXX')\"\n",
	}

	for path, script := range scripts {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // test script
			t.Fatal(err)
		}
	}

	data, err := json.Marshal(map[string]any{
		"statusLine": map[string]any{"type": "command", "command": statusLine},
		"hooks": map[string]any{
			"Stop": []map[string]any{{"hooks": []map[string]string{{"type": "command", "command": stop}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// runFork asks a question to a fork of the session, as runtime.Forker does,
// and returns the total_cost_usd of its -p JSON output.
func runFork(t *testing.T, env interactiveEnv, sessionID string) float64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	args := append([]string{
		"-p", "Without using any tool, what is the codeword? Answer with the word only.",
		"--resume", sessionID, "--fork-session", "--tools", "",
		"--strict-mcp-config", "--output-format", "json", "--model", "haiku",
	}, runtime.SettingSources...)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = env.workDir
	cmd.Env = env.environ()

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fork: %v\n%s", err, out)
	}

	var result struct {
		Result       string   `json:"result"`
		SessionID    string   `json:"session_id"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode fork output: %v\n%s", err, out)
	}

	if result.TotalCostUSD == nil {
		t.Fatalf("the fork's -p output has no total_cost_usd:\n%s", out)
	}

	t.Logf("fork %s answered %q for %.6f USD", result.SessionID, result.Result, *result.TotalCostUSD)

	return *result.TotalCostUSD
}

func cost(r statusLineReport) float64 {
	if r.Cost.TotalCostUSD == nil {
		return -1
	}

	return *r.Cost.TotalCostUSD
}

func costOrNil(r *statusLineReport) any {
	if r == nil || r.Cost.TotalCostUSD == nil {
		return nil
	}

	return fmt.Sprintf("%.6f", *r.Cost.TotalCostUSD)
}

// screenBuffer keeps everything the TUI drew; reading the PTY continuously
// also keeps claude from blocking on a full terminal.
type screenBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *screenBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func (b *screenBuffer) drain(terminal *os.File) {
	chunk := make([]byte, 64*1024)

	for {
		n, err := terminal.Read(chunk)

		b.mu.Lock()
		b.buf.Write(chunk[:n])
		b.mu.Unlock()

		if err != nil {
			return
		}
	}
}
