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

	tests := []struct {
		name   string
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
			if got := orgChanges(before, tt.change(before)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("changes = %#v\nwant %#v", got, tt.want)
			}
		})
	}
}
