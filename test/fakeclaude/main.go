// Command fakeclaude stands in for the claude CLI in end-to-end tests: it
// lets the whole daemon run multi-agent scenarios for free and
// deterministically, with no model behind it.
//
// It accepts the same options Nobody Inc passes to claude and plays the same
// part in the protocol:
//
//   - a session start, new or resumed, runs the SessionStart hook, which
//     hands over the preloaded skills;
//   - every turn (the initial prompt, then each line typed into its terminal)
//     runs the UserPromptSubmit hook, then the Stop hook until no more mail
//     is handed over, exactly as Claude Code does;
//   - the hooks are the real nobody-hook binary from --settings;
//   - messages are sent with the real send_message tool, over the MCP server
//     given in --mcp-config.
//
// What it does with its mail follows fixed rules (see react):
//
//   - a request whose body is "ASK <address>: <question>" makes it send that
//     question as a query to <address>;
//   - a request whose body is "WORK <seconds>" keeps it busy that long;
//   - a query gets an inform answer; the query "SYSTEM PROMPT?" is answered
//     with the system prompt it was started with (--append-system-prompt),
//     and "PRELOADED SKILLS?" with how the session started and what its
//     SessionStart hook handed over;
//   - an inform answering one of its queries is forwarded, as done, to whoever
//     made the original request;
//   - when send_message fails, the error is escalated to the human;
//   - a notice from nobody about a ticket in its team's queue makes it claim
//     the ticket, commit a <ticket>.md file in its worktree, attach a
//     deliverable and close it; same for a ticket assigned to it (without the
//     claim).
//
// Costs (ADR-0013): like Claude Code, it hands its statusLine (from
// --settings) the session's running cost, once when the session starts (0)
// and after every turn. A new process starts from 0, a resumed session too,
// with the same session ID. It spends only when told to:
//
//   - a request whose body is "SPEND <usd>" costs that much;
//   - a ticket whose title has "SPEND <usd>" costs that much, and stays in
//     progress (the work is not over);
//   - a Stop refused for a budget makes it call budget_request (the request
//     named in the refusal, $1, a fixed justification);
//   - a notice from nobody about a budget request (the director's) makes it
//     pass the request on with budget_submit, recommending $1.
//
// "claude mcp add-json" and "claude mcp remove" change MCP servers in the
// same files as the real CLI (mcpcli.go).
//
// Started with --fork-session (a fork answering an urgent question), it prints
// a one-shot JSON answer like claude -p does: ESCALATE if the question
// contains "UNKNOWN", an answer otherwise; either costs forkCostUSD.
//
// Started with stream-json output and the API out of reach, as the daemon
// does to discover the built-in skills, it prints an init event listing the
// project's skills and one built-in skill, then waits to be killed.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/hook"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "mcp" {
		return mcpCommand(args[1:])
	}

	opts, err := parseArgs(args)
	if err != nil {
		return err
	}

	if opts.discovery {
		return listSkills()
	}

	if opts.fork {
		return answerAsFork(opts.prompt)
	}

	a, err := newAgent(opts)
	if err != nil {
		return err
	}
	defer a.mcp.Close()

	a.startSession()
	a.reportCost() // the session is ready: nothing spent yet
	a.turn(opts.prompt)

	// Lines typed into the terminal (the daemon's wake-up prompt, or a human)
	// each start a new turn, like a prompt submitted in Claude Code.
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		if prompt := strings.TrimSpace(lines.Text()); prompt != "" {
			a.turn(prompt)
		}
	}

	return lines.Err()
}

// options are the parts of the claude command line we use.
type options struct {
	hooks map[string]string // hook event → command
	// statusLine is the statusLine command of --settings, run through the
	// shell as Claude Code does ("nobody-hook statusline").
	statusLine string
	mcpURL     string
	prompt     string
	fork       bool
	// systemPrompt is --append-system-prompt.
	systemPrompt string
	// discovery is the daemon reading the skills a session loads: stream-json
	// output with the API out of reach (skills.DiscoverBuiltin).
	discovery    bool
	outputFormat string
	// resume is the session --resume continues; "" for a new one.
	resume string
}

