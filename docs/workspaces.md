# Workspaces: features, branches and team worktrees

This page is about the **project the agents work on** (the directory holding `.nobody/`, see `agents.md`), not about the Nobody Inc repository itself. The project keeps its own branching model; Nobody Inc only adds branches prefixed with `nobody/` and never merges into any other branch.

## Features

A feature is a request, e.g. "Add the order API". Starting one (Feature panel in the UI, or `POST /api/features`):

1. creates the **feature branch** `nobody/<slug>` from a base branch (the project's current branch unless another one is given);
2. creates one **worktree per team**, on its own branch:

   ```
   project/                                   ← the user's checkout, untouched
   project/.nobody/worktrees/add-the-order-api/
       specs/   branch nobody/add-the-order-api--specs
       dev/     branch nobody/add-the-order-api--dev
   ```

   `.nobody/worktrees/` is ignored by git (`.nobody/.gitignore`, and `.git/info/exclude` for projects without it), so worktrees never show in the user's `git status`. The rest of `.nobody/` is the project's configuration and is committed. Throwaway merge worktrees live in `.nobody/worktrees/_merge/`;
3. restarts every agent in its team's worktree.

New tickets belong to the active feature. Only one feature is active at a time; without one, agents work in the project directory itself and tickets have no branches.

## Prerequisite: trust the project once

Claude Code asks whether to trust a new folder before it starts, and an agent waiting on that prompt never starts working. Since Claude Code 2.1.63, trust is shared across the worktrees of a repository: open the project once with `claude` in its main checkout and accept the prompt, and every team worktree is covered. Otherwise the prompt shows up in each agent's terminal (visible from the UI) after a feature starts.

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
  - the team branch is then merged into the feature branch: a fast-forward when possible, otherwise a merge in a temporary worktree;
  - on a conflict nothing changes, and the refusal explains how to fix it (merge the feature branch into your branch, resolve, commit, close again);
  - the delivered commits are attached to the ticket as `commit` deliverables.
- **Finishing the feature** is up to the user: `nobody/<slug>` holds all the work, to review and merge wherever they want. Nobody Inc does not push.

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
