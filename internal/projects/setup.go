package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/settings"
)

// Directory modes when Nobody Inc creates them.
const (
	catalogDirPerm = 0o755
	newProjectPerm = 0o755
)

// gitIdentity signs the first commit of a project created by Nobody Inc, as
// the merges of finished features are (workspace.Repo).
var gitIdentity = []string{"-c", "user.name=Nobody Inc", "-c", "user.email=nobody@nobody.local"}

// Errors of SetUp and Create.
var (
	ErrNotDirectory = errors.New("not a directory")
	ErrExists       = errors.New("already exists")
)

// SetUpResult says what SetUp found and did.
type SetUpResult struct {
	// Dir is the project's absolute path.
	Dir string
	// Created is true when the folder had no .nobody/ yet and got one.
	Created bool
	// Git is true when the folder is a git repository: features (branches
	// and worktrees) need one.
	Git bool
	// Trusted is true when Claude Code trusts the folder. It is false only
	// when it could not be trusted (Claude Code was never set up on this
	// machine): agents then wait on Claude Code's trust question.
	Trusted bool
}

// SetUp makes a folder a Nobody Inc project, as opening it from the control
// room does, and is harmless on a folder that already is one:
//
//   - the user's catalog directory and settings file are created if missing;
//   - a folder without .nobody/ gets one with its director and no team (the
//     user builds the teams, or asks the director to);
//   - Claude Code is made to trust the folder, as accepting its trust
//     question would (runtime.Trust), so that agents do not stop on that
//     question (docs/workspaces.md).
//
// It writes the user's real Claude Code config unless CLAUDE_CONFIG_DIR
// points elsewhere.
func SetUp(home, projectDir string) (SetUpResult, error) {
	dir, err := filepath.Abs(projectDir)
	if err != nil {
		return SetUpResult{}, err
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return SetUpResult{}, fmt.Errorf("%s: %w", dir, ErrNotDirectory)
	}

	if err := ensureUserFiles(home); err != nil {
		return SetUpResult{}, err
	}

	result := SetUpResult{Dir: dir, Git: IsGitRepo(dir)}

	if !org.HasConfig(dir) {
		if err := writeNewConfig(dir); err != nil {
			return SetUpResult{}, err
		}

		result.Created = true
	}

	result.Trusted, err = trust(dir)
	if err != nil {
		return SetUpResult{}, err
	}

	return result, nil
}

// Create makes a new project: it creates the folder (its parent must
// exist), makes it a git repository with an empty first commit, so that
// features have a base branch to start from, then sets it up (SetUp).
func Create(ctx context.Context, home, projectDir string) (SetUpResult, error) {
	dir, err := filepath.Abs(projectDir)
	if err != nil {
		return SetUpResult{}, err
	}

	if _, err := os.Stat(dir); err == nil {
		return SetUpResult{}, fmt.Errorf("%s: %w", dir, ErrExists)
	}

	if err := os.Mkdir(dir, newProjectPerm); err != nil {
		return SetUpResult{}, fmt.Errorf("create the project folder: %w", err)
	}

	if err := gitInit(ctx, dir); err != nil {
		return SetUpResult{}, err
	}

	return SetUp(home, dir)
}

// IsGitRepo reports whether dir is the top of a git repository (it holds
// .git, a directory, or a file in a linked worktree).
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))

	return err == nil
}

// ensureUserFiles creates the user's template directory and settings file
// if they do not exist yet.
func ensureUserFiles(home string) error {
	userCatalog := filepath.Join(home, "catalog")
	if err := os.MkdirAll(userCatalog, catalogDirPerm); err != nil {
		return fmt.Errorf("create %s: %w", userCatalog, err)
	}

	_, err := settings.WriteDefaultIfMissing(home)

	return err
}

// writeNewConfig writes the .nobody/ of a new project: the built-in director
// alone (docs/god.md).
func writeNewConfig(dir string) error {
	god, err := catalog.Director()
	if err != nil {
		return err
	}

	return org.WriteProject(&org.Org{
		ProjectDir: dir,
		God:        &org.Agent{Name: god.Name, Definition: god.Recruit()},
	})
}

// trust makes Claude Code trust dir. A Claude Code that was never set up has
// no config to write to: that is reported as untrusted, not as an error.
func trust(dir string) (bool, error) {
	if _, err := runtime.Trust(dir); err != nil {
		if errors.Is(err, runtime.ErrNoClaudeConfig) {
			return false, nil
		}

		return false, fmt.Errorf("make Claude Code trust %s: %w", dir, err)
	}

	return true, nil
}

// gitInit makes dir a git repository with an empty first commit.
func gitInit(ctx context.Context, dir string) error {
	if _, err := git(ctx, dir, "init"); err != nil {
		return err
	}

	commit := append(append([]string{}, gitIdentity...), "commit", "--allow-empty", "-m", "Initial commit")
	_, err := git(ctx, dir, commit...)

	return err
}