func parseArgs(args []string) (options, error) {
	var opts options

	takesValue := map[string]bool{
		"--settings": true, "--mcp-config": true, "--append-system-prompt": true,
		"--setting-sources": true, "--allowedTools": true, "--model": true,
		"--resume": true, "--tools": true, "--output-format": true, "-p": true,
		"--permission-mode": true, "--add-dir": true, "--disallowedTools": true,
	}

	flags := map[string]bool{"--fork-session": true, "--strict-mcp-config": true, "--verbose": true}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if flags[arg] {
			opts.fork = opts.fork || arg == "--fork-session"

			continue
		}

		if !takesValue[arg] {
			opts.prompt = arg // the last positional argument is the first prompt

			continue
		}

		if i+1 >= len(args) {
			return opts, fmt.Errorf("%s needs a value", arg)
		}

		i++

		if err := opts.set(arg, args[i]); err != nil {
			return opts, err
		}
	}

	opts.discovery = opts.outputFormat == "stream-json" && os.Getenv("ANTHROPIC_BASE_URL") == "http://127.0.0.1:1"

	return opts, nil
}

// fakeBuiltinSkill is the one built-in skill fakeclaude has.
const fakeBuiltinSkill = "fake-builtin"

// listSkills prints the init event of a session: the project's skills
// (.claude/skills of the working directory) and fakeBuiltinSkill. Then it
// waits, as the real claude keeps retrying the unreachable API, until the
// daemon kills it.
func listSkills() error {
	names := []string{fakeBuiltinSkill}

	entries, _ := os.ReadDir(filepath.Join(".claude", "skills")) // no skills directory: no project skill
	for _, e := range entries {
		names = append(names, e.Name())
	}

	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "system", "subtype": "init", "skills": names}); err != nil {
		return err
	}

	select {}
}

// set records the value of an option we use; the others are ignored.
func (opts *options) set(name, value string) error {
	var err error

	switch name {
	case "-p":
		opts.prompt = value
	case "--settings":
		if opts.hooks, err = parseHooks(value); err == nil {
			opts.statusLine, err = parseStatusLine(value)
		}
	case "--mcp-config":
		opts.mcpURL, err = parseMCPURL(value)
	case "--append-system-prompt":
		opts.systemPrompt = value
	case "--output-format":
		opts.outputFormat = value
	case "--resume":
		opts.resume = value
	}

	return err
}

func parseHooks(settings string) (map[string]string, error) {
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(settings), &s); err != nil {
		return nil, fmt.Errorf("parse --settings: %w", err)
	}

	hooks := map[string]string{}

	for event, groups := range s.Hooks {
		if len(groups) > 0 && len(groups[0].Hooks) > 0 {
			hooks[event] = groups[0].Hooks[0].Command
		}
	}

	return hooks, nil
}

// parseStatusLine finds the statusLine command in the --settings value.
func parseStatusLine(settings string) (string, error) {
	var s struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"` //nolint:tagliatelle // field name imposed by Claude Code
	}
	if err := json.Unmarshal([]byte(settings), &s); err != nil {
		return "", fmt.Errorf("parse --settings: %w", err)
	}

	return s.StatusLine.Command, nil
}

// parseMCPURL finds the office server's URL in the --mcp-config value: a
// file path, as the daemon passes it, or inline JSON.
func parseMCPURL(config string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(config), "{") {
		data, err := os.ReadFile(config) //nolint:gosec // the path the daemon passes as --mcp-config
		if err != nil {
			return "", fmt.Errorf("read --mcp-config: %w", err)
		}

		config = string(data)
	}

	var c struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"` //nolint:tagliatelle // field name imposed by Claude Code
	}
	if err := json.Unmarshal([]byte(config), &c); err != nil {
		return "", fmt.Errorf("parse --mcp-config: %w", err)
	}

	return c.MCPServers["nobody"].URL, nil
}

// connectTimeout bounds the connection to the office MCP server.
const connectTimeout = 10 * time.Second

// sayf draws a line in the fake's terminal, like Claude Code drawing its UI.
func sayf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, format+"\r\n", args...)
}

// agent is one fake Claude Code session.
type agent struct {
	opts options
	mcp  *mcp.ClientSession
	// origins maps a query we sent to the request that made us send it, so the
	// answer can be forwarded back.
	origins map[string]mail
	// preloaded is how the session started and what its SessionStart hook
	// handed over, for the "PRELOADED SKILLS?" query.
	preloaded string
	// spentUSD is the session's running cost, what the statusLine is told.
	spentUSD float64
}

