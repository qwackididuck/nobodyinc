package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// proposals carries feature proposals between the director and the human
// (docs/god.md), in the organization's own protocol: the proposal reaches the
// human as a "propose" message from the director, and the human's decision
// answers it with "agree" or "refuse", which the director reads as mail.
type proposals struct {
	messages *messaging.Service
	logger   *slog.Logger
}

// attach makes the work service report the director's proposals and the
// human's decisions to p.
func (p *proposals) attach(tickets *work.Service) {
	tickets.OnFeatureProposed = p.onProposed
	tickets.OnFeatureDecided = p.onDecided
}

// onProposed sends the proposal to the human and returns the message's ID.
func (p *proposals) onProposed(f work.Feature) string {
	body := fmt.Sprintf("I propose the feature %s: %s.\n\nNeed:\n%s\n\nAcceptance criteria:\n%s\n\n"+
		"Start it or refuse it from the control room (this chat, or the banner above the office).",
		f.ID, f.Title, orNone(f.Description), orNone(f.AcceptanceCriteria))

	msg, err := p.messages.Send(context.Background(), messaging.Draft{
		From: f.CreatedBy, To: org.HumanAddress, Act: messaging.ActPropose, Priority: messaging.Normal,
		Subject: "Feature proposal " + f.ID + ": " + f.Title, Body: body,
	})
	if err != nil {
		p.logger.Error("send feature proposal", "feature", f.ID, "err", err)

		return ""
	}

	return msg.ID
}

// onDecided answers the director's proposal with the human's decision.
func (p *proposals) onDecided(f work.Feature) {
	if f.ProposalMessage == "" {
		return
	}

	d := messaging.Draft{
		From: org.HumanAddress, To: f.CreatedBy, Priority: messaging.Normal, InReplyTo: f.ProposalMessage,
	}

	if f.Status == work.FeatureStarted {
		d.Act, d.Subject = messaging.ActAgree, "Feature "+f.ID+" started: "+f.Title
		d.Body = fmt.Sprintf("Started on the branch %s. Split it into tickets for the teams.", f.Branch)
	} else {
		d.Act, d.Subject = messaging.ActRefuse, "Feature "+f.ID+" refused: "+f.Title
		d.Body = "Reason: " + orNone(f.RefusalReason)
	}

	if _, err := p.messages.Send(context.Background(), d); err != nil {
		p.logger.Error("answer feature proposal", "feature", f.ID, "err", err)
	}
}

// onFinished tells the director that the human finished a feature: it is
// merged into its base branch and the teams are back in the project.
func (p *proposals) onFinished(ctx context.Context, director string, f work.Feature) {
	if director == "" {
		return
	}

	merged := "There was nothing to merge."
	if f.MergeCommit != "" {
		merged = fmt.Sprintf("It is merged into %s (commit %s).", f.BaseBranch, shortHash(f.MergeCommit))
	}

	_, err := p.messages.Send(ctx, messaging.Draft{
		From: org.SystemAddress, To: director, Act: messaging.ActInform, Priority: messaging.Normal,
		Subject: "Feature " + f.ID + " finished: " + f.Title,
		Body:    "The human finished the feature " + f.ID + ". " + merged + " The teams work in the project again; no feature is active.",
	})
	if err != nil {
		p.logger.Error("tell the director a feature is finished", "feature", f.ID, "err", err)
	}
}

// shortHashLength is how much of a commit hash messages show.
const shortHashLength = 10

func shortHash(h string) string {
	if len(h) > shortHashLength {
		return h[:shortHashLength]
	}

	return h
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none given)"
	}

	return s
}
