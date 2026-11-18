package daemon

import (
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

func TestSessionStartedSettlesASilentResume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		idle, held  bool
		wantSettled runtime.Status
	}{
		{name: "not a silent resume: unchanged", held: true},
		{name: "silent resume: idle", idle: true, wantSettled: runtime.StatusIdle},
		{name: "silent resume of an agent a budget holds: on hold", idle: true, held: true, wantSettled: runtime.StatusBudgetHold},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			started := func(string) (string, bool) { return "the organization", tt.idle }
			held := func(string) bool { return tt.held }

			reopen, settled := sessionStarted(started, held)("dev1@dev")
			if reopen != "the organization" || settled != tt.wantSettled {
				t.Errorf("got %q, %q; want the organization, %q", reopen, settled, tt.wantSettled)
			}
		})
	}
}