func newAgent(opts options) (*agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	transport := &mcp.StreamableClientTransport{Endpoint: opts.mcpURL, DisableStandaloneSSE: true}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "fakeclaude"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to the office MCP: %w", err)
	}

	return &agent{opts: opts, mcp: session, origins: map[string]mail{}}, nil
}

// startSession runs the SessionStart hook, as Claude Code does when a
// session starts or is resumed, and keeps what it hands over.
func (a *agent) startSession() {
	source := "startup"
	if a.opts.resume != "" {
		source = "resume"
	}

	a.preloaded = "source=" + source + "\n"
	if resp := a.runHookFrom(hook.EventSessionStart, source); resp != nil && resp.HookSpecificOutput != nil {
		a.preloaded += resp.HookSpecificOutput.AdditionalContext
	}
}

// turn plays one Claude Code turn.
func (a *agent) turn(prompt string) {
	sayf("> %s", prompt)

	if resp := a.runHook(hook.EventUserPromptSubmit); resp != nil && resp.HookSpecificOutput != nil {
		a.react(resp.HookSpecificOutput.AdditionalContext)
	}
	// Claude Code keeps going as long as the Stop hook blocks.
	for {
		resp := a.runHook(hook.EventStop)
		if resp == nil || resp.Decision != "block" {
			break
		}

		if request := budgetRequestIn.FindStringSubmatch(resp.Reason); request != nil {
			a.justify(request[1])

			continue
		}

		a.react(resp.Reason)
	}

	a.reportCost()
	sayf("turn finished")
}

// budgetRequestIn finds the request a Stop refused for a budget names
// (daemon.budgetRequestPrompt).
var budgetRequestIn = regexp.MustCompile(`call the budget_request tool \(request (BR-\d+)\)`)

// justify answers a Stop refused for a budget, as an agent following the
// refusal would: it asks for $1 more with budget_request.
func (a *agent) justify(request string) {
	sayf("budget reached: asking for an extension (%s)", request)
	a.call("budget_request", map[string]any{
		"request": request, "amount_usd": 1, "justification": "Half done; " + os.Getenv("NOBODY_AGENT") + " needs a little more.",
	})
}

// spend adds to the session's running cost and tells the statusLine, as
// Claude Code does after each message.
func (a *agent) spend(usd float64) {
	a.spentUSD += usd
	sayf("spent $%.2f (session total $%.2f)", usd, a.spentUSD)
	a.reportCost()
}

// reportCost hands the statusLine the session's running cost, in the shape
// Claude Code gives it (only the fields Nobody Inc reads, and the model).
func (a *agent) reportCost() {
	if a.opts.statusLine == "" {
		return
	}

	input, err := json.Marshal(map[string]any{
		"session_id": "fake-session-" + os.Getenv("NOBODY_AGENT"),
		"model":      map[string]string{"id": "fake"},
		"cost":       map[string]float64{"total_cost_usd": a.spentUSD},
	})
	if err != nil {
		return
	}

	cmd := exec.CommandContext(context.Background(), "sh", "-c", a.opts.statusLine) //nolint:gosec // the command comes from our own --settings
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = os.Stderr

	if out, err := cmd.Output(); err != nil || len(bytes.TrimSpace(out)) > 0 {
		sayf("statusLine: %v %q", err, out) // it must print nothing
	}
}

// runHook runs the hook configured for an event, as Claude Code does: the event
// as JSON on stdin, the answer as JSON on stdout.
func (a *agent) runHook(event string) *hook.Response {
	return a.runHookFrom(event, "")
}

// runHookFrom is runHook with the SessionStart source.
func (a *agent) runHookFrom(event, source string) *hook.Response {
	command, ok := a.opts.hooks[event]
	if !ok {
		return nil
	}

	session := "fake-session-" + os.Getenv("NOBODY_AGENT")
	cwd, _ := os.Getwd() // an unknown working directory: the daemon falls back to the project

	input, err := json.Marshal(hook.Event{Name: event, SessionID: session, Cwd: cwd, Source: source})
	if err != nil {
		return nil
	}

	cmd := exec.CommandContext(context.Background(), command) //nolint:gosec // the hook path comes from our own --settings
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil
	}

	var resp hook.Response
	if err := json.Unmarshal(out, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: bad hook answer:", err)

		return nil
	}

	return &resp
}

