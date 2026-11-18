-- Scheduled tasks (ADR-0012). The schedules themselves live in the project's
-- .nobody/schedules.yaml; the database keeps what they did and the
-- director's proposals.

-- One row per occurrence, kept after its schedule is deleted.
CREATE TABLE schedule_runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_id    TEXT NOT NULL,
    schedule_title TEXT NOT NULL,             -- copied: the schedule may be renamed or deleted
    trigger        TEXT NOT NULL,             -- schedule | manual
    mode           TEXT NOT NULL,             -- ticket | message, as when it fell due
    status         TEXT NOT NULL,             -- waiting | running | done | canceled | skipped | unavailable | interrupted
    due_at         TEXT NOT NULL,             -- RFC 3339, UTC
    started_at     TEXT NOT NULL DEFAULT '',
    ended_at       TEXT NOT NULL DEFAULT '',
    agent          TEXT NOT NULL DEFAULT '',
    ticket         TEXT NOT NULL DEFAULT '',
    message        TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT ''
);

CREATE INDEX schedule_runs_schedule ON schedule_runs (schedule_id, id);
CREATE INDEX schedule_runs_status ON schedule_runs (status);

-- Schedules the director proposed; accepting one writes it to schedules.yaml.
CREATE TABLE schedule_proposals (
    id               INTEGER PRIMARY KEY AUTOINCREMENT, -- shown as "SP-<id>"
    schedule         TEXT NOT NULL,                     -- the proposed schedule, as JSON
    status           TEXT NOT NULL,                     -- proposed | accepted | refused
    proposed_by      TEXT NOT NULL,
    proposal_message TEXT NOT NULL DEFAULT '',
    refusal_reason   TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    decided_at       TEXT NOT NULL DEFAULT ''
);
