package skills

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"sync"
	"time"
)

// unreachableAPI points Claude Code at an address nothing listens on. A
// session started with it still emits its init event, which lists every
// skill, then fails to reach the model: nothing leaves the machine and
// nothing is billed. Killing a normal session right after its init event
// would not do, as its first API request leaves slightly earlier (checked
// by test/contract).
const unreachableAPI = "ANTHROPIC_BASE_URL=http://127.0.0.1:1"

// discoveryTimeout bounds how long DiscoverBuiltin waits for the init event.
const discoveryTimeout = 30 * time.Second

// Discovery describes how to start claude to read the skills it loads.
type Discovery struct {
	// ClaudePath is the claude executable; "claude" from PATH when empty.
	ClaudePath string
	// Args are the agents' settings flags (--setting-sources...), so that
	// the session loads what an agent would.
	Args []string
	// Dir is the directory the session starts in: the project's main
	// checkout.
	Dir string
	// Env is the environment of the session.
	Env []string
}

// DiscoverBuiltin starts claude with the API out of reach, reads the skills
// listed by its init event, and kills it. The built-in skills are those that
// are neither project nor personal skills (known).
func DiscoverBuiltin(ctx context.Context, d Discovery, known Found) ([]string, error) {
	names, err := initSkills(ctx, d)
	if err != nil {
		return nil, err
	}

	builtin := make([]string, 0, len(names))

	for _, name := range names {
		_, project := known.Get(ScopeProject, name)
		_, personal := known.Get(ScopePersonal, name)

		if !project && !personal && !slices.Contains(builtin, name) {
			builtin = append(builtin, name)
		}
	}

	slices.Sort(builtin)

	return builtin, nil
}

// initSkills runs the session and returns the skills of its init event.
func initSkills(ctx context.Context, d Discovery) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	claude := d.ClaudePath
	if claude == "" {
		claude = "claude"
	}

	args := append([]string{
		"-p", "List nothing.", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
	}, d.Args...)

	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = d.Dir
	cmd.Env = append(slices.Clone(d.Env), unreachableAPI)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("discover built-in skills: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("discover built-in skills: start claude: %w", err)
	}

	defer func() {
		_ = cmd.Process.Kill() // it would keep retrying the unreachable API
		_ = cmd.Wait()         // killed on purpose: its exit status says nothing
	}()

	// The init event is one long line: it lists every tool, skill and command.
	const initialBuffer, maxLine = 1 << 20, 10 << 20

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, initialBuffer), maxLine)

	for scanner.Scan() {
		var ev struct {
			Type    string   `json:"type"`
			Subtype string   `json:"subtype"`
			Skills  []string `json:"skills"`
		}

		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Type == "system" && ev.Subtype == "init" {
			return ev.Skills, nil
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("discover built-in skills: no answer from claude in %s", discoveryTimeout)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("discover built-in skills: read claude's output: %w", err)
	}

	return nil, errors.New("discover built-in skills: claude ended without listing its skills")
}

// Builtin keeps the last list of built-in skills, read at start-up and
// refreshed on demand.
type Builtin struct {
	discover func(context.Context) ([]string, error)

	mu    sync.Mutex
	names []string
	err   error
}

// NewBuiltin returns an empty list that discover fills.
func NewBuiltin(discover func(context.Context) ([]string, error)) *Builtin {
	return &Builtin{discover: discover, err: errors.New("built-in skills not discovered yet")}
}

// Refresh discovers the built-in skills again. On failure, the previous list
// is kept and the error remembered.
func (b *Builtin) Refresh(ctx context.Context) {
	names, err := b.discover(ctx)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.err = err
	if err == nil {
		b.names = names
	}
}

// Names returns the last list, and the error of the last refresh.
func (b *Builtin) Names() ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.names), b.err
}
