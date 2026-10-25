# GitHub and GitLab

Connect GitHub (as a GitHub App) and GitLab (with an access token) under Administration →
GitHub & GitLab. Then link repositories to projects (Project settings → Repositories). Issue keys
in branch names, commit messages and pull/merge request titles, branches or descriptions link
that activity to the issue — but only for projects linked to the repository, so `ABC-1` in an
unrelated repository is ignored. Keys match in any case and with any separator, so a pull request
from `feature/plat-12-checkout`, `PLAT-12_fix-totals` or `users/ada/Plat-12/try` links to PLAT-12
even when its title never mentions the issue.

What you get on each issue:

- **Development panel**: branches, commits, pull/merge requests (state and review), latest
  pipeline status; summarised on board cards.
- **Smart commits** — commands in commit messages, run as the committer (matched by
  email to an active account, with that person's permissions; unknown committers are linked
  but cannot change issues):
  - `PLAT-12 #comment Fixed the retry logic` adds a comment.
  - `PLAT-12 #start-work`, `PLAT-12 #in-review`, `PLAT-12 #done` run the transition whose id,
    name or target status matches (slugified). Conditions, validators and post-functions apply;
    a transition that needs a screen is skipped.
  - Commands apply to the keys on the same line; a commit seen again (re-push, another branch)
    does not repeat them.
- **Write-back**: when an issue is first linked to a PR/MR, a comment with the issue key, summary
  and status is posted on it (turn off per connection with *write back*).
- **Create branch** from an issue (named `KEY-summary-slug` by default).
- **Automation triggers**: `pr.opened`, `pr.merged`, `commit.pushed`, `branch.created`,
  `pipeline.completed` (see [automation.md](automation.md)), e.g. "move to In Review when a PR
  opens, to Done when it merges".

## GitHub App

1. GitHub → Settings → Developer settings → GitHub Apps → *New GitHub App* (in your
   organisation).
2. **Webhook URL**: shown on the connection after you create it in Workflow
   (`<WFM_HOOKS_PUBLIC_URL>/hooks/github/<connection id>`). Create the connection in Workflow
   first with a placeholder App id, or edit the App afterwards. **Webhook secret**: any random
   string; enter the same value in Workflow (or leave it empty there and copy the generated one).
3. **Repository permissions**: Contents: *Read & write* (write only for "create branch"),
   Pull requests: *Read & write* (write for the PR comment), Actions: *Read*, Metadata: *Read*.
4. **Subscribe to events**: Push, Create, Delete, Pull request, Pull request review, Workflow run.
5. Generate a private key, note the App id, and install the App on the repositories you want.
6. In Workflow: *New connection* → GitHub, App id, App slug, private key (PEM), webhook secret.

Installing the App on more repositories sends an `installation_repositories` event and they
appear in Workflow automatically. GitHub Enterprise Server: set the API URL to
`https://github.example.com/api/v3`.

## GitLab

1. Create a group access token (or project/personal token) with the `api` scope — `read_api` is
   enough if you add webhooks yourself and don't need comments or branch creation.
2. In Workflow: *New connection* → GitLab, URL (`https://gitlab.com` or your instance), token,
   optional webhook secret, and **Manage webhooks** to have Workflow create the project hook
   whenever you link a repository.
3. Without managed webhooks, add a webhook on each project (or group, on Premium) with the URL
   shown on the connection, the secret token, and the triggers *Push events*, *Merge request
   events* and *Pipeline events*.

Repositories are discovered from the token's projects by the periodic reconcile (and *Sync now*).

## Reliability

- Deliveries are verified (GitHub `X-Hub-Signature-256` HMAC, GitLab `X-Gitlab-Token`), stored
  and acknowledged immediately, then processed by workers — a slow database or a burst of
  pushes never makes GitHub/GitLab time out.
- Redeliveries are de-duplicated by delivery id (GitHub `X-GitHub-Delivery`, GitLab
  `X-Gitlab-Event-UUID`, or a content hash).
- Failed processing retries with backoff; recent deliveries and their status are listed on the
  connection, with *Reprocess*.
- A reconcile job (`WFM_SCM_RECONCILE_INTERVAL`, 30 minutes) lists repositories, branches and
  pull/merge requests from the APIs to fill gaps from missed deliveries. Reconciled changes do not
  fire automation.
- Point `WFM_HOOKS_PUBLIC_URL` at the `hooks` deployment's public address if it is exposed
  separately from the UI; in Kubernetes the chart's ingress routes `/hooks` and `/scim` there.
