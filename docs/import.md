# Import from Jira

Organisation admins can bring projects over from **Jira Cloud** or **Jira Data Center / Server**
under Administration → Import from Jira, with their history: issues keep their keys, and sprint
reports, cycle time, time in status and cumulative flow cover the imported past. A team can keep
using Jira during the move and re-run the import to pick up what changed.

## The wizard

1. **Connect.** Jira Cloud: the site URL (`https://acme.atlassian.net`), the email of an
   Atlassian account and an [API token](https://id.atlassian.com/manage-profile/security/api-tokens)
   (REST API v3). Data Center / Server: the base URL and a personal access token (Profile →
   Personal Access Tokens, REST API v2). *Test connection* checks the site, the edition and the
   credentials. The token is stored encrypted with `WFM_SECRET_KEY` and never shown again; delete
   it once you are done (the import record and its mapping stay). The account needs *Browse
   projects* on what you import; nothing is ever written to Jira.
2. **Projects.** Choose Jira projects and, for each, the project it becomes: key (the Jira key
   by default), name, template (Scrum, Kanban, Bug tracking, or *from the Jira boards*: a scrum
   board means Scrum, service projects Bug tracking, anything else Kanban) and access (open to
   members, or private — Jira permissions are not imported). *Save and analyse* starts the scan.
3. **Mapping.** The scan reads every issue of the selected projects once, in the background, and
   counts issues, comments, attachments, sprints and versions, and the statuses, issue types,
   priorities, people and story point fields in use. The mapping is pre-filled and editable:
   - **statuses** → a status of the target workflows: same name, else the first status of the
     same category (*To Do*, *In Progress*, *Done*). Where an issue type's workflow lacks the
     chosen status (the Scrum template's sub-tasks have no *In Review*), its issues use the first
     status of the same category in that workflow; the dry run says so.
   - **issue types** → epic, standard and sub-task types by hierarchy level, others by name,
     unknown ones (Spike, Initiative…) to Task.
   - **priorities** → Highest … Lowest by name; Blocker/Critical → Highest, Major → High,
     Minor → Low, Trivial → Lowest, others Medium.
   - **story points** from the number field named *Story Points* or *Story point estimate* that
     most issues use (or none).
   - **components** as labels `component:<name>`, and whether to download attachments.
   - **people**, see below.
4. **Dry run.** Counts per project and a list of problems — unmapped values, status fallbacks,
   key changes and clashes, people without email, attachments over the size limit — without
   writing anything. Blocking problems (a key already used here, missing credentials, a stale
   analysis) disable the import.
5. **Import.** Runs in the background with progress per phase, can be stopped and resumed, and
   ends with a report: counters and every problem met (with example issue keys).

## What is imported

| Jira | Here |
|---|---|
| Project | A project from the chosen template; you become its admin. A different key keeps the Jira key resolving (key alias). |
| Issue key and number | Kept: PLAT-123 stays PLAT-123 (with another key, NEW-123). The project's next number continues above the highest Jira number; gaps stay gaps. |
| Summary, description | Atlassian Document Format (Cloud) or wiki markup (Data Center) converted to Markdown: headings, emphasis, code, quotes and panels, lists and task lists, tables, links, mentions, images and attachments (linked to the imported files). |
| Type, status, priority | Mapped as above. Resolution: Jira's resolution name and date when the status is in the done category (*Done* and the time it was resolved otherwise). |
| Assignee, reporter (or creator) | People, see below. Active accounts become watchers. |
| Labels, components | Labels (spaces become dashes, at most 30); components as `component:<name>`. |
| Created, updated, resolved, due date, start date | Kept. |
| Parent (Cloud `parent`, Data Center *Epic Link*), sub-tasks | Parent links, when they fit the epic → story → sub-task hierarchy after type mapping and stay within the project. Sub-tasks follow their parent's sprint. |
| Story points | The chosen field. |
| Sprints (boards' sprints, and sprints only issues mention) | Sprints with name, goal, state, dates; closed sprints keep their completion time. Each issue is in its current sprint (its open one, else the last); one active sprint per project (parallel active sprints become future). The commitment and completion snapshots of started sprints are replayed from history, so sprint reports, burndowns and velocity cover them. |
| Fix versions | Releases (released, archived, dates), assigned to issues. |
| Comments | With authors, times and edits. |
| Attachments | Downloaded and stored like uploads, up to `WFM_ATTACHMENT_MAX_MB` each; larger ones are skipped and reported. |
| Issue links | Blocks, Duplicate, Cloners and Relates; other link types become *relates to*. Links to issues outside the imported projects are skipped. |
| Rank | Backlog order follows Jira's rank. |
| Changelog | Issue history with the original times and authors: status changes (with the mapped status ids), assignee and reporter, sprint, story points, priority, type, summary, resolution, labels, fix versions, due date, parent, attachments and links. Cycle time, lead time, time in status, cumulative flow and sprint reports are computed from it. |

Not imported: watchers and votes, worklogs, custom fields other than story points and start date,
Jira permissions, workflows themselves (issues are mapped onto the template's), boards'
configuration, filters and dashboards. Automation does not run and no notifications are sent for
imported changes.

## People and placeholders

A Jira person becomes, in this order: what you chose in the mapping (an account here, a
placeholder, or nobody); the account an earlier run mapped them to; the account with the same
email; otherwise a **placeholder**.

A placeholder is an inactive account with the person's name and email that owns their issues,
comments and history. It has no password or token and cannot sign in. When the person later signs
in with that email through single sign-on — while just-in-time provisioning is allowed
(`WFM_JIT_PROVISIONING`) — the placeholder becomes their account, with everything attached; Google
directory sync and SCIM adopt it the same way when they provision that email. Without
just-in-time provisioning, signing in is refused until SCIM or the directory provisions the
person. Placeholders appear as inactive users in Administration → Users; activating one there by
hand also makes it a normal account.

Jira Cloud hides email addresses unless people make them visible, so many Cloud people arrive
without one. They get a reserved address (`jira-<account id>@<site>.invalid`) that no identity
provider verifies, so nobody can claim them: map them to an account or enter their email in the
mapping step (the dry run lists them).

## Re-runs, resuming and limits

- A mapping table from Jira ids to ids here (per site) makes every run idempotent: unchanged
  issues are skipped, changed ones are updated from Jira (their fields, and their imported history
  and comments; comments and history added here stay), new ones are created. Deleting the import record keeps the
  mapping, so importing the same site again still updates rather than duplicates. Changing the
  mapping rewrites the issues on the next run.
- A run works in checkpointed steps (a page of 50 issues at a time). If it fails, is stopped, or
  the worker restarts, *Resume import* continues from the last checkpoint. Long runs hand over to
  a new background job every few minutes.
- Jira's rate limits are respected: `429` and `503` answers wait for `Retry-After` (else back off
  exponentially, up to a minute), other server errors are retried a few times.
- An issue number already taken by an issue created here (outside the import) is skipped and
  reported.
- Moving issues between Jira projects after an import, or deleting issues or links in Jira, is
  not mirrored: moved issues stay where they were imported (and are reported), deleted ones stay.
- The importer reads Jira with JQL `project = "KEY" AND key > "KEY-n" ORDER BY key`; it needs the
  enhanced search (`/rest/api/3/search/jql`) on Cloud and `/rest/api/2/search` on Data Center,
  and Jira Software's agile API for sprints (without it, sprints come only from issues).
- Memory stays bounded: issues are read and written a page at a time, attachments are streamed
  to object storage (the PostgreSQL blob store holds one attachment in memory at a time), and only
  lookups (people, sprints, versions) are kept in memory.

## Network

Jira calls go through the same SSRF guard as webhooks: private and loopback addresses are refused.
For a Data Center instance on a private network, add its host to `WFM_OUTBOUND_ALLOWED_HOSTS`
(see [configuration.md](configuration.md)).

## Trying it locally

`go run ./internal/jiraimport/jiratest/fakejira -addr 127.0.0.1:8599` serves the fake Jira Cloud
used by the tests (`-flavor datacenter` for Data Center) and prints its credentials. Start the
server with `WFM_OUTBOUND_ALLOWED_HOSTS=127.0.0.1` (or `WFM_WEBHOOK_ALLOW_PRIVATE=true`) and run the
wizard against `http://127.0.0.1:8599`. `E2E_FAKE_JIRA=http://127.0.0.1:8599 npx playwright test
jira-import` runs the wizard's end-to-end test against it.

---

Jira is a trademark of Atlassian. Workflow is not affiliated with, endorsed or sponsored by
Atlassian; Jira's REST API is used with your own site and credentials to move your data.
