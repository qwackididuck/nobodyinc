package skills

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// Uncommitted returns the names of the project skills that are not
// committed as they are: untracked, or changed since the last commit. Agents
// working on a feature only see what is committed (docs/workspaces.md), so
// the control room warns about these.
//
// It asks git in the project's main checkout, twice for all skills: the files
// it tracks, and the files that differ from HEAD.
func Uncommitted(ctx context.Context, projectDir string, found Found) ([]string, error) {
	const dir = ".claude/skills"

	tracked, err := git(ctx, projectDir, "ls-files", "-z", "--", dir)
	if err != nil {
		return nil, err
	}

	changed, err := git(ctx, projectDir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", dir)
	if err != nil {
		return nil, err
	}

	trackedSkills := skillsOf(splitNul(tracked), dir)
	changedSkills := skillsOf(statusPaths(changed), dir)

	var out []string

	for _, s := range found.Skills {
		if s.Scope == ScopeProject && (!trackedSkills[s.Name] || changedSkills[s.Name]) {
			out = append(out, s.Name)
		}
	}

	return out, nil
}

// skillsOf maps paths under dir to the names of their skills.
func skillsOf(paths []string, dir string) map[string]bool {
	names := map[string]bool{}

	for _, p := range paths {
		rest, ok := strings.CutPrefix(path.Clean(p), dir+"/")
		if !ok {
			continue
		}

		name, _, _ := strings.Cut(rest, "/")
		names[name] = true
	}

	return names
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