// mail is a message as read from the text the hooks hand over.
type mail struct {
	ID, From, InReplyTo, Act, Subject, Body, Ticket string
}

// react applies the fixed rules to the mail found in text.
func (a *agent) react(text string) {
	for _, m := range parseMail(text) {
		sayf("mail %s from %s [%s] %s", m.ID, m.From, m.Act, m.Subject)

		switch {
		case m.From == "nobody":
			a.reactToNotice(m)
		case m.Act == "request":
			a.reactToRequest(m)
		case m.Act == "query":
			a.answer(m)
		case m.Act == "inform":
			a.forwardAnswer(m)
		}
	}
}

// answer answers a query: with the system prompt when asked for it.
func (a *agent) answer(m mail) {
	body := "answer to: " + m.Body

	switch strings.TrimSpace(m.Body) {
	case "SYSTEM PROMPT?":
		body = a.opts.systemPrompt
	case "PRELOADED SKILLS?":
		body = a.preloaded
	}

	a.send(map[string]any{"to": m.From, "act": "inform", "subject": "Re: " + m.Subject, "body": body, "in_reply_to": m.ID})
}

// reactToRequest handles "ASK <address>: <question>", "WORK <seconds>" and
// "SPEND <usd>".
func (a *agent) reactToRequest(m mail) {
	switch {
	case strings.HasPrefix(m.Body, "ASK "):
		a.ask(m)
	case strings.HasPrefix(m.Body, "WORK "):
		seconds, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(m.Body, "WORK ")))
		time.Sleep(time.Duration(seconds) * time.Second)
	case strings.HasPrefix(m.Body, "SPEND "):
		if usd, ok := spendIn(m.Body); ok {
			a.spend(usd)
		}
	}
}

// spendPattern is "SPEND <usd>" in a request's body or a ticket's title.
var spendPattern = regexp.MustCompile(`SPEND (\d+(?:\.\d+)?)`)

func spendIn(text string) (float64, bool) {
	match := spendPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}

	usd, err := strconv.ParseFloat(match[1], 64)

	return usd, err == nil
}

// reactToNotice handles the system's notices: a budget request for the
// director, and tickets.
func (a *agent) reactToNotice(m mail) {
	if strings.HasPrefix(m.Subject, "Budget request BR-") {
		a.submit(m)

		return
	}

	switch {
	case m.Ticket == "":
		return
	case strings.Contains(m.Body, "waiting in your team's queue"):
		if a.call("ticket_claim", map[string]any{"id": m.Ticket}) {
			a.work(m)
		}
	case strings.Contains(m.Body, "is assigned to you"):
		a.work(m)
	}
}

// work does a ticket: one whose title says "SPEND <usd>" costs that much and
// stays in progress; any other is finished at once.
func (a *agent) work(m mail) {
	if usd, ok := spendIn(m.Body); ok {
		a.spend(usd)

		return
	}

	a.finish(m.Ticket)
}

// submit passes a budget request on to the human, as the director does with
// budget_submit: an opinion, a recommendation, and a justification and an
// amount for a request nobody justified.
func (a *agent) submit(m mail) {
	request, _, _ := strings.Cut(strings.TrimPrefix(m.Subject, "Budget request "), ":")
	a.call("budget_submit", map[string]any{
		"request": request, "opinion": "Worth finishing.", "recommended_usd": 1,
		"justification": "The work is not over.", "amount_usd": 1,
	})
}

// forwardAnswer sends the answer to one of our queries back to whoever made
// the original request.
func (a *agent) forwardAnswer(m mail) {
	if origin, ok := a.origins[m.InReplyTo]; ok {
		a.send(map[string]any{"to": origin.From, "act": "done", "subject": "Re: " + origin.Subject,
			"body": m.Body, "in_reply_to": origin.ID})
	}
}

// ask handles "ASK <address>: <question>".
func (a *agent) ask(request mail) {
	target, question, ok := strings.Cut(strings.TrimPrefix(request.Body, "ASK "), ":")
	if !ok {
		return
	}

	if id := a.send(map[string]any{"to": strings.TrimSpace(target), "act": "query",
		"subject": request.Subject, "body": strings.TrimSpace(question)}); id != "" {
		a.origins[id] = request
	}
}

