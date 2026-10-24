# Slack and Mattermost

Two integrations, each optional:

- **Channel notifications** — a project posts what happens (new issues, status changes,
  comments, sprints, releases, pull requests, failed pipelines) into Slack or Mattermost
  channels, through each channel's incoming webhook. Automation rules can post too.
- **The `/wfm` command** — look up an issue or run a JQL search from chat.

## Channel notifications

1. Create an incoming webhook for the channel:
   - **Slack**: create (or reuse) a Slack app → *Incoming Webhooks* → *Add New Webhook to
     Workspace*, pick the channel. The URL looks like `https://hooks.slack.com/services/…`.
   - **Mattermost**: *Integrations* → *Incoming Webhooks* → *Add*, pick the channel. The URL looks
     like `https://mattermost.example.com/hooks/…`.
2. In Workflow: Project settings → **Chat** → *Add channel*. Choose Slack or Mattermost, paste the
   URL, give it a name people recognise (`#team-payments`), and pick the events:

| Event | Posted when |
|---|---|
| `issue.created` | an issue is created |
| `issue.transitioned` | an issue changes status |
| `issue.assigned` | an issue's assignee changes |
| `comment.created` | someone comments (the first 400 characters are quoted) |
| `sprint.started`, `sprint.completed` | a sprint starts or completes (with its goal and how many issues carried over) |
| `release.released` | a release ships |
| `pr.opened`, `pr.merged` | a linked pull/merge request opens or merges |
| `pipeline.failed` | a linked CI pipeline fails |

   *Everything* also covers event types added in later versions. An optional **JQL filter**
   limits issue events to matching issues, e.g. `type = Bug AND priority >= High` for an
   on-call channel; it does not apply to sprint and release events, nor to messages from
   automation rules, and `currentUser()` matches nobody here.
3. *Send test* posts a test message; the channel's delivery log shows each message's status.

Messages use Slack Block Kit and Mattermost attachments, link to the issue, and are coloured by
status category. The webhook URL is a credential: it is stored encrypted and never shown again
(the list shows only its host and last characters; leave the field empty when editing to keep
it). Deliveries are queued after the change commits; network errors, 5xx and 429 answers are
retried with backoff, while other answers (a deleted channel, a revoked webhook) are recorded as
failed. Pause a channel to stop messages without losing its settings.

**Automation**: the `chat` action posts a message of your own to one of the project's channels,
with the usual templates — e.g. *when an issue enters a done status, post
"🎉 {{issue.key}} {{issue.summary}} is done" to #releases* (a template in the gallery). Removing a
channel pauses the rules that post to it. See [automation.md](automation.md).

### Self-hosted Mattermost on a private network

Outbound calls refuse private addresses (SSRF protection). Allow your Mattermost host by name —
only that host may resolve to a private address:

```sh
WFM_OUTBOUND_ALLOWED_HOSTS=mattermost.corp.example        # or .corp.example for a whole domain
```

In Kubernetes, worker egress allows port 443 by default; if Mattermost listens on another port
(8065), add a `networkPolicy.egress.extraRules` entry for it.

## The `/wfm` command

```
/wfm PLAT-12                                   show an issue (status, assignee, type, priority)
/wfm search assignee = "ada@example.com" AND statusCategory != Done
/wfm help
```

Answers are only visible to the person who asked. Commands are **read-only** and only see
**open projects**, because a chat workspace can include guests without accounts here; set
`WFM_CHAT_COMMAND_PROJECTS=PLAT,OPS` to choose the projects explicitly (private projects only
appear when listed).

**Slack**: in your Slack app → *Slash Commands* → *Create New Command*: command `/wfm`, request
URL `<WFM_HOOKS_PUBLIC_URL>/hooks/chat/slack`. Copy *Signing Secret* from *Basic Information* into
`WFM_SLACK_SIGNING_SECRET`. Requests are verified with Slack's signature and rejected when older
than five minutes.

**Mattermost**: *Integrations* → *Slash Commands* → *Add*: trigger word `wfm`, request URL
`<WFM_HOOKS_PUBLIC_URL>/hooks/chat/mattermost`, method POST. Put the generated token in
`WFM_MATTERMOST_COMMAND_TOKENS` (comma-separated if you have several teams).

Without the secret or token, the endpoint answers 404.
