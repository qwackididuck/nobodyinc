package runtime

import (
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/hook"
)

func TestObserveHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		event     string
		delivered int
		want      Status
	}{
		{"stop with nothing delivered: idle", hook.EventStop, 0, StatusIdle},
		{"stop with mail delivered: keeps working", hook.EventStop, 2, StatusWorking},
		{"tool use: working", hook.EventPostToolUse, 0, StatusWorking},
		{"permission request: awaiting approval", hook.EventPermissionRequest, 0, StatusAwaitingApproval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewStatuses()
			s.ObserveHook("archi@specs", hook.Event{Name: tt.event}, tt.delivered)

			if got := s.Get("archi@specs"); got != tt.want {
				t.Fatalf("status = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSessionIsLearntFromHooks(t *testing.T) {
	t.Parallel()

	s := NewStatuses()
	if s.Session("archi@specs") != "" {
		t.Fatal("no session before any hook")
	}

	s.ObserveHook("archi@specs", hook.Event{Name: hook.EventPostToolUse, SessionID: "s-1"}, 0)

	if got := s.Session("archi@specs"); got != "s-1" {
		t.Fatalf("session = %q", got)
	}

	if !s.Busy("archi@specs") {
		t.Fatal("an agent using a tool is busy")
	}
}

func TestUnknownAgentIsStopped(t *testing.T) {
	t.Parallel()

	if got := NewStatuses().Get("ghost@dev"); got != StatusStopped {
		t.Fatalf("status = %s, want stopped", got)
	}
}

func TestOnIdle(t *testing.T) {
	t.Parallel()

	s := NewStatuses()

	var idle []string

	s.OnIdle = func(address string) { idle = append(idle, address) }

	s.Set("archi@specs", StatusWorking)
	s.Set("archi@specs", StatusIdle)

	if len(idle) != 1 || idle[0] != "archi@specs" {
		t.Fatalf("OnIdle calls = %v", idle)
	}
}

func TestOnChangeOnlyForRealChanges(t *testing.T) {
	t.Parallel()

	s := NewStatuses()

	var changes []Status

	s.OnChange = func(_ string, st Status) { changes = append(changes, st) }

	for _, st := range []Status{StatusStopped, StatusStarting, StatusWorking, StatusWorking, StatusIdle, StatusIdle} {
		s.Set("dev1@dev", st)
	}

	want := []Status{StatusStarting, StatusWorking, StatusIdle}
	if len(changes) != len(want) {
		t.Fatalf("changes = %v, want %v (an unknown agent is stopped; repeats are not changes)", changes, want)
	}

	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("changes = %v, want %v", changes, want)
		}
	}
}
