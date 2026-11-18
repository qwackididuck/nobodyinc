package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

// Resuming the agents' sessions when a project opens again (ADR-0015): a
// first daemon runs the project and stops, a second one opens the same
// project and database. fakeclaude keeps its sessions on disk, and answers
// "MEMORY?" with what its session remembers.

func TestReopenedProjectResumesSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// before runs on the first daemon, which then stops.
		before func(t *testing.T, d *daemon.Daemon)
		// between runs while the project is closed, with dev1's session file.
		between func(t *testing.T, dev1Session string)
		// wantMemory and wantNotMemory are in dev1's memory, or not, after
		// the reopening; wantStart is in its answer to "PRELOADED SKILLS?".
		wantMemory    []string
		wantNotMemory []string
		wantStart     []string
	}{
		{
			name: "an idle agent resumes silently, with the current organization",
			before: func(t *testing.T, d *daemon.Daemon) {
				t.Helper()
				sendAsHuman(t, d, "dev1@dev", "Remember", "The fruit is MANGO.")
				eventually(t, "dev1 got the message", func() bool { return memoryHas(t, d, "MANGO") })
				eventually(t, "dev1 idle again", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusIdle })
			},
			wantMemory: []string{"The fruit is MANGO."},
			// No turn ran at the reopening: neither the change prompt nor the
			// carry-on prompt.
			wantNotMemory: []string{"Nobody Inc restarted your session", "carry on with what you were doing"},
			wantStart:     []string{"source=resume", "Nobody Inc reopened the project", "<current_organization>"},
		},
		{
			name: "an agent stopped mid-turn resumes with a prompt to carry on",
			before: func(t *testing.T, d *daemon.Daemon) {
				t.Helper()
				sendAsHuman(t, d, "dev1@dev", "Long task", "WORK 60")
				// Working on it: the message reached its session.
				eventually(t, "dev1 got the message", func() bool { return memoryHas(t, d, "WORK 60") })
			},
			wantMemory: []string{"WORK 60", "carry on with what you were doing"},
			wantStart:  []string{"source=resume", "Nobody Inc reopened the project"},
		},
		{
			name: "a lost session starts afresh",
			before: func(t *testing.T, d *daemon.Daemon) {
				t.Helper()
				sendAsHuman(t, d, "dev1@dev", "Remember", "The fruit is MANGO.")
				eventually(t, "dev1 got the message", func() bool { return memoryHas(t, d, "MANGO") })
			},
			between: func(t *testing.T, dev1Session string) {
				t.Helper()
				// Claude Code no longer has the session.
				if err := os.Remove(dev1Session); err != nil {
					t.Fatal(err)
				}
			},
			wantNotMemory: []string{"MANGO"},
			wantStart:     []string{"source=startup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := testOrg(t)
			db := filepath.Join(t.TempDir(), "nobody.db")

			first := startDaemonWithDB(t, o, db)
			tt.before(t, first)
			dev1Session := sessionFile(t, first, "dev1@dev")

			if err := first.Close(); err != nil {
				t.Fatal(err)
			}

			if tt.between != nil {
				tt.between(t, dev1Session)
			}

			second := startDaemonWithDB(t, o, db)

			memory := ask(t, second, "dev1@dev", "MEMORY?")
			for _, want := range tt.wantMemory {
				if !strings.Contains(memory, want) {
					t.Errorf("dev1's memory lacks %q:\n%s", want, memory)
				}
			}

			for _, unwanted := range tt.wantNotMemory {
				if strings.Contains(memory, unwanted) {
					t.Errorf("dev1's memory has %q:\n%s", unwanted, memory)
				}
			}

			start := ask(t, second, "dev1@dev", "PRELOADED SKILLS?")
			for _, want := range tt.wantStart {
				if !strings.Contains(start, want) {
					t.Errorf("dev1's session start lacks %q:\n%s", want, start)
				}
			}
		})
	}
}

// ask sends a query from the human to an agent and returns its answer.
func ask(t *testing.T, d *daemon.Daemon, to, question string) string {
	t.Helper()

	q, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: to, Act: messaging.ActQuery, Priority: messaging.Normal,
		Subject: question, Body: question,
	})
	if err != nil {
		t.Fatal(err)
	}

	answer := waitForAnswer(t, d, q.ID).Body

	eventually(t, to+" idle again", func() bool { return d.Statuses.Get(to) == runtime.StatusIdle })

	return answer
}

// memoryHas reports whether dev1's recorded session file mentions text.
func memoryHas(t *testing.T, d *daemon.Daemon, text string) bool {
	t.Helper()

	data, err := os.ReadFile(sessionFile(t, d, "dev1@dev"))

	return err == nil && strings.Contains(string(data), text)
}

// sessionFile is the fakeclaude session file of the session the daemon
// remembers for an agent (test/fakeclaude/sessions.go).
func sessionFile(t *testing.T, d *daemon.Daemon, address string) string {
	t.Helper()

	sessions, err := d.Store.AgentSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range sessions {
		if s.Address == address {
			return filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "fake-sessions", s.SessionID+".json")
		}
	}

	return ""
}
