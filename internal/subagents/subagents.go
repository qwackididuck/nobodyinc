// Package subagents reads the project's Claude Code subagents and turns them
// into agent definitions, for the human to import them into teams
// (ADR-0018).
//
// A subagent is a markdown file of .claude/agents/: a YAML header (name,
// description, tools, disallowedTools, model, permissionMode, skills, and
// keys Nobody Inc does not carry over), then its system prompt. Its identity
// is its name, not its file name. Agents keep getting the subagents as
// subagents (the "project" settings source): importing one copies it into
// an agent file, with from: subagent:<name>, and never touches .claude/.
package subagents

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/frontmatter"
)

// Subagent is one subagent of the project, and the agent it would become.
type Subagent struct {
	// Name is the subagent's name, its identity for Claude Code; "" when its
	// file has none (Err says so).
	Name string
	// File is its path, relative to the project.
	File        string
	Description string
	// Agent is the agent definition it converts to (Convert), with
	// from: subagent:<Name>.
	Agent catalog.Definition
	// AgentName is the name suggested for the agent: Name made a valid
	// agent name, "" when nothing usable is left.
	AgentName string
	// DroppedTools is its tools list, not carried over (its meaning is the
	// opposite of an agent's tools: ADR-0018); "" for none.
	DroppedTools string
	// IgnoredKeys are the header keys not carried over (mcpServers, hooks,
	// maxTurns...), and the skills left out for an invalid name.
	IgnoredKeys []string
	// Err is why the file cannot be imported; "" when it can.
	Err string
}

// Origin is the from: value of an agent imported from the subagent name.
func Origin(name string) string {
	return originPrefix + name
}

// originPrefix starts the from: value of an imported agent.
const originPrefix = "subagent:"

// Dir is where the project's subagents live: .claude/agents of its main
// checkout.
func Dir(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "agents")
}

// ext is the extension of a subagent file.
const ext = ".md"

// Load reads every subagent file of the project at projectDir,
// sub-directories included, hidden ones left out, sorted by name. A file
// that cannot be read is kept, with Err set, for the control room to show.
// A missing directory holds no subagents.
func Load(projectDir string) ([]Subagent, error) {
	root := Dir(projectDir)
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("read the subagents: %w", err)
	}

	var found []Subagent

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		hidden := path != root && strings.HasPrefix(d.Name(), ".")

		switch {
		case d.IsDir() && hidden:
			return filepath.SkipDir
		case d.IsDir(), hidden, !strings.HasSuffix(d.Name(), ext):
			return nil
		}

		rel, err := filepath.Rel(projectDir, path)
		if err != nil {
			return err
		}

		found = append(found, read(path, filepath.ToSlash(rel)))

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the subagents: %w", err)
	}

	slices.SortFunc(found, func(a, b Subagent) int { return strings.Compare(a.Name+"\x00"+a.File, b.Name+"\x00"+b.File) })

	return found, nil
}

// read reads the subagent file at path (rel relative to the project).
func read(path, rel string) Subagent {
	s := Subagent{File: rel}

	data, err := os.ReadFile(path)
	if err != nil {
		s.Err = err.Error()

		return s
	}

	f, err := frontmatter.Parse(data)
	if err != nil {
		s.Err = err.Error()

		return s
	}

	return Convert(f, rel)
}

// Convert turns a parsed subagent file into the agent it would become, as
// ADR-0018 sets it out.
func Convert(f frontmatter.File, rel string) Subagent {
	s := Subagent{
		Name:        strings.TrimSpace(f.Value(keyName)),
		File:        rel,
		Description: strings.TrimSpace(f.Description),
	}

	if s.Name == "" {
		s.Err = "no name in its header: Claude Code does not load it either"

		return s
	}

	model := modelOf(f.Value(keyModel))
	skills, badSkills := splitSkills(list(f.Value(keySkills)))

	s.Agent = catalog.Definition{
		Role:      role(s.Description),
		Expertise: expertise(s.Description),
		Model:     model,
		Permissions: catalog.Permissions{
			Profile: Profile(f.Value(keyPermissionMode), model),
			Deny:    list(f.Value(keyDisallowedTools)),
		},
		Skills: skills,
		From:   Origin(s.Name),
		Prompt: f.Body,
	}
	s.AgentName = AgentName(s.Name)
	s.DroppedTools = strings.Join(list(f.Value(keyTools)), ", ")

	for _, key := range f.Keys() {
		if !slices.Contains(carried, key) {
			s.IgnoredKeys = append(s.IgnoredKeys, key)
		}
	}

	for _, skill := range badSkills {
		s.IgnoredKeys = append(s.IgnoredKeys, "skills: "+skill)
	}

	return s
}

