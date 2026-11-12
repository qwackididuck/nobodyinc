package runtime

import (
	"slices"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
)

// Claude Code permission modes used by the profiles (docs/agents.md).
const (
	modeDefault     = "default"
	modeAcceptEdits = "acceptEdits"
	modeAuto        = "auto"
)

// protectedConfigRules are denied to every agent, whatever its profile: the
// project's .nobody/ holds the agents' own configuration, and only Nobody Inc
// may change it (ADR-0004). Paths starting with "./" are relative to the
// agent's working directory (its worktree, or the project).
var protectedConfigRules = []string{"Edit(./.nobody/**)", "Write(./.nobody/**)"}

// allowlistAllow is what the allowlist profile runs without asking, on top of
// the file edits that acceptEdits accepts in the working directory: git, and
// the usual ways of running tests.
var allowlistAllow = []string{
	"Bash(git *)",
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
//   - default: every sensitive action goes to the approval queue;
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
// of .nobody/. Deny rules always win over allow rules in Claude Code.
func permissionRules(p catalog.Permissions) ([]string, []string) {
	var allow, deny []string

	if p.EffectiveProfile() == catalog.ProfileAllowlist {
		allow = append(allow, allowlistAllow...)
		deny = append(deny, allowlistDeny...)
	}

	allow = append(allow, p.Allow...)
	deny = append(deny, p.Deny...)
	deny = append(deny, protectedConfigRules...)

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
