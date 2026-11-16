# ADR-0007 — Finishing a feature: review, then a local merge into the base branch

- Status: accepted
- Date: 2026-11-16

## Context

A feature gets its own branch, `nobody/<slug>`, and one worktree and branch per team (`docs/workspaces.md`). Until now, Nobody Inc never touched any branch it did not own: once the tickets were done, finishing the feature was left to the user, with git. The control room had no way to see what a feature changed, and the feature stayed active forever: its worktrees stayed, and the agents kept working in them until another feature replaced it.

In the first real test, the human did a whole feature with the director and then had no way to validate and close it.

## Decision

### 1. The human reviews, then finishes

- `GET /api/features/{id}/review` shows what the feature branch brings compared with its base branch, since they diverged: the commits, the files with lines added and deleted, the diff (cut at 400 KB), and the feature's tickets that are not done or canceled.
- `POST /api/features/{id}/finish` finishes the **active** feature. Only the human may: like starting a feature, it is a decision about the user's own repository. The director is told by a notice from `nobody`.

### 2. Finishing merges into the base branch, locally

This is the one exception to "Nobody Inc never touches a branch it does not own", and it happens only on the human's request:

- the feature branch is merged into the base branch with `git merge --no-ff`, signed by Nobody Inc, so that the feature stays visible in the history;
- where the base branch is checked out decides where the merge happens: in the project's checkout if it is there (refused while a tracked file has uncommitted changes, which the merge could otherwise mix with), in a throwaway worktree if it is checked out nowhere, and refused if another worktree has it;
- on a conflict the merge is aborted: nothing changes, and the human gets the commands to merge it by hand;
- **nothing is ever pushed**: publishing stays the user's decision.

### 3. Then the feature ends

- the feature becomes `finished` and is no longer active; its merge commit is recorded;
- team agents restart in the project directory (the director, which always works there, is not restarted);
- the teams' worktrees and branches are removed afterwards, once nobody works in them. The feature branch is kept: it is merged, and it documents what the feature was.

### 4. Unfinished tickets

While some of the feature's tickets are not done or canceled, finishing is refused. The human may finish anyway (`force`): what those tickets did not deliver is then lost with the worktrees. The control room says so before doing it.

## Consequences

- A feature has a full life in the control room: proposed, started, reviewed, finished.
- The project's base branch can now be changed by Nobody Inc, only through this explicit action, never pushed, and refused rather than risking the user's uncommitted work.
- Several features active at once, pushing and opening pull requests remain to be done.
