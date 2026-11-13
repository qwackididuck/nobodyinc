package runtime

import (
	"path/filepath"
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

// Claude Code permission modes used by the profiles (docs/agents.md).
// "manual" is the documented name of the mode Claude Code reports as
// "default": every sensitive action asks for permission.
const (
	modeDefault     = "manual"
	modeAcceptEdits = "acceptEdits"
	modeAuto        = "auto"
)

// protectedDirs are never written by agents, whatever their profile
// (ADR-0004):
//
//   - .nobody/ holds the agents' own configuration: only Nobody Inc changes it;
//   - .claude/ holds Claude Code settings an agent loads on its next start
//     (--setting-sources project,local): writing there would widen its own
//     permissions.
var protectedDirs = []string{".nobody", ".claude"}

// protectedRules are the deny rules for protectedDirs, both in the agent's
// working directory ("./", its worktree or the project) and, by absolute path
// ("//"), in the project's main checkout, which the daemon reads: a worktree
// lives outside the project (workspace.Repo.WorktreesDir), so the relative
// rules alone would not cover it.
func protectedRules(projectDir string) []string {
	var rules []string

	for _, dir := range protectedDirs {
		rules = append(rules, "Edit(./"+dir+"/**)", "Write(./"+dir+"/**)")

		if projectDir != "" {
			abs := "/" + filepath.ToSlash(filepath.Join(projectDir, dir)) // "//abs/path": absolute in Claude Code rules
			rules = append(rules, "Edit("+abs+"/**)", "Write("+abs+"/**)")
		}
	}

	return rules
}

// allowlistAllow is what the allowlist profile runs without asking, on top of
// the file edits that acceptEdits accepts in the working directory: the git
// commands an agent needs to commit its work (not "git *": checkout, restore
// or apply could write anywhere, the project's .nobody/ included), and the
// usual ways of running tests.
var allowlistAllow = []string{
	"Bash(git status)", "Bash(git status *)", "Bash(git diff)", "Bash(git diff *)",
	"Bash(git log)", "Bash(git log *)", "Bash(git show *)", "Bash(git add *)", "Bash(git commit *)",
	"Bash(go test *)", "Bash(go vet *)",
	"Bash(npm test *)", "Bash(npm run test *)", "Bash(pnpm test *)", "Bash(yarn test *)",
	"Bash(task test *)", "Bash(make test *)",
	"Bash(pytest *)", "Bash(cargo test *)",
}

// allowlistDeny keeps git within the machine: Nobody Inc never pushes
// (docs/workspaces.md), so an agent must not either.
var allowlistDeny = []string{"Bash(git push *)"}

// permissionMode is the --permission-mode of a profile:
//
//   - default: manual, every sensitive action goes to the approval queue;
//   - allowlist: acceptEdits (file edits in the working directory pass),
//     plus the allowlistAllow rules;
//   - auto: Claude Code's classifier decides.
func permissionMode(p catalog.Permissions) string {
	switch p.EffectiveProfile() {
	case catalog.ProfileAllowlist:
		return modeAcceptEdits
	case catalog.ProfileAuto:
		return modeAuto
	default:
		return modeDefault
	}
}

// permissionRules returns the allow and deny rules written into the agent's
// settings: the profile's presets, the agent's own rules, and the protection
// of .nobody/ and .claude/. Deny rules always win over allow rules in Claude
// Code.
func permissionRules(p catalog.Permissions, projectDir string) ([]string, []string) {
	var allow, deny []string

	if p.EffectiveProfile() == catalog.ProfileAllowlist {
		allow = append(allow, allowlistAllow...)
		deny = append(deny, allowlistDeny...)
	}

	allow = append(allow, p.Allow...)
	deny = append(deny, p.Deny...)
	deny = append(deny, protectedRules(projectDir)...)

	return dedupe(allow), dedupe(deny)
}

// dedupe removes repeated rules, keeping the first occurrence.
func dedupe(rules []string) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}

	return out
}
