# AI assistants (MCP)

Workflow is a [Model Context Protocol](https://modelcontextprotocol.io) server, so Claude and
other AI assistants can work with your issues: find work with JQL, read an issue with its
comments and timing, file and update issues, move them through their workflow, and report on
sprints and flow.

The assistant acts **as you**, through a personal access token: it sees only what you can
browse, workflow conditions and validators apply, and every change appears in the issue history
under your name. Use a token with only the `read` scope for an assistant that should look but
not touch.

## Connect

1. Profile → **Personal access tokens** → *New token* (scopes `read` and `write`, or `read`).
   The dialog then shows the setup for each client with the token filled in.
2. Add the server to your assistant. The endpoint is `<WFM_PUBLIC_URL>/mcp` (streamable HTTP).

**Claude Code**

```sh
claude mcp add --transport http workflow https://wfm.example.com/mcp \
  --header "Authorization: Bearer wfm_pat_…"
```

**HTTP clients** (Cursor, VS Code, Windsurf and others) — in their MCP settings:

```json
{
  "mcpServers": {
    "workflow": {
      "type": "http",
      "url": "https://wfm.example.com/mcp",
      "headers": { "Authorization": "Bearer wfm_pat_…" }
    }
  }
}
```

**Claude Desktop** and other clients that launch local servers: install the `wfm` binary (it
relays stdio to `/mcp`) and add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "workflow": {
      "command": "wfm",
      "args": ["mcp"],
      "env": { "WFM_SERVER": "https://wfm.example.com", "WFM_TOKEN": "wfm_pat_…" }
    }
  }
}
```

In Kubernetes, `/mcp` is served by the `web` deployment on the internal ingress, like the UI and
API; assistants must be able to reach that address.

## Tools

| Tool | Does |
|---|---|
| `whoami` | the token's owner, organisation role and scopes |
| `list_projects` | projects you can browse |
| `get_project` | issue types (keys for creating issues), boards and columns, open sprints, unreleased releases, labels |
| `search_issues` | JQL search, one line per issue, paged (`limit` up to 100) |
| `get_issue` | fields, description, parent, children, links, pull requests, recent comments, time tracking (estimated, logged, remaining), lead/cycle time and time in each status, and the transitions available now |
| `get_board` | a board's columns and issues (the active sprint on a Scrum board) |
| `get_sprint_report` | a sprint's commitment, completion, scope change and day-by-day burndown |
| `get_metrics` | `summary`, `breakdown`, `cycle_time`, `created_resolved`, `cumulative_flow` or `velocity` over projects and JQL |
| `find_users` | people by name or email |
| `create_issue` | create an issue (type, summary, Markdown description, priority, assignee by email or `me`, parent, labels, points, sprint, dates) |
| `update_issue` | change fields; `clear` empties them |
| `transition_issue` | run a workflow transition by id, name or target status, with screen fields and a comment |
| `add_comment` | comment (Markdown) |
| `log_work` | log time spent (`2h`, `1d 4h`), when it was done, a description; the remaining estimate goes down by the time logged unless `remainingEstimate` sets it or `leaveEstimate` keeps it |
| `link_issues` | `blocks`, `relates`, `duplicates` or `clones` |

Read tools are annotated read-only; nothing deletes. Errors (an invalid JQL position, a
validator refusing a transition, a missing permission) come back as tool errors with the reason,
so the assistant can correct itself.

Things to ask: *"What's in progress on PLAT and who's on it?"*, *"File a bug in PLAT for the
Safari checkout crash, high priority, assign it to me and put it in the active sprint"*, *"Move
PLAT-42 to review with a comment summarising the PR"*, *"How is the sprint going — is scope
creeping?"*, *"Which bugs took longest to fix this month?"*.

## Security

- `/mcp` accepts only `Authorization: Bearer` tokens — browser cookies are ignored, so a web page
  cannot drive it on your behalf. Unauthenticated requests get 401 with `WWW-Authenticate`.
- Tokens expire (1–366 days) and can be revoked on your profile; *last used* shows activity.
- Service-account tokens work too, for a shared assistant with its own permissions.
