package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Permission profiles (see docs/agents.md and ADR-0004).
const (
	// ProfileDefault sends every sensitive action to the approval queue.
	ProfileDefault = "default"
	// ProfileAllowlist lets routine work (git, tests, edits) run unasked.
	ProfileAllowlist = "allowlist"
	// ProfileAuto lets Claude Code's classifier decide.
	ProfileAuto = "auto"
)

var profiles = []string{ProfileDefault, ProfileAllowlist, ProfileAuto}

// Definition is the content of a template or agent file: the YAML front
// matter and, in Prompt, the markdown body.
//
// Templates use Description and Version; recruited agents use From and
// OnLeave. Everything else is shared, so recruiting is a copy.
type Definition struct {
	// Description is the one-line summary shown in the catalog.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Version identifies a revision of a template; bump it on change.
	Version int `json:"version,omitempty" yaml:"version,omitempty"`
	// Role is a one-line description, e.g. "Software architect".
	Role string `json:"role,omitempty" yaml:"role,omitempty"`
	// Expertise is public and short: what colleagues should ask this agent
	// about, and what it decides. Every colleague sees it in the
	// organization chart, which tells them whom to ask.
	Expertise string `json:"expertise,omitempty" yaml:"expertise,omitempty"`
	// Model is the Claude model; empty means the Claude Code default.
	Model       string      `json:"model,omitempty"      yaml:"model,omitempty"`
	Permissions Permissions `json:"permissions,omitzero" yaml:"permissions,omitempty"`
	// Tools may be used without asking, on top of the office tools.
	Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	// MCPExclude lists the MCP servers ("gitlab") or single tools
	// ("gitlab/delete_project") the agent does not get. Every agent gets every
	// server of Claude Code's three scopes otherwise (ADR-0010).
	MCPExclude []string `json:"mcp_exclude,omitempty" yaml:"mcp_exclude,omitempty"`
	// Skills are preloaded, as in a Claude Code subagent definition: their
	// content is handed to the agent at every session start (ADR-0011). The
	// agent gets every other skill too, to invoke when it needs one.
	Skills []string `json:"skills,omitempty" yaml:"skills,omitempty"`
	// SkillsExclude lists the skills the agent does not get, built-in ones
	// included. An exclusion naming a skill absent from this machine is
	// ignored.
	SkillsExclude []string `json:"skills_exclude,omitempty" yaml:"skills_exclude,omitempty"`
	// From is "<template>@<version>" for a recruit; information only.
	From string `json:"from,omitempty" yaml:"from,omitempty"`
	// OnLeave stops the agent while keeping its file and session.
	OnLeave bool `json:"on_leave,omitempty" yaml:"on_leave,omitempty"`
	// Prompt is the markdown body: the agent's own instructions.
	Prompt string `json:"prompt,omitempty" yaml:"-"`
}

// Permissions sets how freely an agent may act.
type Permissions struct {
	// Profile is one of the Profile constants; empty means ProfileDefault.
	Profile string `json:"profile,omitempty" yaml:"profile,omitempty"`
	// Allow and Deny are Claude Code permission rules, e.g. "Bash(go test *)".
	Allow []string `json:"allow,omitempty" yaml:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"  yaml:"deny,omitempty"`
}

// EffectiveProfile is the profile, with the empty value resolved.
func (p Permissions) EffectiveProfile() string {
	if p.Profile == "" {
		return ProfileDefault
	}

	return p.Profile
}

// validName is the rule for template, agent and team names: they appear in
// file names and in addresses ("dev1@dev").
var validName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidName reports whether name can be used for a template, an agent or a
// team.
func ValidName(name string) bool {
	return validName.MatchString(name)
}

const frontMatterFence = "---"

// Parse reads a definition file: a front matter between "---" lines, then the
// prompt. Unknown fields are rejected so that a typo in a hand-edited file
// does not silently drop a setting.
func Parse(data []byte) (Definition, error) {
	header, body, err := splitFrontMatter(string(data))
	if err != nil {
		return Definition{}, err
	}

	var d Definition

	dec := yaml.NewDecoder(strings.NewReader(header))
	dec.KnownFields(true)

	// An empty front matter decodes to io.EOF: it is a valid, empty header.
	if err := dec.Decode(&d); err != nil && header != "" {
		return Definition{}, fmt.Errorf("front matter: %w", err)
	}

	d.Prompt = strings.TrimSpace(body)

	if err := d.Validate(); err != nil {
		return Definition{}, err
	}

	return d, nil
}

