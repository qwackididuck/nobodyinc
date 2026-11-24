package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool is one tool an MCP server offers.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ListTimeout bounds how long ListTools waits for a server.
const ListTimeout = 20 * time.Second

// ErrUnsupported is returned for a server type Nobody Inc cannot reach.
var ErrUnsupported = errors.New("unsupported MCP server type")

// ListTools starts the server (stdio) or reaches it (http, sse), with its
// ${VARIABLE} references expanded from lookup, asks for its tools and stops
// it. A stdio server runs with environ as its environment. It lets the human
// choose the tools an agent does not get.
func ListTools(ctx context.Context, cfg Config, lookup func(string) (string, bool), environ []string) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, ListTimeout)
	defer cancel()

	transport, err := transportFor(ctx, cfg, lookup, environ)
	if err != nil {
		return nil, err
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "nobody-inc"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to the server: %w", err)
	}
	defer session.Close()

	tools := []Tool{}

	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list the server's tools: %w", err)
		}

		tools = append(tools, Tool{Name: tool.Name, Description: tool.Description})
	}

	slices.SortFunc(tools, func(a, b Tool) int {
		if a.Name < b.Name {
			return -1
		}

		return 1
	})

	return tools, nil
}

// transportFor builds the client transport of a server, its values expanded.
func transportFor(ctx context.Context, cfg Config, lookup func(string) (string, bool), environ []string) (mcp.Transport, error) {
	switch kind := TypeOf(cfg); kind {
	case "stdio":
		cmd, err := command(ctx, cfg, lookup, environ)
		if err != nil {
			return nil, err
		}

		return &mcp.CommandTransport{Command: cmd}, nil
	case "http", "sse":
		url, err := Expand(stringField(cfg, "url"), lookup)
		if err != nil {
			return nil, err
		}

		headers, err := expandMap(cfg["headers"], lookup)
		if err != nil {
			return nil, err
		}

		client := &http.Client{Transport: withHeaders{headers: headers}}
		if kind == "sse" {
			return &mcp.SSEClientTransport{Endpoint: url, HTTPClient: client}, nil
		}

		return &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, kind)
	}
}

// command is the process of a stdio server: its command and arguments, with
// environ (the daemon's environment, with the project's variables) plus the
// server's own env.
func command(ctx context.Context, cfg Config, lookup func(string) (string, bool), environ []string) (*exec.Cmd, error) {
	name, err := Expand(stringField(cfg, "command"), lookup)
	if err != nil {
		return nil, err
	}

	var args []string

	if list, ok := cfg["args"].([]any); ok {
		for _, a := range list {
			arg, err := Expand(fmt.Sprint(a), lookup)
			if err != nil {
				return nil, err
			}

			args = append(args, arg)
		}
	}

	env, err := expandMap(cfg["env"], lookup)
	if err != nil {
		return nil, err
	}

	// The command is the user's own server configuration, run as they would.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = slices.Clone(environ)

	for _, key := range slices.Sorted(maps.Keys(env)) {
		cmd.Env = append(cmd.Env, key+"="+env[key])
	}

	return cmd, nil
}

// TypeOf is a server's transport: its "type", or stdio when it has none
// (Claude Code's default).
func TypeOf(cfg Config) string {
	if t := stringField(cfg, "type"); t != "" {
		return t
	}

	return "stdio"
}

func stringField(cfg Config, key string) string {
	s, _ := cfg[key].(string)

	return s
}

// expandMap expands the values of an env or headers object.
func expandMap(v any, lookup func(string) (string, bool)) (map[string]string, error) {
	out := map[string]string{}

	m, _ := v.(map[string]any)
	for key, value := range m {
		expanded, err := Expand(fmt.Sprint(value), lookup)
		if err != nil {
			return nil, err
		}

		out[key] = expanded
	}

	return out, nil
}

// withHeaders adds a server's headers to every request.
type withHeaders struct {
	headers map[string]string
}

func (w withHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for key, value := range w.headers {
		r.Header.Set(key, value)
	}

	return http.DefaultTransport.RoundTrip(r)
}
