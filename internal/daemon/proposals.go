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

// onProposed sends the proposal to the human and returns the message's ID.
func (p *proposals) onProposed(f work.Feature) string {
	body := fmt.Sprintf("I propose the feature %s: %s.\n\nNeed:\n%s\n\nAcceptance criteria:\n%s\n\n"+
		"Start it or refuse it from the control room (Feature panel).",
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

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none given)"
	}

	return s
}
