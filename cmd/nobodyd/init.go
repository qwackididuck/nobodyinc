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

// runInit implements `nobodyd init [project-dir]`.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: nobodyd init [project-dir]\n\n"+
			"Creates <project-dir>/.nobody/ with its director (default: current directory).\n"+
			"Teams are created afterwards, from the control room or by asking the director.")
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

// initProject creates the .nobody/ directory of a project with its director
// and no team (the user builds the teams), and the user's template directory
// if it does not exist yet. It refuses to
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

	god, err := catalog.Director()
	if err != nil {
		return err
	}

	o := &org.Org{
		ProjectDir: dir,
		God:        &org.Agent{Name: god.Name, Definition: god.Recruit()},
	}

	if err := org.WriteProject(o); err != nil {
		return err
	}

	printNextSteps(out, dir, userCatalog)

	return nil
}

func printNextSteps(out io.Writer, dir, userCatalog string) {
	fmt.Fprintf(out, "Created %s with the director %s and no team yet.\n\n", org.ConfigDir(dir), catalog.DirectorName)

	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "Note: %s is not a git repository; features (branches and worktrees) need one.\n\n", dir)
	}

	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  - commit .nobody/ so that others can run the same organization;")

	if trusted, err := runtime.ProjectTrusted(dir); err != nil || !trusted {
		fmt.Fprintf(out, "  - trust the project once in Claude Code (run `claude` in %s and accept);\n", dir)
	}

	fmt.Fprintf(out, "  - start the daemon: nobodyd -project %s\n"+
		"  - create your teams in the control room, or ask the director to.\n"+
		"Your own agent templates go in %s; your settings are in %s.\n",
		dir, userCatalog, filepath.Join(filepath.Dir(userCatalog), settings.FileName))
}
