package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// StagedSkillsDir is where Stage puts the links, inside the staging
// directory: Claude Code loads <dir>/.claude/skills of a directory given with
// --add-dir (checked by test/contract).
func StagedSkillsDir(stagingDir string) string {
	return filepath.Join(stagingDir, ".claude", "skills")
}

// Stage makes the staging directory hold one symbolic link per personal
// skill agents get, and nothing else: links to skills now gone or shadowed
// are removed. Every agent is started with --add-dir on stagingDir.
//
// Links rather than copies: a change of a personal skill reaches the agents
// at their next start without copying again. Agents cannot write through
// them, as they are denied writes to the staging and personal directories
// (runtime.protectedRules).
func Stage(stagingDir string, personal []Skill) error {
	dir := StagedSkillsDir(stagingDir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("staging directory for personal skills: %w", err)
	}

	want := make(map[string]string, len(personal))
	for _, s := range personal {
		want[s.Name] = s.Dir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("staging directory for personal skills: %w", err)
	}

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())

		if target, ok := want[e.Name()]; ok && linksTo(path, target) {
			delete(want, e.Name()) // already in place

			continue
		}

		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("staging directory for personal skills: %w", err)
		}
	}

	for name, target := range want {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("stage personal skill %s: %w", name, err)
		}
	}

	return nil
}

// linksTo reports whether path is a symbolic link to target.
func linksTo(path, target string) bool {
	got, err := os.Readlink(path)
	if errors.Is(err, fs.ErrInvalid) {
		return false // not a link
	}

	return err == nil && got == target
}
