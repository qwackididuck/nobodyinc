# Environment variables of a project

A project can have environment variables of its own (ADR-0020): the Claude key, the token an MCP server refers to as `${GITLAB_TOKEN}`, a proxy, anything a tool run by an agent reads. Only the processes Nobody Inc starts for the project get them; your shell and your other projects do not.

## Where they apply

For every process started for the project, over the daemon's environment (a variable set here replaces one of the same name):

- each agent's `claude` process, hence the hooks, the MCP servers and the commands it runs;
- the fork that answers an urgent question for a busy agent;
- the discovery of Claude Code's built-in skills;
- the probing of an MCP server's tools (`GET /api/mcp/{name}/tools`), and the "set" flag of `GET /api/mcp`.

Agents get their own variables last (`NOBODY_AGENT`, `NOBODY_DAEMON_URL`, the git identity): the project cannot change them.

## Where they live

`~/.nobody/projects/<id>/env.yaml`, a map of name to value, readable by you only (`0600`). It is not in `.nobody/`, which may be committed. The API never returns a value, and neither do the control room, the logs or the events.

```yaml
ANTHROPIC_API_KEY: sk-ant-…
GITLAB_TOKEN: glpat-…
```

You may edit the file by hand while the daemon is stopped; the daemon reads it when the project opens. A file that is not a map of strings stops the project from opening.

## Names

A name is letters, digits and `_`, not starting with a digit, at most 128 characters; a value is not empty and holds no NUL character (delete a variable to unset it). These names are refused, because Nobody Inc sets them or removes them on purpose (ADR-0003):

| Refused | Why |
|---|---|
| `NOBODY_*` | the hooks reach the daemon through them |
| `GIT_AUTHOR_*`, `GIT_COMMITTER_*` | each agent has its own git identity |
| `CLAUDECODE`, `CLAUDE_PID`, `CLAUDE_CODE_SESSION_*`, `CLAUDE_CODE_MESSAGING_*`, … | markers of a parent Claude Code session, removed from agents (`internal/runtime/command.go`) |

## Changing them

Setting or deleting a variable restarts every agent once idle, resuming its session: a running process keeps the environment it started with. No turn is interrupted.

| Action | Endpoint |
|---|---|
| List the names | `GET /api/env` |
| Set or replace | `PUT /api/env/{name}` with `{"value"}` |
| Remove | `DELETE /api/env/{name}` |

The code is `internal/projectenv` (storage, validation, overlay), `internal/server/env.go` (API) and `Launch.ProjectEnv` in `internal/runtime/command.go`.
