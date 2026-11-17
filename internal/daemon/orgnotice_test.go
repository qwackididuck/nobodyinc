package daemon

import (
	"reflect"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

func TestOrgChanges(t *testing.T) {
	t.Parallel()

	base := func() *org.Org {
		return &org.Org{Teams: []org.Team{{ID: "dev", Goal: "Build", Agents: []org.Agent{
			{Name: "dev1", Role: "Developer"},
			{Name: "dev2", Role: "Developer"},
		}}}}
	}

	// devBlocksQA is a starting point where team dev blocks team qa.
	devBlocksQA := func(o *org.Org) *org.Org { return withBlock(o.WithTeam(org.Team{ID: "qa"}), "dev", "qa") }

	tests := []struct {
		name string
		// setup, when set, changes base before the change.
		setup  func(o *org.Org) *org.Org
		change func(o *org.Org) *org.Org
		want   []orgChange
	}{
		{
			name:   "nothing",
			change: func(o *org.Org) *org.Org { return o },
		},
		{
			name: "arrival with expertise",
			change: func(o *org.Org) *org.Org {
				c, _ := o.WithAgent("dev", org.Agent{Name: "qa1", Role: "QA", Expertise: "edge cases"})

				return c
			},
			want: []orgChange{{About: "qa1@dev", Text: "qa1@dev (QA) joined team dev. Ask them about: edge cases."}},
		},
		{
			name:   "departure",
			change: func(o *org.Org) *org.Org { return o.WithoutAgent("dev2@dev") },
			want:   []orgChange{{About: "dev2@dev", Text: "dev2@dev (Developer) left the organization."}},
		},
		{
			name: "leave",
			change: func(o *org.Org) *org.Org {
				c, _ := o.WithAgent("dev", org.Agent{Name: "dev1", Role: "Developer", OnLeave: true})

				return c
			},
			want: []orgChange{{About: "dev1@dev", Text: "dev1@dev is on leave: mail to them waits until they return; " +
				"write to their team if it cannot wait."}},
		},
		{
			name: "new role",
			change: func(o *org.Org) *org.Org {
				c, _ := o.WithAgent("dev", org.Agent{Name: "dev1", Role: "Lead", Expertise: "the plan"})

				return c
			},
			want: []orgChange{{About: "dev1@dev", Text: "dev1@dev is now Lead. Ask them about: the plan."}},
		},
		{
			name:   "new team",
			change: func(o *org.Org) *org.Org { return o.WithTeam(org.Team{ID: "qa", Goal: "Test it"}) },
			want:   []orgChange{{Text: "New team qa: Test it."}},
		},
		{
			name:   "team closed",
			change: func(o *org.Org) *org.Org { return o.WithoutTeam("dev") },
			want: []orgChange{
				{Text: "Team dev was closed, with its members."},
				{About: "dev1@dev", Text: "dev1@dev (Developer) left the organization."},
				{About: "dev2@dev", Text: "dev2@dev (Developer) left the organization."},
			},
		},
		{
			name: "new block: told to both teams",
			change: func(o *org.Org) *org.Org {
				return withBlock(o.WithTeam(org.Team{ID: "qa"}), "dev", "qa")
			},
			want: []orgChange{
				{Teams: []string{"dev", "qa"}, Text: "Team dev may no longer start conversations with team qa; " +
					"its members can still answer messages from qa."},
				{Text: "New team qa."},
			},
		},
		{
			name:  "block lifted: told to both teams",
			setup: devBlocksQA,
			change: func(o *org.Org) *org.Org {
				return withBlock(o, "dev")
			},
			want: []orgChange{{Teams: []string{"dev", "qa"}, Text: "Team dev may start conversations with team qa again."}},
		},
		{
			name:  "blocked team closed: its closure says enough",
			setup: devBlocksQA,
			change: func(o *org.Org) *org.Org {
				return o.WithoutTeam("qa")
			},
			want: []orgChange{{Text: "Team qa was closed, with its members."}},
		},
		{
			name: "new instructions only: nothing to tell",
			change: func(o *org.Org) *org.Org {
				c, _ := o.WithAgent("dev", org.Agent{Name: "dev1", Role: "Developer", Prompt: "New."})

				return c
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := base()
			if tt.setup != nil {
				before = tt.setup(before)
			}

			if got := orgChanges(before, tt.change(before)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("changes = %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

// withBlock returns a copy of o where team id blocks the given teams.
func withBlock(o *org.Org, id string, block ...string) *org.Org {
	t, _ := o.Team(id)
	t.Block = block

	return o.WithTeam(t)
}

func TestOrgChangeToldTo(t *testing.T) {
	t.Parallel()

	block := orgChange{Teams: []string{"dev", "qa"}, Text: "blocked"}
	arrival := orgChange{About: "dev1@dev", Text: "arrived"}

	tests := []struct {
		name          string
		change        orgChange
		address, team string
		want          bool
	}{
		{"a block, to the team that blocks", block, "dev2@dev", "dev", true},
		{"a block, to the team blocked", block, "tester@qa", "qa", true},
		{"a block, not to another team", block, "archi@specs", "specs", false},
		{"a block, not to the director", block, "god", "", false},
		{"an arrival, to everyone else", arrival, "archi@specs", "specs", true},
		{"an arrival, not to the agent itself", arrival, "dev1@dev", "dev", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.change.toldTo(tt.address, tt.team); got != tt.want {
				t.Fatalf("toldTo(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}
