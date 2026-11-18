package hook

import (
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/skills"
)

// Texts shown to the agent before its mail. They are written for the model:
// short, explicit about what is expected.
const (
	promptIntro = "Mail from your colleagues is attached below. Read it and act on it: answer it, " +
		"do the work asked, or explain why no action is needed."
	stopIntro = "Before ending your turn, read the mail below and act on it: answer it, " +
		"adjust your work, or explain why no action is needed."
	urgentIntro = "URGENT mail arrived while you were working. Take it into account now, " +
		"then continue your current task."
	howToReply = "To answer a message, use the send_message tool with in_reply_to set to its ID."
)

// renderMail turns messages into the plain text the agent will read.
func renderMail(intro string, messages []messaging.Message) string {
	var b strings.Builder
	b.WriteString(intro)
	b.WriteString("\n")
	b.WriteString(howToReply)
	b.WriteString("\n")

	for i, m := range messages {
		fmt.Fprintf(&b, "\n--- Message %d/%d", i+1, len(messages))

		if m.Priority == messaging.Urgent {
			b.WriteString(" (URGENT)")
		}

		b.WriteString(" ---\n")
		fmt.Fprintf(&b, "ID: %s\n", m.ID)
		fmt.Fprintf(&b, "From: %s\n", m.From)

		if m.InReplyTo != "" {
			fmt.Fprintf(&b, "In reply to: %s\n", m.InReplyTo)
		}

		fmt.Fprintf(&b, "Act: %s%s\n", m.Act, replyHint(m))

		if m.Ticket != "" {
			fmt.Fprintf(&b, "Ticket: %s\n", m.Ticket)
		}

		fmt.Fprintf(&b, "Subject: %s\n\n", m.Subject)
		b.WriteString(m.Body)
		b.WriteString("\n")
	}

	return b.String()
}

func replyHint(m messaging.Message) string {
	switch {
	case m.Act.RequiresReply():
		return " (an answer is expected)"
	case m.From == org.SystemAddress:
		return " (a notice from the system: do not reply, but do what it asks)"
	default:
		return " (no answer needed: do not reply)"
	}
}

// preloadIntro opens the preloaded skills. They come at every session start
// (startup, resume, compaction), so the agent is told they replace any
// earlier copy.
const preloadIntro = "Skills preloaded for you (your agent file lists them under skills:). " +
	"Follow them whenever they apply; you do not need to invoke them with the Skill tool. " +
	"If an earlier copy of a skill appears in this conversation, this one replaces it. " +
	"Paths in a skill are relative to its dir."

// SessionStartResponse builds the answer to a SessionStart event, as extra
// context: reopen, the context of a session resumed because the project
// opened again (ADR-0015, "" for none), then the instructions of the skills
// the agent preloads (ADR-0011); nil when there is neither.
func SessionStartResponse(preloaded []skills.Preloaded, reopen string) *Response {
	if len(preloaded) == 0 && reopen == "" {
		return nil
	}

	var b strings.Builder

	if reopen != "" {
		b.WriteString(reopen)
		b.WriteString("\n")
	}

	if len(preloaded) > 0 {
		if reopen != "" {
			b.WriteString("\n")
		}

		b.WriteString(preloadIntro)
		b.WriteString("\n")

		for _, p := range preloaded {
			fmt.Fprintf(&b, "\n<preloaded_skill name=%q dir=%q>\n%s\n</preloaded_skill>\n", p.Name, p.Dir, p.Body)
		}
	}

	return &Response{HookSpecificOutput: &SpecificOutput{
		HookEventName:     EventSessionStart,
		AdditionalContext: b.String(),
	}}
}
