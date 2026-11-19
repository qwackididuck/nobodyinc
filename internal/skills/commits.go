package skills

import (
	"context"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/gitfiles"
)

// Uncommitted returns the names of the project skills that are not
// committed as they are: untracked, or changed since the last commit. Agents
// working on a feature only see what is committed (docs/workspaces.md), so
// the control room warns about these.
func Uncommitted(ctx context.Context, projectDir string, found Found) ([]string, error) {
	const dir = ".claude/skills"

	state, err := gitfiles.Of(ctx, projectDir, dir)
	if err != nil {
		return nil, err
	}

	trackedSkills := skillsOf(gitfiles.Under(state.Tracked, dir))
	changedSkills := skillsOf(gitfiles.Under(state.Changed, dir))

	var out []string

	for _, s := range found.Skills {
		if s.Scope == ScopeProject && (!trackedSkills[s.Name] || changedSkills[s.Name]) {
			out = append(out, s.Name)
		}
	}

	return out, nil
}

// skillsOf maps paths relative to the skills directory to the names of
// their skills: the first segment.
func skillsOf(paths []string) map[string]bool {
	names := map[string]bool{}

	for _, p := range paths {
		name, _, _ := strings.Cut(p, "/")
		names[name] = true
	}

	return names
}