// The header keys read from a subagent.
const (
	keyName            = "name"
	keyModel           = "model"
	keyPermissionMode  = "permissionMode"
	keyDisallowedTools = "disallowedTools"
	keySkills          = "skills"
	keyTools           = "tools"
)

// carried are the header keys that end up in the agent (tools is read, but
// shown apart: DroppedTools).
var carried = []string{keyName, keyModel, keyPermissionMode, keyDisallowedTools, keySkills, keyTools}

// modelOf is the agent's model for a subagent's: "inherit" or none is
// Claude Code's default.
func modelOf(model string) string {
	model = strings.TrimSpace(model)
	if model == "inherit" {
		return ""
	}

	return model
}

// Profile is the permission profile of an agent imported from a subagent
// with that permissionMode and model: acceptEdits is allowlist, auto is auto
// (but with a Haiku model, which has no auto mode), anything else default.
func Profile(permissionMode, model string) string {
	switch strings.TrimSpace(permissionMode) {
	case "acceptEdits":
		return catalog.ProfileAllowlist
	case "auto":
		if strings.Contains(strings.ToLower(model), "haiku") {
			return catalog.ProfileDefault
		}

		return catalog.ProfileAuto
	default:
		return catalog.ProfileDefault
	}
}

// AgentName makes a subagent's name a valid agent name: lower case, "_",
// spaces and dots as "-", other characters dropped, dashes not repeated nor
// at the ends; "" when no letter starts what is left.
func AgentName(name string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(name) {
		if keep, ok := nameRune(r); ok && (keep != '-' || !strings.HasSuffix(b.String(), "-")) {
			b.WriteRune(keep)
		}
	}

	out := strings.Trim(b.String(), "-")
	if !catalog.ValidName(out) {
		return ""
	}

	return out
}

// nameRune is what a (lower-case) character of a subagent's name becomes in
// an agent name: letters and digits stay, separators become "-", the rest
// is dropped (false).
func nameRune(r rune) (rune, bool) {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return r, true
	case r == '-' || r == '_' || r == '.' || unicode.IsSpace(r):
		return '-', true
	default:
		return 0, false
	}
}

// roleMax and expertiseMax bound the role and expertise taken from a
// description: the role is one line in every colleague's org chart, the
// expertise a short public summary.
const (
	roleMax      = 80
	expertiseMax = 300
)

// role is the first sentence of a description, on one line.
func role(description string) string {
	line := strings.Join(strings.Fields(description), " ")
	if i := strings.Index(line, ". "); i >= 0 {
		line = line[:i]
	}

	return shorten(strings.TrimSuffix(line, "."), roleMax)
}

// expertise is a description on one line, shortened if long.
func expertise(description string) string {
	return shorten(strings.Join(strings.Fields(description), " "), expertiseMax)
}

// shorten cuts s to at most limit characters, at a word, with an ellipsis.
func shorten(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	cut := string(runes[:limit-1])
	if i := strings.LastIndex(cut, " "); i > limit/2 {
		cut = cut[:i]
	}

	return strings.TrimRight(cut, " ,;:") + "…"
}

// list splits a header list, written as a YAML list or as a comma-separated
// string (frontmatter.File.Value joins a list with ", ").
func list(value string) []string {
	var out []string

	for item := range strings.SplitSeq(strings.Trim(value, "[]"), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}

	return out
}

// splitSkills separates the skill names an agent file accepts from the
// others.
func splitSkills(names []string) ([]string, []string) {
	var good, bad []string

	for _, name := range names {
		if (catalog.Definition{Skills: []string{name}}).Validate() == nil {
			good = append(good, name)
		} else {
			bad = append(bad, name)
		}
	}

	return good, bad
}
