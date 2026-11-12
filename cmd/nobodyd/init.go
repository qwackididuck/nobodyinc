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
)

// catalogDirPerm is the mode of ~/.nobody/catalog when init creates it.
const catalogDirPerm = 0o755

// First team written by init: one developer recruited from the built-in
// "dev" template, so that the project runs right away.
const (
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

	tmpl, err := catalog.New(userCatalog).Get(firstTemplate)
	if err != nil {
		return err
	}

	o := &org.Org{
		ProjectDir: dir,
		Teams: []org.Team{{
			ID:     firstTeam,
			Goal:   firstGoal,
			Agents: []org.Agent{{Name: firstAgent, Definition: tmpl.Recruit()}},
		}},
	}

	if err := org.WriteProject(o); err != nil {
		return err
	}

	printNextSteps(out, dir, userCatalog)

	return nil
}

func printNextSteps(out io.Writer, dir, userCatalog string) {
	fmt.Fprintf(out, "Created %s with team %q and agent %s.\n\n", org.ConfigDir(dir), firstTeam, org.Address(firstAgent, firstTeam))

	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "Note: %s is not a git repository; features (branches and worktrees) need one.\n\n", dir)
	}

	fmt.Fprintf(out, "Next steps:\n"+
		"  - commit .nobody/ so that others can run the same organization;\n"+
		"  - trust the project once in Claude Code (run `claude` in %s);\n"+
		"  - start the daemon: nobodyd -project %s\n"+
		"Your own agent templates go in %s.\n", dir, dir, userCatalog)
}
