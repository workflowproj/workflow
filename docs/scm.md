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
- **Deployments**: the environments the issue reached, with status and time (below).
- **Automation triggers**: `pr.opened`, `pr.merged`, `commit.pushed`, `branch.created`,
  `pipeline.completed`, `deployment.completed` (see [automation.md](automation.md)), e.g. "move
  to In Review when a PR opens, to Done when it merges".

## Tags

A release can ship as a Git tag (a repository connected to the project and a tag name): set it in
the release's *Edit* dialog, or let it link itself. When a connected repository gets a new tag,
the project's release named like it is linked to it, matching exactly or with a leading `v`
added or dropped (`v3.9.0` and `3.9.0`). A release already linked to another tag keeps that
link. The release page links to the tag at the provider and, when the previous release has a tag
in the same repository, to the changes between the two.

Project settings → Releases has two options, both off by default:

- **Create a release for each new tag**: a tag that names no release creates one, named after
  the tag.
- **Mark the release released when its tag is pushed**: the release is marked released (its date
  set to today if it had none) and `release.released` is raised. Unfinished issues keep it as
  their fix version, and its readiness lists them.

GitHub reports tags with the *Create* event (`ref_type: tag`) and a *Push* to `refs/tags/…`,
which also gives the commit; GitLab with *Tag push events*. Deleted tags change nothing.

## Deployments

Workflow records deployments reported by GitHub (`deployment_status` events, for example from
GitHub Actions jobs with an `environment`) and GitLab (*Deployment events*): repository,
environment, ref, commit, status, link, who deployed and when. A status that arrives after a
newer one is ignored. GitHub's `inactive` status (superseded by a newer deployment) is not an
outcome and changes nothing.

When a deployment to an environment completes (succeeds or fails), a worker job works out which
issues it contains: it asks the provider for the commits between the previous successful
deployment to the same environment in that repository and this one (GitHub's compare API,
GitLab's repository compare), reads at most 250 of them (the newest), and takes the issues those
commits mention plus the issues already linked to them. The first deployment to an environment
only has its head commit's issues; a redeploy of the same commit brings nothing new. If the
provider cannot compare the two (the earlier commit was rewritten away), the head commit is used.
These calls go through the outbound request guard: the connection's own API host may be on a
private network, other destinations may not (see `WFM_OUTBOUND_ALLOWED_HOSTS` in
[configuration.md](configuration.md)). Failed calls are retried with backoff.

Each issue the deployment contains then gets `deployment.completed` (automation rules, outbound
webhooks), once per outcome: redeliveries stay quiet, and a re-run that turns a failure into a
success raises it again. The issue's Development panel lists each environment it went to (the
latest successful deployment containing it, else the latest failed one) linked to the
deployment; board cards show a deployed mark; search has `deployedTo = "production"`
([search.md](search.md)); and a release reaches an environment when its tag was deployed there or
every one of its done issues was ([releases.md](releases.md#environments)).

## GitHub App

1. GitHub → Settings → Developer settings → GitHub Apps → *New GitHub App* (in your
   organisation).
2. **Webhook URL**: shown on the connection after you create it in Workflow
   (`<WFM_HOOKS_PUBLIC_URL>/hooks/github/<connection id>`). Create the connection in Workflow
   first with a placeholder App id, or edit the App afterwards. **Webhook secret**: any random
   string; enter the same value in Workflow (or leave it empty there and copy the generated one).
3. **Repository permissions**: Contents: *Read & write* (write only for "create branch"; read
   is what the compare API needs for deployments), Pull requests: *Read & write* (write for the
   PR comment), Actions: *Read*, Deployments: *Read*, Metadata: *Read*.
4. **Subscribe to events**: Push, Create, Delete, Pull request, Pull request review, Workflow run,
   Deployment status.
5. Generate a private key, note the App id, and install the App on the repositories you want.
6. In Workflow: *New connection* → GitHub, App id, App slug, private key (PEM), webhook secret.

Installing the App on more repositories sends an `installation_repositories` event and they
appear in Workflow automatically. GitHub Enterprise Server: set the API URL to
`https://github.example.com/api/v3`.

An App created before deployments were supported needs the Deployments permission and the
Deployment status event added; GitHub asks each installation's owner to accept the new
permission.

## GitLab

1. Create a group access token (or project/personal token) with the `api` scope — `read_api` is
   enough if you add webhooks yourself and don't need comments or branch creation (deployments
   only read: the repository compare and commit APIs).
2. In Workflow: *New connection* → GitLab, URL (`https://gitlab.com` or your instance), token,
   optional webhook secret, and **Manage webhooks** to have Workflow create the project hook
   whenever you link a repository. Managed hooks subscribe to push, merge request, pipeline, tag
   push and deployment events; *Sync now* on the connection updates hooks created by earlier
   versions.
3. Without managed webhooks, add a webhook on each project (or group, on Premium) with the URL
   shown on the connection, the secret token, and the triggers *Push events*, *Tag push events*,
   *Merge request events*, *Pipeline events* and *Deployment events*.

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
