package daemon

import (
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
)

func TestPickMember(t *testing.T) {
	t.Parallel()

	o := &org.Org{Teams: []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "a"}, {Name: "b"}, {Name: "c"}}}}}

	tests := []struct {
		name     string
		statuses map[string]runtime.Status
		exclude  string
		want     string
	}{
		{"idle member first", map[string]runtime.Status{"a@dev": runtime.StatusWorking, "b@dev": runtime.StatusIdle}, "", "b@dev"},
		{"then any running member", map[string]runtime.Status{"a@dev": runtime.StatusExited, "b@dev": runtime.StatusWorking}, "", "b@dev"},
		{"then anyone", map[string]runtime.Status{}, "", "a@dev"},
		{"excluded member skipped", map[string]runtime.Status{"a@dev": runtime.StatusIdle}, "a@dev", "b@dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := runtime.NewStatuses()
			for addr, st := range tt.statuses {
				s.Set(addr, st)
			}

			if got := pickMember(o, s, "dev", tt.exclude); got != tt.want {
				t.Fatalf("pickMember = %q, want %q", got, tt.want)
			}
		})
	}

	if got := pickMember(o, runtime.NewStatuses(), "ghosts", ""); got != "" {
		t.Fatalf("unknown team: got %q", got)
	}
}
