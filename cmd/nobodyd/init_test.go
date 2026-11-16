package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

func TestInitProject(t *testing.T) {
	t.Parallel()

	home, project := t.TempDir(), t.TempDir()

	var out bytes.Buffer
	if err := initProject(&out, home, project); err != nil {
		t.Fatal(err)
	}

	o, err := org.LoadProject(project)
	if err != nil {
		t.Fatal(err)
	}

	if god, ok := o.Member("god"); !ok || !o.IsGod("god") || god.Agent.Prompt == "" || god.Agent.Model != "opus" {
		t.Fatalf("director not created from the built-in director: %+v", god)
	}

	if len(o.Teams) != 0 || len(o.Members()) != 1 {
		t.Fatalf("init should create the director alone, got teams %+v", o.Teams)
	}

	if _, err := os.Stat(filepath.Join(home, "catalog")); err != nil {
		t.Fatalf("user catalog not created: %v", err)
	}

	if !strings.Contains(out.String(), "not a git repository") {
		t.Fatalf("missing git warning in:\n%s", out.String())
	}

	if err := initProject(&out, home, project); err == nil {
		t.Fatal("second init should refuse an existing configuration")
	}
}

func TestProjectStateDir(t *testing.T) {
	t.Parallel()

	a := projectStateDir("/home/me/.nobody", "/work/shop")
	b := projectStateDir("/home/me/.nobody", "/other/shop")

	if a == b {
		t.Fatal("two projects with the same name share a state directory")
	}

	if !strings.HasPrefix(filepath.Base(a), "shop-") || filepath.Dir(a) != "/home/me/.nobody/projects" {
		t.Fatalf("state dir = %s", a)
	}

	if a != projectStateDir("/home/me/.nobody", "/work/shop") {
		t.Fatal("state dir is not stable")
	}
}
