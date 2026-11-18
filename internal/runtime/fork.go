package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// escalate is what a fork answers when the question is not settled yet.
const escalate = "ESCALATE"

const forkTimeout = 2 * time.Minute

// forkPrompt is the question asked to the fork of the busy agent's session.
const forkPrompt = `%s is waiting for a quick answer while you are busy with another task.
Their question (%s): %s

Answer only from decisions and facts already present in this conversation, in a few sentences.
If the answer has not been decided or stated here yet, answer exactly %s and nothing else.`

// Forker answers urgent questions sent to busy agents without disturbing them.
//
// It forks the agent's Claude Code session (claude -p --resume <session>
// --fork-session --tools ""): the fork sees everything the agent knows, has
// no tools, and leaves the original session untouched. Its answer goes back
// to the asker at once, and the agent gets a notice so it can correct it.
//
// The question leaves the agent's tray while the fork thinks, so the agent
// does not get it from its hooks in the meantime and answer it twice. When
// the fork answers ESCALATE (or fails), the question goes back to the tray
// and follows the usual path.
type Forker struct {
	Org        org.Source
	Statuses   *Statuses
	Logger     *slog.Logger
	ClaudePath string
	// WorkDir returns the directory the agent works in; the fork runs there.
	WorkDir func(address string) string
	// Send files messages; MarkDelivered and Requeue take the question out of
	// the agent's tray and put it back (usually messaging.Service.Send,
	// store.MarkDelivered and store.Requeue).
	Send          func(ctx context.Context, d messaging.Draft) (messaging.Message, error)
	MarkDelivered func(ctx context.Context, recipient string, ids []string, at time.Time) error
	Requeue       func(ctx context.Context, recipient string, ids []string) error
	// Run runs claude and returns its stdout; replaceable in tests.
	Run func(ctx context.Context, dir string, args []string) ([]byte, error)
	// RecordCost, when set, is told what each fork cost (its -p output's
	// total_cost_usd), with the question's sender: the asker pays for the
	// answer, not the agent forked (ADR-0013).
	RecordCost func(ctx context.Context, asker, session string, costUSD float64)
}

// ShouldAnswer reports whether a message is an urgent question for a busy
// agent whose session is known: the only case a fork is worth it. An idle
// agent is woken up and reads the question right away anyway.
func (f *Forker) ShouldAnswer(m messaging.Message) bool {
	return m.Act == messaging.ActQuery && m.Priority == messaging.Urgent &&
		f.Statuses.Busy(m.To) && f.Statuses.Session(m.To) != ""
}

// Answer forks the recipient's session to answer an urgent question.
func (f *Forker) Answer(ctx context.Context, q messaging.Message) {
	member, ok := f.Org.Current().Member(q.To)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, forkTimeout)
	defer cancel()

	if err := f.MarkDelivered(ctx, q.To, []string{q.ID}, time.Now().UTC()); err != nil {
		f.Logger.Warn("hold question for the fork", "agent", q.To, "err", err)

		return
	}

	answer, err := f.ask(ctx, member, q)
	if err == nil && answer != escalate {
		err = f.reply(ctx, q, answer)
		if err == nil {
			return
		}
	}

	// No usable answer: back to the tray, the agent will read it from its hooks.
	f.Logger.Info("fork did not answer, question back in the tray", "agent", q.To, "question", q.ID, "err", err)

	if err := f.Requeue(context.WithoutCancel(ctx), q.To, []string{q.ID}); err != nil {
		f.Logger.Error("requeue question", "agent", q.To, "question", q.ID, "err", err)
	}
}

// ask runs the fork and returns its answer.
func (f *Forker) ask(ctx context.Context, m org.Member, q messaging.Message) (string, error) {
	args := []string{
		"-p", fmt.Sprintf(forkPrompt, q.From, q.Subject, q.Body, escalate),
		"--resume", f.Statuses.Session(q.To), "--fork-session",
		"--tools", "",
		"--setting-sources", "project,local", // same isolation as agents (ADR-0003)
		"--strict-mcp-config", // and no MCP server: the fork only answers from memory
		"--output-format", "json",
	}
	if m.Agent.Model != "" {
		args = append(args, "--model", m.Agent.Model)
	}

	out, err := f.run(ctx, q, args)
	if err != nil {
		return "", err
	}

	var result struct {
		Result       string  `json:"result"`
		IsError      bool    `json:"is_error"`
		SessionID    string  `json:"session_id"`
		TotalCostUSD float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("decode fork output: %w", err)
	}

	// Paid whatever the answer: an ESCALATE or an error cost money too.
	if f.RecordCost != nil && result.TotalCostUSD > 0 {
		f.RecordCost(ctx, q.From, result.SessionID, result.TotalCostUSD)
	}

	if result.IsError {
		return "", fmt.Errorf("fork reported an error: %s", result.Result)
	}

	answer := strings.TrimSpace(result.Result)
	if answer == "" || strings.EqualFold(strings.Trim(answer, ".`* "), escalate) {
		return escalate, nil
	}

	return answer, nil
}

// reply sends the fork's answer to the asker, takes the question out of the
// agent's tray, and leaves the agent a notice to review the answer.
func (f *Forker) reply(ctx context.Context, q messaging.Message, answer string) error {
	sent, err := f.Send(ctx, messaging.Draft{
		From:      q.To,
		To:        q.From,
		Act:       messaging.ActInform,
		Priority:  messaging.Urgent,
		Subject:   "Re: " + q.Subject,
		Body:      answer + "\n\n(Quick answer from " + q.To + "'s context while they were busy; they may follow up with a correction.)",
		InReplyTo: q.ID,
	})
	if err != nil {
		return err
	}

	_, err = f.Send(ctx, messaging.Draft{
		From:     org.SystemAddress,
		To:       q.To,
		Act:      messaging.ActInform,
		Priority: messaging.Normal,
		Subject:  "Answered on your behalf: " + q.Subject,
		Body: fmt.Sprintf("While you were busy, %s asked (message %s):\n%s\n\nA fork of your session answered (message %s):\n%s\n\n"+
			"If this answer is wrong or incomplete, send a correction to %s with in_reply_to set to %s. Otherwise do nothing.",
			q.From, q.ID, q.Body, sent.ID, answer, q.From, q.ID),
	})

	return err
}

func (f *Forker) run(ctx context.Context, q messaging.Message, args []string) ([]byte, error) {
	if f.Run != nil {
		return f.Run(ctx, f.WorkDir(q.To), args)
	}

	claude := f.ClaudePath
	if claude == "" {
		claude = "claude"
	}

	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = f.WorkDir(q.To)
	cmd.Env = withoutParentSession(os.Environ())

	return cmd.Output()
}