// splitFrontMatter separates the YAML header from the markdown body and
// returns them in that order.
func splitFrontMatter(s string) (string, string, error) {
	s = strings.ReplaceAll(s, "\r\n", "\n")

	rest, ok := strings.CutPrefix(s, frontMatterFence+"\n")
	if !ok {
		return "", "", errors.New(`file must start with a "---" line opening the front matter`)
	}

	if body, ok := strings.CutPrefix(rest, frontMatterFence+"\n"); ok {
		return "", body, nil
	}

	if header, body, ok := strings.Cut(rest, "\n"+frontMatterFence+"\n"); ok {
		return header, body, nil
	}

	if header, ok := strings.CutSuffix(rest, "\n"+frontMatterFence); ok {
		return header, "", nil
	}

	return "", "", errors.New(`front matter is not closed by a "---" line`)
}

// Format writes a definition back to its file form.
func Format(d Definition) ([]byte, error) {
	header, err := MarshalYAML(d)
	if err != nil {
		return nil, fmt.Errorf("encode front matter: %w", err)
	}

	var b bytes.Buffer

	b.WriteString(frontMatterFence + "\n")

	// An empty definition marshals to "{}", which is not worth writing.
	if s := string(header); s != "{}\n" {
		b.WriteString(s)
	}

	b.WriteString(frontMatterFence + "\n")

	if d.Prompt != "" {
		b.WriteString(strings.TrimSpace(d.Prompt))
		b.WriteByte('\n')
	}

	return b.Bytes(), nil
}

// yamlIndent matches the usual style of hand-written YAML files.
const yamlIndent = 2

// MarshalYAML encodes v the way configuration files are written by hand
// (two-space indentation), since users edit them directly.
func MarshalYAML(v any) ([]byte, error) {
	var b bytes.Buffer

	enc := yaml.NewEncoder(&b)
	enc.SetIndent(yamlIndent)

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Validate checks the values that have a fixed set of choices.
func (d Definition) Validate() error {
	var errs []error

	if p := d.Permissions.Profile; p != "" && !slices.Contains(profiles, p) {
		errs = append(errs, fmt.Errorf("permissions.profile %q: use one of %s", p, strings.Join(profiles, ", ")))
	}

	for _, tool := range d.Tools {
		if strings.Contains(tool, ",") {
			errs = append(errs, fmt.Errorf("tools: %q cannot contain a comma (use one entry per tool)", tool))
		}
	}

	// Claude Code silently falls back to asking for everything when auto mode
	// is unavailable for the model, which Haiku is (checked by task contract).
	if d.Permissions.Profile == ProfileAuto && strings.Contains(strings.ToLower(d.Model), "haiku") {
		errs = append(errs, errors.New("permissions.profile auto is not available with Haiku models: use sonnet or opus, or another profile"))
	}

	for _, e := range d.MCPExclude {
		if _, _, err := SplitExclusion(e); err != nil {
			errs = append(errs, err)
		}
	}

	errs = append(errs, validateSkills(d.Skills, d.SkillsExclude)...)

	if d.Version < 0 {
		errs = append(errs, fmt.Errorf("version %d: must be positive", d.Version))
	}

	return errors.Join(errs...)
}

// skillName is Claude Code's rule for skill names (skills.ValidName; not
// imported, to keep the file format free of the skills package).
var skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// validateSkills checks the names in skills and skills_exclude, and refuses
// a skill both preloaded and excluded.
func validateSkills(preload, exclude []string) []error {
	var errs []error

	for field, names := range map[string][]string{"skills": preload, "skills_exclude": exclude} {
		for _, name := range names {
			if !skillName.MatchString(name) {
				errs = append(errs, fmt.Errorf("%s: %q is not a skill name (lowercase letters, digits and hyphens)", field, name))
			}
		}
	}

	for _, name := range preload {
		if slices.Contains(exclude, name) {
			errs = append(errs, fmt.Errorf("skill %q is both preloaded (skills) and excluded (skills_exclude): choose one", name))
		}
	}

	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

	return errs
}

// exclusionPart is a server or tool name in an mcp_exclude entry.
var exclusionPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// SplitExclusion splits an mcp_exclude entry into its server and, for a
// single tool, the tool: "gitlab" excludes the whole server,
// "gitlab/delete_project" one tool.
func SplitExclusion(entry string) (string, string, error) {
	server, tool, isTool := strings.Cut(entry, "/")
	if !exclusionPart.MatchString(server) || (isTool && !exclusionPart.MatchString(tool)) {
		return "", "", fmt.Errorf("mcp_exclude: %q: use a server name, or server/tool", entry)
	}

	return server, tool, nil
}
