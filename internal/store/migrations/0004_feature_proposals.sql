-- Feature proposals: the director proposes a feature, the human starts or
-- refuses it (docs/god.md). Features created before were started at once.
ALTER TABLE features ADD COLUMN status TEXT NOT NULL DEFAULT 'started'; -- proposed | started | refused
ALTER TABLE features ADD COLUMN acceptance_criteria TEXT NOT NULL DEFAULT '';
-- The "propose" message sent to the human, answered when they decide.
ALTER TABLE features ADD COLUMN proposal_message TEXT NOT NULL DEFAULT '';
ALTER TABLE features ADD COLUMN refusal_reason TEXT NOT NULL DEFAULT '';
