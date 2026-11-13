package org

import (
	"errors"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	agent := func(name string) Agent { return Agent{Name: name} }

	tests := []struct {
		name    string
		org     Org
		wantErr bool
	}{
		{
			name: "valid",
			org: Org{
				Teams:    []Team{{ID: "specs", Agents: []Agent{agent("archi")}}, {ID: "dev", Agents: []Agent{agent("dev1")}}},
				Channels: []Channel{{From: "dev", To: "specs"}},
			},
		},
		{
			name:    "duplicate team",
			org:     Org{Teams: []Team{{ID: "dev"}, {ID: "dev"}}},
			wantErr: true,
		},
		{
			name:    "duplicate agent in a team",
			org:     Org{Teams: []Team{{ID: "dev", Agents: []Agent{agent("a"), agent("a")}}}},
			wantErr: true,
		},
		{
			name:    "invalid team name",
			org:     Org{Teams: []Team{{ID: "Dev Team"}}},
			wantErr: true,
		},
		{
			name:    "invalid agent name",
			org:     Org{Teams: []Team{{ID: "dev", Agents: []Agent{agent("Dev 1")}}}},
			wantErr: true,
		},
		{
			name: "invalid agent definition",
			org: Org{Teams: []Team{{ID: "dev", Agents: []Agent{
				{Name: "a", Permissions: catalog.Permissions{Profile: "yolo"}},
			}}}},
			wantErr: true,
		},
		{
			name:    "channel to unknown team",
			org:     Org{Teams: []Team{{ID: "dev"}}, Channels: []Channel{{From: "dev", To: "ghosts"}}},
			wantErr: true,
		},
		{
			name:    "channel to itself",
			org:     Org{Teams: []Team{{ID: "dev"}}, Channels: []Channel{{From: "dev", To: "dev"}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.org.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMembersAndChannels(t *testing.T) {
	t.Parallel()

	o := &Org{
		Teams:    []Team{{ID: "specs", Agents: []Agent{{Name: "archi"}}}, {ID: "dev", Agents: []Agent{{Name: "dev1"}}}},
		Channels: []Channel{{From: "dev", To: "specs"}},
	}

	if _, ok := o.Member("archi@specs"); !ok {
		t.Fatal("archi@specs not found")
	}

	if len(o.Members()) != 2 {
		t.Fatalf("members = %v", o.Members())
	}

	if !o.HasChannel("dev", "specs") || o.HasChannel("specs", "dev") {
		t.Fatal("channels are one-way: dev -> specs only")
	}
}

func TestTeamOf(t *testing.T) {
	t.Parallel()

	if got := TeamOf("archi@specs"); got != "specs" {
		t.Fatalf("TeamOf = %q", got)
	}

	if got := TeamOf(HumanAddress); got != "" {
		t.Fatalf("TeamOf(human) = %q, want empty", got)
	}
}

func TestWithAndWithoutAgentLeaveTheSnapshotAlone(t *testing.T) {
	t.Parallel()

	o := &Org{Teams: []Team{{ID: "dev", Agents: []Agent{{Name: "dev1", Role: "Developer"}}}}}

	added, err := o.WithAgent("dev", Agent{Name: "dev2"})
	if err != nil {
		t.Fatal(err)
	}

	replaced, err := o.WithAgent("dev", Agent{Name: "dev1", Role: "Lead"})
	if err != nil {
		t.Fatal(err)
	}

	removed := o.WithoutAgent("dev1@dev")

	if _, ok := added.Member("dev2@dev"); !ok {
		t.Error("WithAgent did not add dev2")
	}

	if m, _ := replaced.Member("dev1@dev"); m.Agent.Role != "Lead" || len(replaced.Members()) != 1 {
		t.Errorf("WithAgent did not replace dev1: %+v", replaced.Members())
	}

	if _, ok := removed.Member("dev1@dev"); ok {
		t.Error("WithoutAgent kept dev1")
	}

	if m, _ := o.Member("dev1@dev"); len(o.Members()) != 1 || m.Agent.Role != "Developer" {
		t.Fatalf("the original snapshot changed: %+v", o.Members())
	}

	if _, err := o.WithAgent("ghosts", Agent{Name: "a"}); !errors.Is(err, ErrUnknownTeam) {
		t.Fatalf("unknown team: err = %v", err)
	}
}
