-- Decisions the director took in the human's place in Nobody mode
-- (docs/automation.md), kept for the human to review.
CREATE TABLE decisions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    asked_by   TEXT NOT NULL,
    message    TEXT NOT NULL UNIQUE, -- the question's message: one decision per question
    question   TEXT NOT NULL,
    decision   TEXT NOT NULL,
    rationale  TEXT NOT NULL,
    impact     TEXT NOT NULL,        -- low | medium | high
    decided_at TEXT NOT NULL,
    seen       INTEGER NOT NULL DEFAULT 0
);
