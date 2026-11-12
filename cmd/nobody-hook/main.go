// Command nobody-hook hands an agent its mail. Claude Code runs it on hook
// events (Stop, PostToolUse); it relays them to the daemon, which owns the
// mailboxes and decides what to say (see internal/hook and docs/hooks.md):
//
//  1. read the event JSON sent by Claude Code on stdin;
//  2. send it to the daemon, which answers with what to tell the agent;
//  3. print that answer as JSON on stdout for Claude Code;
//  4. only then acknowledge the delivered messages, so mail is never lost.
//
// Configuration comes from the environment the daemon starts the agent with:
//
//	NOBODY_AGENT       the agent's address, e.g. "archi@specs" (required)
//	NOBODY_DAEMON_URL  the daemon's base URL (default http://127.0.0.1:7420)
//
// Exit codes follow Claude Code's convention: 0 on success, 1 on error. Exit
// code 1 is a non-blocking error: Claude Code shows it but the agent keeps
// working, which is what we want if the daemon is unreachable.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const defaultDaemonURL = "http://127.0.0.1:7420"

func main() {
	cfg := config{
		agent:     os.Getenv("NOBODY_AGENT"),
		daemonURL: os.Getenv("NOBODY_DAEMON_URL"),
	}
	// Long enough for a permission request waiting for the human; Claude Code
	// kills the hook at its own timeout anyway.
	if err := run(context.Background(), os.Stdin, os.Stdout, cfg, &http.Client{Timeout: 11 * time.Minute}); err != nil {
		fmt.Fprintln(os.Stderr, "nobody-hook:", err)
		os.Exit(1)
	}
}

type config struct {
	agent     string
	daemonURL string
}

// decision mirrors the daemon's answer (HookDecision in api/openapi.yaml).
type decision struct {
	Response   json.RawMessage `json:"response,omitempty"`
	MessageIDs []string        `json:"message_ids"`
}

func run(ctx context.Context, stdin io.Reader, stdout io.Writer, cfg config, client *http.Client) error {
	if cfg.agent == "" {
		return errors.New("NOBODY_AGENT is not set")
	}

	if cfg.daemonURL == "" {
		cfg.daemonURL = defaultDaemonURL
	}

	hookURL := cfg.daemonURL + "/internal/agents/" + url.PathEscape(cfg.agent) + "/hook"

	event, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read hook event: %w", err)
	}

	var d decision
	if err := postJSON(ctx, client, hookURL, event, &d); err != nil {
		return err
	}

	if len(d.Response) == 0 {
		return nil // nothing to tell the agent
	}

	if _, err := stdout.Write(append(d.Response, '\n')); err != nil {
		return fmt.Errorf("write hook response: %w", err)
	}

	ack, err := json.Marshal(map[string][]string{"message_ids": d.MessageIDs})
	if err != nil {
		return fmt.Errorf("encode acknowledgement: %w", err)
	}

	return postJSON(ctx, client, hookURL+"/ack", ack, nil)
}

// maxErrorBody bounds how much of a daemon error is quoted in our own error.
const maxErrorBody = 1024

// postJSON posts body to endpoint and decodes the JSON answer into out (if not nil).
func postJSON(ctx context.Context, client *http.Client, endpoint string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build daemon request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody)) // best effort, for the error message only

		return fmt.Errorf("daemon answered %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode daemon answer: %w", err)
	}

	return nil
}
