package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/settings"
)

// catalogDirPerm is the mode of ~/.nobody/catalog when init creates it.
const catalogDirPerm = 0o755

// What init writes, so that the project runs right away: a director
// recruited from the built-in "god" template, and a first team with one
// developer recruited from the built-in "dev" template.
const (
	godName       = "god"
	godTemplate   = "god"
	firstTeam     = "dev"
	firstGoal     = "Implement the requested features with unit tests."
	firstAgent    = "dev1"
	firstTemplate = "dev"
)

// runInit implements `nobodyd init [project-dir]`.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: nobodyd init [project-dir]\n\n"+
			"Creates <project-dir>/.nobody/ with a first team (default: current directory).")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	projectDir := "."
	if fs.NArg() > 0 {
		projectDir = fs.Arg(0)
	}

	home, err := nobodyHome()
	if err != nil {
		return err
	}

	return initProject(os.Stdout, home, projectDir)
}

// initProject creates the .nobody/ directory of a project with a first team,
// and the user's template directory if it does not exist yet. It refuses to
// touch a project that already has a configuration.
func initProject(out io.Writer, home, projectDir string) error {
	dir, err := filepath.Abs(projectDir)
	if err != nil {
		return err
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	if org.HasConfig(dir) {
		return fmt.Errorf("%s already exists: edit it, or remove it to start over", org.ConfigDir(dir))
	}

	userCatalog := filepath.Join(home, "catalog")
	if err := os.MkdirAll(userCatalog, catalogDirPerm); err != nil {
		return fmt.Errorf("create %s: %w", userCatalog, err)
	}

	if _, err := settings.WriteDefaultIfMissing(home); err != nil {
		return err
	}

	templates := catalog.New(userCatalog)

	dev, err := templates.Get(firstTemplate)
	if err != nil {
		return err
	}

	god, err := templates.Get(godTemplate)
	if err != nil {
		return err
	}

	o := &org.Org{
		ProjectDir: dir,
		God:        &org.Agent{Name: godName, Definition: god.Recruit()},
		Teams: []org.Team{{
			ID:     firstTeam,
			Goal:   firstGoal,
			Agents: []org.Agent{{Name: firstAgent, Definition: dev.Recruit()}},
		}},
	}

	if err := org.WriteProject(o); err != nil {
		return err
	}

	printNextSteps(out, dir, userCatalog)

	return nil
}

func printNextSteps(out io.Writer, dir, userCatalog string) {
	fmt.Fprintf(out, "Created %s with the director %s, and team %q with agent %s.\n\n",
		org.ConfigDir(dir), godName, firstTeam, org.Address(firstAgent, firstTeam))

	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "Note: %s is not a git repository; features (branches and worktrees) need one.\n\n", dir)
	}

	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  - commit .nobody/ so that others can run the same organization;")

	if trusted, err := runtime.ProjectTrusted(dir); err != nil || !trusted {
		fmt.Fprintf(out, "  - trust the project once in Claude Code (run `claude` in %s and accept);\n", dir)
	}

	fmt.Fprintf(out, "  - start the daemon: nobodyd -project %s\n"+
		"Your own agent templates go in %s; your settings are in %s.\n",
		dir, userCatalog, filepath.Join(filepath.Dir(userCatalog), settings.FileName))
}
