package org

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

func TestProjectAndTeamPrompts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	o := &Org{
		ProjectDir:    dir,
		ProjectPrompt: "A shop in Go.",
		Teams: []Team{
			{ID: "dev", Goal: "Build", Prompt: "Use TDD.", Agents: []Agent{{Name: "dev1", Role: "Developer"}}},
			{ID: "qa"},
		},
	}
	if err := WriteProject(o); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	dev, _ := got.Team("dev")
	qa, _ := got.Team("qa")

	if got.ProjectPrompt != "A shop in Go." || dev.Prompt != "Use TDD." || qa.Prompt != "" {
		t.Fatalf("prompts = %q, %q, %q", got.ProjectPrompt, dev.Prompt, qa.Prompt)
	}

	if len(dev.Agents) != 1 {
		t.Fatalf("TEAM.md read as an agent: %+v", dev.Agents)
	}

	// An empty prompt removes its file.
	if err := WriteTeamPrompt(dir, "dev", "  "); err != nil {
		t.Fatal(err)
	}

	if err := WriteProjectPrompt(dir, ""); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(ConfigDir(dir), teamsDir, "dev", teamPromptFile),
		filepath.Join(ConfigDir(dir), projectPromptFile),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still there after an empty prompt (err %v)", path, err)
		}
	}
}

func TestWithTeam(t *testing.T) {
	t.Parallel()

	o := &Org{Teams: []Team{{ID: "dev", Goal: "Build", Agents: []Agent{{Name: "dev1"}}}}}

	added := o.WithTeam(Team{ID: "qa", Goal: "Test", Agents: []Agent{{Name: "smuggled"}}})
	if len(o.Teams) != 1 || len(added.Teams) != 2 {
		t.Fatalf("receiver changed or team not added: %d, %d teams", len(o.Teams), len(added.Teams))
	}

	if qa, _ := added.Team("qa"); len(qa.Agents) != 0 {
		t.Fatal("a new team comes without agents")
	}

	changed := o.WithTeam(Team{ID: "dev", Goal: "Ship", Prompt: "Use TDD."})
	if dev, _ := changed.Team("dev"); dev.Goal != "Ship" || dev.Prompt != "Use TDD." || len(dev.Agents) != 1 {
		t.Fatalf("changed team = %+v", dev)
	}

	if dev, _ := o.Team("dev"); dev.Goal != "Build" {
		t.Fatal("the receiver's team changed")
	}
}

func TestEnsureDirector(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := WriteOrgFile(&Org{ProjectDir: dir}); err != nil {
		t.Fatal(err)
	}

	d := catalog.Definition{Role: "Director", Prompt: "Lead."}

	created, err := EnsureDirector(dir, "god", d)
	if err != nil || !created {
		t.Fatalf("first call: created %v, err %v", created, err)
	}

	// Another name does not add a second director.
	created, err = EnsureDirector(dir, "boss", d)
	if err != nil || created {
		t.Fatalf("second call: created %v, err %v", created, err)
	}

	o, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !o.IsGod("god") || len(o.Members()) != 1 {
		t.Fatalf("director = %+v, members %d", o.God, len(o.Members()))
	}
}

func TestWithoutTeam(t *testing.T) {
	t.Parallel()

	o := &Org{
		Teams:    []Team{{ID: "specs"}, {ID: "dev", Agents: []Agent{{Name: "dev1"}}}, {ID: "qa"}},
		Channels: []Channel{{From: "dev", To: "specs"}, {From: "qa", To: "dev"}, {From: "qa", To: "specs"}},
	}

	c := o.WithoutTeam("dev")

	if _, ok := c.Team("dev"); ok || len(c.Teams) != 2 {
		t.Fatalf("teams = %+v", c.Teams)
	}

	if len(c.Channels) != 1 || c.Channels[0] != (Channel{From: "qa", To: "specs"}) {
		t.Fatalf("channels = %+v, want only qa -> specs", c.Channels)
	}

	if _, ok := c.Member("dev1@dev"); ok {
		t.Fatal("the team's members should go with it")
	}

	if len(o.Teams) != 3 || len(o.Channels) != 3 {
		t.Fatal("the receiver changed")
	}

	if err := c.Validate(); err != nil {
		t.Fatalf("the organization without the team is invalid: %v", err)
	}
}
