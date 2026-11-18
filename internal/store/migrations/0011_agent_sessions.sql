-- The Claude Code session each agent runs, kept across daemon restarts so
-- that a project opening again resumes its agents' sessions (ADR-0015).
CREATE TABLE agent_sessions (
    address    TEXT PRIMARY KEY,           -- "alice@dev"
    session_id TEXT NOT NULL,              -- Claude Code session ID
    work_dir   TEXT NOT NULL,              -- where the session started: it is only resumed there
    mid_turn   INTEGER NOT NULL DEFAULT 0, -- 1 while the agent is in the middle of a turn
    updated_at TEXT NOT NULL               -- RFC 3339, UTC
);
