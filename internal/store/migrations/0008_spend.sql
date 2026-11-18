-- What the agents spend (ADR-0013): one row per delta of a session's running
-- cost, as Claude Code's statusLine reports it, or per urgent fork. Each row
-- is attributed when it is written, from the daemon's state at that moment.
-- Amounts are Claude Code's estimate at list price, in USD.
CREATE TABLE spend (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    at           TEXT NOT NULL,              -- RFC 3339, UTC
    month        TEXT NOT NULL,              -- "YYYY-MM", in the machine's time zone
    source       TEXT NOT NULL,              -- statusline | fork
    session      TEXT NOT NULL DEFAULT '',   -- the Claude Code session that spent it
    agent        TEXT NOT NULL DEFAULT '',   -- '' for a fork the human asked for (staff only)
    team         TEXT NOT NULL DEFAULT '',   -- '' for the director and the human
    ticket       TEXT NOT NULL DEFAULT '',   -- "T-12": the agent's ticket in progress
    feature      TEXT NOT NULL DEFAULT '',   -- "F-3": that ticket's feature
    schedule_run INTEGER NOT NULL DEFAULT 0, -- the occurrence the agent was running
    amount_usd   REAL NOT NULL
);

CREATE INDEX spend_month ON spend (month);
CREATE INDEX spend_ticket ON spend (ticket);
CREATE INDEX spend_feature ON spend (feature);
CREATE INDEX spend_schedule_run ON spend (schedule_run);
