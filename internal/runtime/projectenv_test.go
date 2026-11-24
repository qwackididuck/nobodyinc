package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/projectenv"
)

// projectenv refuses what runtime removes from the agents' environment: the
// two lists must not drift apart.
func TestParentSessionVarsAreReserved(t *testing.T) {
	t.Parallel()

	for _, name := range parentSessionVars {
		if !projectenv.Reserved(name) {
			t.Errorf("%s is removed from the agents' environment but a project may set it", name)
		}
	}

	for _, prefix := range parentSessionPrefixes {
		if !projectenv.Reserved(prefix + "X") {
			t.Errorf("%sX is removed from the agents' environment but a project may set it", prefix)
		}
	}
}

// The project's variables replace the daemon's for the agent's process only:
// the agent's own variables stay out of reach.
func TestProjectEnvReachesTheAgent(t *testing.T) {
	store, err := projectenv.Open(filepath.Join(t.TempDir(), projectenv.FileName))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Set("ANTHROPIC_API_KEY", "project-key"); err != nil {
		t.Fatal(err)
	}

	l := Launch{HookPath: "/bin/nobody-hook", DaemonURL: "http://d", MCPConfigDir: t.TempDir(), WorkDir: func(string) string { return t.TempDir() }, ProjectEnv: store.Overlay}
	t.Setenv("ANTHROPIC_API_KEY", "user-key")

	o := testOrg()

	m, _ := o.Member("archi@specs")

	cmd, err := l.Command(context.Background(), o, m, Resume{})
	if err != nil {
		t.Fatal(err)
	}

	if got := lastValue(cmd.Env, "ANTHROPIC_API_KEY"); got != "project-key" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want the project's", got)
	}

	if got := lastValue(cmd.Env, "NOBODY_AGENT"); got != "archi@specs" {
		t.Fatalf("NOBODY_AGENT = %q", got)
	}
}

// lastValue is the value exec gives a variable set several times: the last.
func lastValue(env []string, name string) string {
	value := ""

	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			value = v
		}
	}

	return value
}
