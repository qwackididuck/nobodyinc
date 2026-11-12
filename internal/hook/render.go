package hook

import (
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
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

		fmt.Fprintf(&b, "Act: %s%s\n", m.Act, replyHint(m.Act))

		if m.Ticket != "" {
			fmt.Fprintf(&b, "Ticket: %s\n", m.Ticket)
		}

		fmt.Fprintf(&b, "Subject: %s\n\n", m.Subject)
		b.WriteString(m.Body)
		b.WriteString("\n")
	}

	return b.String()
}

func replyHint(a messaging.Act) string {
	if a.RequiresReply() {
		return " (an answer is expected)"
	}

	return " (no answer needed: do not reply)"
}
