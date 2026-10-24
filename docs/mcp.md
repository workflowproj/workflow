# AI assistants (MCP)

Workflow is a [Model Context Protocol](https://modelcontextprotocol.io) server, so Claude and
other AI assistants can work with your issues: find work with JQL, read an issue with its
comments and timing, file and update issues, move them through their workflow, and report on
sprints and flow.

The assistant acts **as you**: it sees only what you can browse, workflow conditions and
validators apply, and every change appears in the issue history under your name. You choose
whether it may only read, or also create, edit, comment on and move issues.

## Connect

Give the assistant the address `<WFM_PUBLIC_URL>/mcp` (streamable HTTP). When it connects,
your browser opens Workflow's consent page: sign in if needed, choose **read only** or **read
and write**, and select *Allow access*. No token to copy; Workflow is the OAuth authorisation
server for its MCP endpoint (see [How sign-in works](#how-sign-in-works)). Profile → **Connect AI
assistants and tools** shows these steps with your server's address filled in.

**Claude** (claude.ai, Claude Desktop and mobile) — Settings → **Connectors** → *Add custom
connector*, paste `https://wfm.example.com/mcp`, then *Connect*. These connectors reach your
server from Anthropic's network, so the address must be reachable from the internet; Claude Code
and other clients running on your computer only need to reach it from there.

**Claude Code**

```sh
claude mcp add --transport http workflow https://wfm.example.com/mcp
```

Then run `/mcp` in Claude Code and choose *Authenticate*; the browser asks you to approve access.

**Other clients** that support MCP OAuth (Cursor, VS Code, ChatGPT connectors, Windsurf and
others) — add the URL in their MCP settings and sign in when they connect:

```json
{ "mcpServers": { "workflow": { "type": "http", "url": "https://wfm.example.com/mcp" } } }
```

**Connected apps.** Profile → **Connected apps** lists the assistants you allowed, with their
access and when they were last used; *Disconnect* revokes them at once (the app has to ask again).
Administrators see and disconnect everyone's under Administration → **Connected apps**.
Approvals, changes and disconnections are in the audit log (`oauth.`).

### With a personal access token

Tokens still work everywhere, for clients without OAuth support, scripts and shared assistants
(service-account tokens). Create one under Profile → **Personal access tokens** (`read` and
`write`, or only `read`); the dialog then shows the setup for each client with the token filled
in:

```sh
claude mcp add --transport http workflow https://wfm.example.com/mcp \
  --header "Authorization: Bearer wfm_pat_…"
```

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

Clients that only launch local servers: install the `wfm` binary (it relays stdio to `/mcp`) and
add to their configuration (e.g. `claude_desktop_config.json`):

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

In Kubernetes, `/mcp`, `/oauth/*` and `/.well-known/*` are served by the `web` deployment on the
internal ingress, like the UI and API; assistants must be able to reach that address (and
`WFM_PUBLIC_URL` must be it, since it is the OAuth issuer and the token audience).

## Tools

| Tool | Does |
|---|---|
| `whoami` | who the assistant acts as: organisation role, scopes and the connected app |
| `list_projects` | projects you can browse |
| `get_project` | issue types (keys for creating issues), boards and columns, open sprints, unreleased releases, labels |
| `search_issues` | JQL search, one line per issue, paged (`limit` up to 100) |
| `get_issue` | fields, description, parent, children, links, pull requests, recent comments, lead/cycle time and time in each status, and the transitions available now |
| `get_board` | a board's columns and issues (the active sprint on a Scrum board) |
| `get_sprint_report` | a sprint's commitment, completion, scope change and day-by-day burndown |
| `get_metrics` | `summary`, `breakdown`, `cycle_time`, `created_resolved`, `cumulative_flow` or `velocity` over projects and JQL |
| `find_users` | people by name or email |
| `create_issue` | create an issue (type, summary, Markdown description, priority, assignee by email or `me`, parent, labels, points, sprint, dates) |
| `update_issue` | change fields; `clear` empties them |
| `transition_issue` | run a workflow transition by id, name or target status, with screen fields and a comment |
| `add_comment` | comment (Markdown) |
| `link_issues` | `blocks`, `relates`, `duplicates` or `clones` |

Read tools are annotated read-only; nothing deletes. An app you allowed only to read is offered
only the read tools. Errors (an invalid JQL position, a validator refusing a transition, a
missing permission) come back as tool errors with the reason, so the assistant can correct
itself.

Things to ask: *"What's in progress on PLAT and who's on it?"*, *"File a bug in PLAT for the
Safari checkout crash, high priority, assign it to me and put it in the active sprint"*, *"Move
PLAT-42 to review with a comment summarising the PR"*, *"How is the sprint going — is scope
creeping?"*, *"Which bugs took longest to fix this month?"*.

## How sign-in works

Workflow implements the [MCP authorization
specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
(OAuth 2.1):

| Endpoint | |
|---|---|
| `/.well-known/oauth-protected-resource` (also `…/mcp`) | protected resource metadata (RFC 9728): resource `<WFM_PUBLIC_URL>/mcp`, issuer `<WFM_PUBLIC_URL>`, scopes `mcp:read`, `mcp:write` |
| `/.well-known/oauth-authorization-server` (also `openid-configuration`) | authorisation server metadata (RFC 8414) |
| `/oauth/register` | dynamic client registration (RFC 7591) |
| `/oauth/authorize` | authorisation code with PKCE (S256) and resource indicators (RFC 8707) |
| `/oauth/token` | code exchange and refresh |
| `/oauth/revoke` | token revocation (RFC 7009) |
| `/oauth/consent` | the consent page (web app) |

1. An unauthenticated request to `/mcp` gets `401` with `WWW-Authenticate: Bearer
   resource_metadata="…/.well-known/oauth-protected-resource/mcp", scope="mcp:read mcp:write"`.
2. The client reads the metadata and identifies itself: with a **client ID metadata document**
   (its `client_id` is an https URL serving its name and redirect URIs, which Workflow fetches;
   Claude Code and claude.ai do this) or by **registering** at `/oauth/register`.
3. It sends you to `/oauth/authorize`; you sign in and decide on the consent page. Workflow
   remembers your answer per app: while it covers what the app asks for, reconnecting skips the
   page.
4. The client exchanges the code for an **access token** (`wfm_oat_…`, 1 hour) and a **refresh
   token** (`wfm_ort_…`, 30 days, renewed on each refresh).

Scopes: `mcp:read` (read tools) and `mcp:write` (also the tools that change issues; implies
read). An app allowed only to read that calls a write tool gets `403` with `error="insufficient_scope"`,
so clients that support it can ask you for more access.

## Security

- `/mcp` accepts only `Authorization: Bearer` tokens — browser cookies are ignored, so a web page
  cannot drive it on your behalf.
- OAuth access tokens are valid for `/mcp` only (their audience): the REST API refuses them.
  Personal access tokens work on both.
- Tokens and codes are random, opaque and stored hashed. Codes are single use and expire after
  two minutes; presenting one twice revokes the tokens it produced. Refresh tokens rotate on
  every use, and presenting one that was already used revokes the whole chain (the app must ask
  again).
- PKCE (S256) and `state` are required; redirect URIs must match a registered one exactly, except
  that loopback addresses (`127.0.0.1`, `[::1]`, `localhost`) match on any port, as native apps
  need. Registered redirect URIs must be https, http on a loopback address, or a private-use
  scheme of a desktop app (such as `cursor://…`). Workflow never redirects an error to an address
  it has not validated; such requests stop on its own page.
- The consent page shows who is asking and where you will be sent: the verified host of an app
  identified by a metadata document, or the name an app registered with and its redirect host,
  with a warning when the app receives access on your computer. Approval needs your browser
  session (a token cannot approve) and is protected against cross-site requests and framing.
- Client ID metadata documents are fetched without following redirects, through the same guard
  as outbound webhooks (no private or loopback addresses unless `WFM_WEBHOOK_ALLOW_PRIVATE` or
  `WFM_OUTBOUND_ALLOWED_HOSTS` allow them), limited to 16 KB and cached per `Cache-Control`
  between 5 minutes and 24 hours.
- Registration, token and metadata fetches are rate-limited per client address. Registered apps
  nobody approved within a week are deleted. `WFM_OAUTH_DYNAMIC_REGISTRATION=false` turns
  registration off (metadata-document clients keep working), `WFM_OAUTH_CLIENT_METADATA_DOCUMENTS=false`
  turns metadata documents off, and `WFM_OAUTH_ENABLED=false` turns OAuth off entirely (tokens only);
  see [configuration](configuration.md#ai-assistants-oauth).
- Deactivating a person stops their apps at once; disconnecting an app revokes its tokens.
- Personal access tokens expire (1–366 days) and can be revoked on your profile; *last used*
  shows activity. Service-account tokens work too, for a shared assistant with its own
  permissions.
