package officemcp

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// fakeSchedules records the proposals.
type fakeSchedules struct {
	proposed []string
}

func (f *fakeSchedules) List(context.Context) ([]schedule.View, error) {
	return []schedule.View{{ID: "watch", Every: "10m"}}, nil
}

func (f *fakeSchedules) Propose(_ context.Context, by string, s schedule.Schedule) (schedule.Proposal, error) {
	f.proposed = append(f.proposed, by+": "+s.ID+" "+s.Every+" "+s.Target+" "+string(s.Mode))

	return schedule.Proposal{ID: "SP-1", Warning: "careful"}, nil
}

func connectWithSchedules(t *testing.T, address string, schedules Schedules) *mcp.ClientSession {
	t.Helper()

	o := testOrg()
	o.God = &org.Agent{Name: "god", Role: "Director"}
	sender := &messaging.Service{Store: memStore{}, Policy: messaging.Policy{Org: o}}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()

	office := Office{Sender: sender, Director: &fakeDirector{}, Schedules: schedules, Version: "test"}
	if _, err := NewServer(o, office, address).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session
}

func TestScheduleToolsAreTheDirectors(t *testing.T) {
	t.Parallel()

	god := toolNames(t, connectWithSchedules(t, "god", &fakeSchedules{}))
	dev := toolNames(t, connectWithSchedules(t, "dev1@dev", &fakeSchedules{}))

	for _, name := range scheduleToolNames {
		if !slices.Contains(god, name) {
			t.Errorf("the director misses %s", name)
		}

		if slices.Contains(dev, name) {
			t.Errorf("a team agent got %s", name)
		}

		if !slices.Contains(ToolNames(), name) {
			t.Errorf("%s is not allowed without asking (ToolNames)", name)
		}
	}

	for _, name := range god {
		if strings.HasPrefix(name, "schedule_") && !slices.Contains(scheduleToolNames, name) {
			t.Errorf("unexpected schedule tool %s: the director may only list and propose", name)
		}
	}
}

func TestScheduleTools(t *testing.T) {
	t.Parallel()

	schedules := &fakeSchedules{}
	god := connectWithSchedules(t, "god", schedules)

	text, isErr := call(t, god, "schedule_propose", map[string]any{
		"id": "watch", "title": "Watch", "every": "10m", "target": "monitor@ops", "mode": "message", "instructions": "Look.",
	})
	if isErr || !strings.Contains(text, `"id":"SP-1"`) || !strings.Contains(text, `"warning":"careful"`) {
		t.Fatalf("schedule_propose = %s (error %v)", text, isErr)
	}

	if len(schedules.proposed) != 1 || schedules.proposed[0] != "god: watch 10m monitor@ops message" {
		t.Fatalf("proposed = %v", schedules.proposed)
	}

	if text, isErr := call(t, god, "schedule_list", nil); isErr || !strings.Contains(text, `"id":"watch"`) {
		t.Fatalf("schedule_list = %s (error %v)", text, isErr)
	}
}
