-- Budget requests, monthly extensions and cap proposals (ADR-0013).

-- One request per budget at its threshold; active while the budget stays
-- reached, at most one active request per budget.
CREATE TABLE budget_requests (
    id               INTEGER PRIMARY KEY AUTOINCREMENT, -- shown as "BR-<id>"
    scope_kind       TEXT NOT NULL,                     -- ticket | feature | agent | team | staff
    scope_id         TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL,                     -- awaiting_agent | awaiting_director | awaiting_human | extended | refused | closed
    active           INTEGER NOT NULL DEFAULT 1,
    asked_agent      TEXT NOT NULL DEFAULT '',
    requested_by     TEXT NOT NULL DEFAULT '',
    amount_usd       REAL,
    justification    TEXT NOT NULL DEFAULT '',
    director_opinion TEXT NOT NULL DEFAULT '',
    recommended_usd  REAL,
    limit_usd        REAL NOT NULL,
    spent_usd        REAL NOT NULL,
    card_message     TEXT NOT NULL DEFAULT '',
    extended_usd     REAL,
    refusal          TEXT NOT NULL DEFAULT '',          -- JSON
    closed_reason    TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    submitted_at     TEXT NOT NULL DEFAULT '',
    decided_at       TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX budget_requests_active ON budget_requests (scope_kind, scope_id) WHERE active = 1;

-- An extension of a monthly cap holds for its month only; the cap in
-- budgets.yaml does not change.
CREATE TABLE budget_extensions (
    scope_kind TEXT NOT NULL,   -- agent | team | staff
    scope_id   TEXT NOT NULL DEFAULT '',
    month      TEXT NOT NULL,   -- "YYYY-MM"
    amount_usd REAL NOT NULL,
    request    INTEGER NOT NULL
);

CREATE INDEX budget_extensions_month ON budget_extensions (month);

-- Caps the director proposed; accepting one writes it to budgets.yaml.
CREATE TABLE budget_proposals (
    id               INTEGER PRIMARY KEY AUTOINCREMENT, -- shown as "BP-<id>"
    scope_kind       TEXT NOT NULL,                     -- agent | team | staff
    scope_id         TEXT NOT NULL DEFAULT '',
    cap              TEXT NOT NULL,                     -- the proposed cap, as JSON
    reason           TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL,                     -- proposed | accepted | refused
    proposed_by      TEXT NOT NULL,
    proposal_message TEXT NOT NULL DEFAULT '',
    refusal_reason   TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    decided_at       TEXT NOT NULL DEFAULT ''
);
