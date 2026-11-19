// Package gitfiles tells which files of a directory are committed as they
// are, in a project's main checkout. Agents working on a feature only see
// what is committed (docs/workspaces.md), so the control room warns about
// project skills and commands that are not (internal/skills,
// internal/commands).
package gitfiles

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// State is what git says of the files under a directory, by path relative to
// the repository, with "/" separators.
type State struct {
	// Tracked are the files git tracks. A file absent from it is untracked
	// or ignored.
	Tracked map[string]bool
	// Changed are the files that differ from HEAD, untracked ones included
	// (ignored ones are not listed).
	Changed map[string]bool
}

// Committed reports whether the file at file (relative to the repository)
// is committed as it is.
func (s State) Committed(file string) bool {
	return s.Tracked[file] && !s.Changed[file]
}

// Of asks git, in the repository at repo, about the files under dir
// (relative to repo, "/" separators): twice for all of them, the files it
// tracks and the files that differ from HEAD.
func Of(ctx context.Context, repo, dir string) (State, error) {
	tracked, err := git(ctx, repo, "ls-files", "-z", "--", dir)
	if err != nil {
		return State{}, err
	}

	changed, err := git(ctx, repo, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", dir)
	if err != nil {
		return State{}, err
	}

	return State{Tracked: set(splitNul(tracked)), Changed: set(statusPaths(changed))}, nil
}

// Under returns the paths of set that are under dir, relative to dir.
func Under(paths map[string]bool, dir string) []string {
	var out []string

	for p := range paths {
		if rest, ok := strings.CutPrefix(path.Clean(p), dir+"/"); ok {
			out = append(out, rest)
		}
	}

	return out
}

func set(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[path.Clean(p)] = true
	}

	return out
}

// statusPaths returns the paths of `git status --porcelain=v1 -z`: each entry
// is "XY path", followed by the original path for a rename or a copy.
func statusPaths(out string) []string {
	var paths []string

	entries := splitNul(out)
	for i := 0; i < len(entries); i++ {
		const prefix = len("XY ")

		e := entries[i]
		if len(e) <= prefix {
			continue
		}

		paths = append(paths, e[prefix:])

		if e[0] == 'R' || e[0] == 'C' {
			i++ // skip the original path
		}
	}

	return paths
}

func splitNul(s string) []string {
	var out []string

	for part := range strings.SplitSeq(s, "\x00") {
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w", args[0], dir, err)
	}

	return string(out), nil
}