// finish attaches a deliverable to a ticket and closes it.
func (a *agent) finish(ticket string) {
	commitWork(ticket)

	if a.call("deliverable_add", map[string]any{"id": ticket, "kind": "note", "summary": "done by " + os.Getenv("NOBODY_AGENT")}) {
		a.call("ticket_update", map[string]any{"id": ticket, "status": "done"})
	}
}

// workFilePerm is the permission of the files the fake writes.
const workFilePerm = 0o600

// ticketID is the shape of a ticket ID, checked before using it in a file name.
var ticketID = regexp.MustCompile(`^T-\d+$`)

// commitWork writes <ticket>.md in the current directory (the team's
// worktree) and commits it, as a real agent commits its work before closing
// a ticket. Outside a git repository (no active feature) it does nothing.
func commitWork(ticket string) {
	if !ticketID.MatchString(ticket) {
		return
	}

	agent := os.Getenv("NOBODY_AGENT")

	content := []byte(ticket + " done by " + agent + "\n")
	if err := os.WriteFile(ticket+".md", content, workFilePerm); err != nil { //nolint:gosec // ticket checked as T-<n>
		return
	}

	for _, args := range [][]string{{"add", ticket + ".md"}, {"commit", "-m", "feat: " + ticket + " done by " + agent}} {
		if err := exec.CommandContext(context.Background(), "git", args...).Run(); err != nil { //nolint:gosec // fixed git subcommands
			sayf("git %s: %v", args[0], err)

			return
		}
	}
}

// call runs an office tool and reports whether it succeeded.
func (a *agent) call(tool string, args map[string]any) bool {
	res, err := a.mcp.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude:", tool, err)

		return false
	}

	if res.IsError {
		sayf("%s refused: %s", tool, textOf(res))

		return false
	}

	return true
}

// send calls send_message and returns the new message ID. A refused message
// is escalated to the human, as the system prompt tells real agents to do.
func (a *agent) send(args map[string]any) string {
	res, err := a.mcp.CallTool(context.Background(), &mcp.CallToolParams{Name: "send_message", Arguments: args})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: send_message:", err)

		return ""
	}

	text := textOf(res)
	if res.IsError {
		sayf("send_message refused: %s", text)

		if args["to"] != "human" {
			a.send(map[string]any{"to": "human", "act": "inform", "subject": "Could not send a message", "body": text})
		}

		return ""
	}

	var out struct {
		ID string `json:"id"`
	}

	_ = json.Unmarshal([]byte(text), &out) // the ID only matters for queries we track

	return out.ID
}

// forkCostUSD is what a fork costs, reported in its -p output.
const forkCostUSD = 0.01

// answerAsFork prints the one-shot answer of a fork, as claude -p would,
// with what it cost.
func answerAsFork(prompt string) error {
	answer := "fork answer"
	if strings.Contains(prompt, "UNKNOWN") {
		answer = "ESCALATE"
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"result": answer, "is_error": false, "session_id": "fake-fork", "total_cost_usd": forkCostUSD,
	})
}

// parseMail reads the messages out of the text rendered by internal/hook:
//
//	--- Message 1/2 ---
//	ID: …
//	From: …
//	In reply to: …   (optional)
//	Act: query (an answer is expected)
//	Subject: …
//	<blank line>
//	body…
func parseMail(text string) []mail {
	var messages []mail

	for _, block := range strings.Split(text, "\n--- Message ")[1:] {
		var m mail

		header, body, _ := strings.Cut(block, "\n\n")
		for line := range strings.SplitSeq(header, "\n") {
			key, value, ok := strings.Cut(line, ": ")
			if !ok {
				continue
			}

			switch key {
			case "ID":
				m.ID = value
			case "From":
				m.From = value
			case "In reply to":
				m.InReplyTo = value
			case "Act":
				m.Act, _, _ = strings.Cut(value, " ")
			case "Subject":
				m.Subject = value
			case "Ticket":
				m.Ticket = value
			}
		}

		m.Body = strings.TrimSpace(body)
		messages = append(messages, m)
	}

	return messages
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder

	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}

	return b.String()
}
