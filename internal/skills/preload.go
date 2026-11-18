package skills

import "path/filepath"

// Preloaded is a skill whose instructions an agent gets at every session
// start, because its file lists it under skills: (ADR-0011).
type Preloaded struct {
	Name string
	// Dir is the skill's directory: its instructions may refer to files
	// next to its SKILL.md.
	Dir string
	// Body is the SKILL.md without its header.
	Body string
}

// Preload finds the skills an agent preloads. A project skill is read in the
// directory the agent works in (its team's worktree during a feature, so the
// version committed on the feature's branch), else the personal skill of that
// name is read. A skill found in neither place is returned in missing, for a
// warning.
func Preload(names []string, workDir string, found Found) ([]Preloaded, []string) {
	var (
		preloaded []Preloaded
		missing   []string
	)

	for _, name := range names {
		if p, ok := preloadFrom(name, filepath.Join(ProjectDir(workDir), name)); ok {
			preloaded = append(preloaded, p)

			continue
		}

		if s, ok := found.Get(ScopePersonal, name); ok {
			if p, ok := preloadFrom(name, s.Dir); ok {
				preloaded = append(preloaded, p)

				continue
			}
		}

		missing = append(missing, name)
	}

	return preloaded, missing
}

func preloadFrom(name, dir string) (Preloaded, bool) {
	f, err := ReadFile(dir)
	if err != nil {
		return Preloaded{}, false
	}

	return Preloaded{Name: name, Dir: dir, Body: f.Body}, true
}
