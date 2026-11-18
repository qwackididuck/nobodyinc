-- Ticket and feature budgets (ADR-0013, point 3): optional, in list-price
-- USD, with an optional threshold overriding the project's (percent).
-- Agent, team and staff caps live in the project's .nobody/budgets.yaml.
ALTER TABLE tickets ADD COLUMN budget_usd REAL;
ALTER TABLE tickets ADD COLUMN threshold INTEGER;
ALTER TABLE features ADD COLUMN budget_usd REAL;
ALTER TABLE features ADD COLUMN threshold INTEGER;
