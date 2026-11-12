package org

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestLiveReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := WriteProject(&Org{ProjectDir: dir, Teams: []Team{{ID: "dev", Agents: []Agent{{Name: "dev1"}}}}}); err != nil {
		t.Fatal(err)
	}

	first, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	live := NewLive(first)

	// A recruit written to disk shows up after Reload; the old snapshot is
	// left untouched for whoever still holds it.
	writeFile(t, filepath.Join(dir, ".nobody", "teams", "dev", "dev2.md"), "---\nrole: Developer\n---\n")

	if _, err := live.Reload(); err != nil {
		t.Fatal(err)
	}

	if _, ok := live.Current().Member("dev2@dev"); !ok {
		t.Fatal("dev2@dev not visible after Reload")
	}

	if _, ok := first.Member("dev2@dev"); ok {
		t.Fatal("the previous snapshot was modified")
	}

	// A broken file keeps the last good organization.
	writeFile(t, filepath.Join(dir, ".nobody", "teams", "dev", "dev3.md"), "no front matter")

	if _, err := live.Reload(); err == nil {
		t.Fatal("Reload accepted a broken agent file")
	}

	if _, ok := live.Current().Member("dev2@dev"); !ok {
		t.Fatal("a failed Reload replaced the current organization")
	}
}

func TestLiveIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := WriteProject(&Org{ProjectDir: dir, Teams: []Team{{ID: "dev"}}}); err != nil {
		t.Fatal(err)
	}

	o, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	live := NewLive(o)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				_ = live.Current().Members()
			}
		})
		wg.Go(func() {
			for range 5 {
				if _, err := live.Reload(); err != nil {
					t.Error(err)
				}
			}
		})
	}

	wg.Wait()
}

func TestOrgIsItsOwnSource(t *testing.T) {
	t.Parallel()

	o := &Org{}

	var s Source = o
	if s.Current() != o {
		t.Fatal("a fixed Org should be its own source")
	}
}
