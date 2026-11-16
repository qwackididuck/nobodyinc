# Workspaces: features, branches and team worktrees

This page is about the **project the agents work on** (the directory holding `.nobody/`, see `agents.md`), not about the Nobody Inc repository itself. The project keeps its own branching model; Nobody Inc only adds branches prefixed with `nobody/` and never merges into any other branch.

## Features

A feature is a request, e.g. "Add the order API". The human starts one directly ("Start a feature" in the banner above the office, or `POST /api/features`), or the director proposes it and the human starts it (`POST /api/features/{id}/start`) or refuses it (`docs/god.md`). A proposed feature touches neither git nor the agents. Starting one:

1. creates the **feature branch** `nobody/<slug>` from a base branch (the project's current branch unless another one is given);
2. creates one **worktree per team**, on its own branch:

   ```
   project/                                        ← the user's checkout, untouched
   ~/.nobody/projects/<id>/worktrees/add-the-order-api/
       specs/   branch nobody/add-the-order-api--specs
       dev/     branch nobody/add-the-order-api--dev
   ```

   Worktrees live outside the project, next to its database, so they never show in the user's `git status` and agents can be denied any write to the project's `.nobody/` by absolute path (ADR-0004). Throwaway merge worktrees live in `worktrees/_merge/`. Earlier versions kept worktrees in `<project>/.nobody/worktrees/` and hid `.nobody/` in `.git/info/exclude`; that line is removed at the next feature (remove old worktrees with `git worktree remove`);
3. restarts every team agent in its team's worktree. The director is not restarted: it always works in the project, and keeps its conversation.

New tickets belong to the active feature. Only one feature is active at a time; without one, agents work in the project directory itself and tickets have no branches.

## Prerequisite: trust the project once

Claude Code asks whether to trust a new folder before it starts, and an agent waiting on that prompt never starts working. Opening the project once with `claude` in its main checkout and accepting the prompt covers every team worktree: git ties a worktree to its repository, wherever it lives (trusting a folder also covers its subfolders). Otherwise the prompt shows up in each agent's terminal (visible from the UI).

`nobodyd` checks this when it starts (`runtime.ProjectTrusted`, which reads the trust flags of Claude Code's `~/.claude.json`, or `$CLAUDE_CONFIG_DIR/.claude.json`) and logs a warning if the project is not trusted yet; `nobodyd init` says so too. The rules it applies are checked against the real CLI by `task contract` (`TestTrustRules`).

## Moving work between teams

Each team works in its own worktree, and the feature branch carries the work from one team to the next:

```
                 nobody/add-the-order-api   (feature branch)
                   ▲ merged when a ticket is done   │ merged when a ticket is claimed
                   │                                 ▼
nobody/…--specs                               nobody/…--dev
  archi commits the spec and closes             dev1 claims its ticket: its worktree
  the ticket                                    first receives the spec
```

- **Claiming a ticket** merges the feature branch into the claimer's team branch, so the agent starts from the latest validated work. If git cannot merge (e.g. a teammate's uncommitted changes are in the way), the claim is refused with an explanation.
- **Closing a ticket** (done):
  - the agent commits its own work first, with conventional commits;
  - closing is refused while the team's worktree has uncommitted changes (the refusal lists them);
  - work that changes the project's `.nobody/` (the agents' own configuration) is refused, or delivered with a warning to the human, depending on `config_protection` in `~/.nobody/settings.yaml` (`docs/agents.md`); the refusal tells the agent to put it back with `git restore --source=<feature branch> --staged --worktree -- .nobody`;
  - the team branch is then merged into the feature branch: a fast-forward when possible, otherwise a merge in a temporary worktree;
  - on a conflict nothing changes, and the refusal explains how to fix it (merge the feature branch into your branch, resolve, commit, close again);
  - the delivered commits are attached to the ticket as `commit` deliverables.
- **Finishing the feature** is the human's decision (ADR-0007). "Review & finish" in the banner above the office (`GET /api/features/{id}/review`) shows the commits, the files changed and the diff against the base branch, and the tickets not done yet. "Merge and finish" (`POST /api/features/{id}/finish`):
  - merges `nobody/<slug>` into the base branch with a merge commit signed by Nobody Inc: in the project's checkout when the base branch is checked out there (refused while a tracked file has uncommitted changes), in a throwaway worktree when it is checked out nowhere, refused when another worktree has it. On a conflict nothing changes, and the refusal gives the commands to merge it by hand. **Nothing is pushed**;
  - is refused while some of the feature's tickets are not done or canceled, unless the human finishes anyway (their undelivered work is lost);
  - ends the feature: it is `finished` and no longer active, team agents restart in the project, then the teams' worktrees and branches are removed. The feature branch is kept. The director gets a notice from `nobody`.

## One ticket at a time

An agent works like a person: `ticket_claim` is refused while it already has a ticket in progress. Parallel work comes from several agents in a team, each on its own ticket, sharing the team's worktree.

## Git identities

- Each agent commits under its own name: `dev1@dev (Nobody Inc)` <`dev1.dev@nobody.local`> (set through `GIT_AUTHOR_*` / `GIT_COMMITTER_*` when the agent starts), so the history shows who wrote what.
- Merges made by the daemon are signed `Nobody Inc` <`nobody@nobody.local`>.

## Code

| Piece | Role |
|---|---|
| `internal/workspace` | The git operations, through the `git` CLI: `Prepare`, `Sync` (claim), `Dirty` and `Deliver` (done). Tested on real temporary repositories. |
| `internal/daemon/git.go` | Connects tickets to the workspace (`work.Workspace`) and tells each agent its directory. |
| `internal/work/feature.go` | Features, and the claim / done hooks in the ticket service. |
| `test/e2e` (`TestFeatureRelayThroughGit`) | The relay from specs to dev through the feature branch, end to end. |

## Not yet

- Several active features at once.
- A worktree without source code for the validation team.
- Pushing branches and opening pull requests.
- An automatic integration phase at the end of a feature.
