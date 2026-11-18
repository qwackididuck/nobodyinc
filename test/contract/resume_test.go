//go:build contract

package contract

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// When a project opens again, its agents resume their sessions without a
// prompt (ADR-0015, "silent resume"): nothing is paid until an agent has
// something to do. These tests pin what the daemon relies on, with an
// interactive session in a PTY as the supervisor runs it.
//
// Checked on 2026-11-18 with Claude Code 2.1.327:
//   - claude --resume <session> without a prompt runs SessionStart (source
//     resume, the same session ID), then waits at its prompt: no turn, no
//     cost;
//   - the additionalContext SessionStart returns reaches the next prompt
//     typed in the terminal, and the session keeps its memory;
//   - a prompt typed as soon as SessionStart has run, before the TUI draws
//     its prompt, is taken (3 runs out of 3);
//   - resuming a session Claude Code does not have prints "No conversation
//     found with session ID: …" and exits with status 1 at once, without
//     running SessionStart: the supervisor takes an agent that exits before
//     its first hook event, while resuming, as a lost session and starts it
//     afresh.

// sessionStartEvent is the part of the SessionStart hook's input the daemon
// reads.
type sessionStartEvent struct {
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
	Cwd       string `json:"cwd"`
}

// stopEvent is the part of the Stop hook's input these tests read.
type stopEvent struct {
	TranscriptPath       string `json:"transcript_path"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// An interactive claude --resume <session> without a prompt runs the
// SessionStart hook (source resume, same session ID) and then waits at its
// prompt without running a turn; the hook's additionalContext reaches the
// next prompt typed in the terminal, and the session keeps its memory.
func TestSilentResume(t *testing.T) {
	t.Parallel()

	env := newInteractiveEnv(t)

	original := startInteractive(t, env)
	first := original.turn(t, "Remember: the fruit codeword is MANGO. Without using any tool, answer only OK.")
	original.close()

	withModelRetry(t, func() string {
		resumed := startInteractiveWithContext(t, env, "The secret codeword is TOUCAN.", "--resume", first.SessionID)
		defer resumed.close()

		starts := resumed.waitForStarts(t, 1)
		if starts[0].Source != "resume" || starts[0].SessionID != first.SessionID {
			t.Fatalf("SessionStart of a silent resume = %+v, want source resume and session %s", starts[0], first.SessionID)
		}

		// Give a turn time to start, if Claude Code ran one on its own.
		time.Sleep(5 * time.Second)

		if n := resumed.stops(t); n != 0 {
			t.Fatalf("a resume without a prompt ran %d turn(s) on its own", n)
		}

		resumed.turn(t, "Without using any tool, give the fruit and secret codewords, as fruit=<word or NONE> secret=<word or NONE>.")

		answer := strings.ToUpper(resumed.lastAnswer(t))
		if !strings.Contains(answer, "MANGO") || !strings.Contains(answer, "TOUCAN") {
			return "want both MANGO (memory) and TOUCAN (SessionStart context), got " + answer
		}

		return ""
	})
}

// A prompt typed as soon as SessionStart has run, without waiting for the
// TUI to draw its prompt, is taken: a silently resumed agent is idle from
// its SessionStart on, and mail arriving then wakes it up at once.
func TestPromptTypedRightAfterSessionStart(t *testing.T) {
	t.Parallel()

	env := newInteractiveEnv(t)

	original := startInteractive(t, env)
	first := original.turn(t, "Remember: the fruit codeword is MANGO. Without using any tool, answer only OK.")
	original.close()

	withModelRetry(t, func() string {
		resumed := launchInteractive(t, env, "", "--resume", first.SessionID)
		defer resumed.close()

		resumed.waitForStarts(t, 1)
		resumed.turn(t, "Without using any tool, which fruit codeword did I give? Answer with the word only.")

		if answer := strings.ToUpper(resumed.lastAnswer(t)); !strings.Contains(answer, "MANGO") {
			return "want MANGO, got " + answer
		}

		return ""
	})
}

// Resuming a session Claude Code does not have makes the CLI exit at once
// with an error, before any hook: how the supervisor detects a lost session
// and falls back to a fresh one. No model is called.
func TestResumeOfAnUnknownSession(t *testing.T) {
	t.Parallel()

	const unknown = "00000000-0000-4000-8000-000000000000"

	s := launchInteractive(t, newInteractiveEnv(t), "", "--resume", unknown)

	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()

	select {
	case err := <-exited:
		if err == nil {
			t.Errorf("claude exited successfully after resuming an unknown session, want an error status")
		}

		t.Logf("claude exited: %v\n%s", err, visibleText(s.screen.String()))
	case <-time.After(30 * time.Second):
		t.Fatalf("claude still runs 30 s after resuming an unknown session; SessionStart events: %+v\n%s",
			s.starts(t), visibleText(s.screen.String()))
	}

	if starts := s.starts(t); len(starts) != 0 {
		t.Errorf("SessionStart ran for an unknown session: %+v", starts)
	}
}

// --- helpers ---

// starts returns the SessionStart events received so far, oldest first.
func (s *interactiveSession) starts(t *testing.T) []sessionStartEvent {
	t.Helper()

	var out []sessionStartEvent

	for _, data := range readHookInputs(t, s.startsDir) {
		var ev sessionStartEvent
		if json.Unmarshal(data, &ev) == nil {
			out = append(out, ev)
		}
	}

	return out
}

func (s *interactiveSession) waitForStarts(t *testing.T, n int) []sessionStartEvent {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if starts := s.starts(t); len(starts) >= n {
			return starts
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("SessionStart did not run %d time(s):\n%s", n, visibleText(s.screen.String()))

	return nil
}

// stops returns how many turns ended so far.
func (s *interactiveSession) stops(t *testing.T) int {
	t.Helper()

	return len(readHookInputs(t, s.stopsDir))
}

// lastAnswer returns the text of the last turn's answer: from the Stop
// hook's input, or else from the end of the transcript it points to.
func (s *interactiveSession) lastAnswer(t *testing.T) string {
	t.Helper()

	inputs := readHookInputs(t, s.stopsDir)
	if len(inputs) == 0 {
		t.Fatal("no turn ended")
	}

	var ev stopEvent
	if err := json.Unmarshal(inputs[len(inputs)-1], &ev); err != nil {
		t.Fatal(err)
	}

	if ev.LastAssistantMessage != "" {
		return ev.LastAssistantMessage
	}

	return lastAssistantText(t, ev.TranscriptPath)
}

// lastAssistantText returns the text of the last assistant message of a
// transcript.
func lastAssistantText(t *testing.T, path string) string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var last string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)

	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.Type != "assistant" {
			continue
		}

		for _, c := range entry.Message.Content {
			if c.Type == "text" {
				last = c.Text
			}
		}
	}

	return last
}

// readHookInputs returns the contents of the files a hook script wrote into
// dir, oldest first.
func readHookInputs(t *testing.T, dir string) [][]byte {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	type stamped struct {
		at   time.Time
		data []byte
	}

	var all []stamped

	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !json.Valid(data) {
			continue // being written: complete next time
		}

		all = append(all, stamped{at: info.ModTime(), data: data})
	}

	slices.SortStableFunc(all, func(a, b stamped) int { return a.at.Compare(b.at) })

	out := make([][]byte, len(all))
	for i, s := range all {
		out[i] = s.data
	}

	return out
}
