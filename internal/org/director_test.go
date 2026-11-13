package org

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectorIsAMemberWithoutTeam(t *testing.T) {
	t.Parallel()

	o := &Org{
		God:   &Agent{Name: "god", Role: "Director"},
		Teams: []Team{{ID: "dev", Agents: []Agent{{Name: "dev1"}}}},
	}

	m, ok := o.Member("god")
	if !ok || m.Team.ID != "" || m.Agent.Role != "Director" || !o.IsGod("god") || o.IsGod("dev1@dev") {
		t.Fatalf("god member = %+v, %v", m, ok)
	}

	if members := o.Members(); len(members) != 2 || members[0].Address != "god" {
		t.Fatalf("members = %+v, want god first, then dev1@dev", members)
	}

	if Address("god", DirectorTeam) != "god" || TeamOf("god") != "" {
		t.Fatal("the director's address is its name alone")
	}
}

func TestWithAgentDirector(t *testing.T) {
	t.Parallel()

	o := &Org{Teams: []Team{{ID: "dev"}}}

	withGod, err := o.WithAgent(DirectorTeam, Agent{Name: "boss", Role: "Director"})
	if err != nil || !withGod.IsGod("boss") || o.God != nil {
		t.Fatalf("WithAgent(director) = %+v, %v; the original must stay without director", withGod.God, err)
	}

	edited, err := withGod.WithAgent(DirectorTeam, Agent{Name: "boss", Role: "Chief"})
	if err != nil || edited.God.Role != "Chief" || withGod.God.Role != "Director" {
		t.Fatalf("editing the director: %+v, %v", edited.God, err)
	}

	if _, err := withGod.WithAgent(DirectorTeam, Agent{Name: "other"}); !errors.Is(err, ErrDirectorExists) {
		t.Fatalf("a second director: err = %v, want ErrDirectorExists", err)
	}

	if without := withGod.WithoutAgent("boss"); without.God != nil || withGod.God == nil {
		t.Fatal("WithoutAgent should remove the director from the copy only")
	}
}

func TestDirectorValidation(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"human", "nobody", "Big Boss"} {
		o := &Org{God: &Agent{Name: name}}
		if err := o.Validate(); err == nil {
			t.Errorf("director named %q accepted", name)
		}
	}
}

func TestDirectorFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := &Org{
		ProjectDir: dir,
		Teams:      []Team{{ID: "dev", Agents: []Agent{{Name: "dev1"}}}},
		God:        &Agent{Name: "god", Role: "Director", Prompt: "Lead."},
	}

	if err := WriteProject(want); err != nil {
		t.Fatal(err)
	}

	if !AgentFileExists(dir, DirectorTeam, "god") {
		t.Fatal("director file not written to .nobody/director/god.md")
	}

	got, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got.God == nil || got.God.Name != "god" || got.God.Prompt != "Lead." {
		t.Fatalf("director loaded as %+v", got.God)
	}

	writeFile(t, filepath.Join(dir, ".nobody", "director", "second.md"), "---\n---\n")

	if _, err := LoadProject(dir); err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("two directors: err = %v", err)
	}

	if err := RemoveAgent(dir, DirectorTeam, "second"); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadProject(dir); err != nil {
		t.Fatalf("back to one director: %v", err)
	}
}
