# Service desk

A service project is a help desk: the people you support (customers) raise requests in a
**help centre** at `/portal`, through forms you design, and follow them there; your team
(agents) works the requests in the normal app, from **queues**, against **SLAs**, with
**approvals** where a request needs one. Customers see the replies meant for them and never
the team's internal notes.

This is a first cut. What it covers, and what it does not yet, is listed under
[Scope](#scope) at the end.

## Setting up a help desk

1. **Projects → New project**, template **Service desk**. Service projects are private unless
   you choose otherwise: everyone who can browse the project works there as an agent and reads
   internal notes, so only add your team (Project settings → Members). An open service project
   shows a warning in its help centre settings.
2. The project comes ready to use: a *Service request* workflow, three issue types (Service
   request, Incident, Change), four starter request types, six queues and SLAs on business
   hours (Monday to Friday, 09:00–17:00 UTC). Adjust them in **Project settings**: *Help
   centre*, *Request types*, *Queues* and *SLAs*.
3. **Customers** (a tab of the project): add the people you support by email. Someone without
   an account gets a customer account and an invitation email to choose a password (this needs
   local accounts, see [identity.md](identity.md#local-accounts)); people who already have an
   account are just added. Without an SMTP server the invitation is created but reaches nobody:
   the page says so (the API's `emailed` is `false`), and an administrator can set up email or
   give the customer a temporary password. Agents with the `customer.manage` permission
   (project `member` and `admin` roles) add and remove customers.
4. Share the help centre's address, `WFM_PUBLIC_URL/portal/KEY`, shown in Project settings →
   Help centre.

### Who can raise requests

Project settings → Help centre → *Who can raise requests*:

- **Customers on the list** (the default): the people on the Customers page, plus the
  project's own members.
- **Customers and everyone in the organisation**: organisation members (not guests) can raise
  requests too without being listed.

**Self sign-up** (off by default) lets anyone with the sign-up link
(`/portal/signup/KEY`) create a customer account. They choose a password from a link emailed
to them, so the address is confirmed before the account can be used; an address that already
has an account gets an email saying so instead (the page answers the same either way, so it
does not reveal who has an account), and only customer accounts are added to the desk that way.
Addresses in the organisation's sign-in domains (`WFM_ALLOWED_DOMAINS` or a provider's own) can't
sign up: those people get their account from the identity provider. Sign-ups are throttled like
password resets.

## Customers

A customer is a person with the organisation role `customer`:

- They only use the help centre. The agent app sends them to `/portal`, and every API endpoint
  outside the portal, their own profile, sign-in and email settings answers `403 portal_only`
  — search, issues, comments, history, attachments, live updates, MCP and tokens included.
- They hold no project permissions: a group mapped to an organisation role does not raise a
  customer's role, and a customer cannot be added as a project member.
- They see a request when they raised it, when an agent shared it with them (*Shared with*,
  in the issue's Service request panel), or when they are asked to approve it.
- They sign in with a password (local accounts) or, if your identity provider lets them in,
  with SSO: an account created as a customer stays a customer when its owner signs in with SSO
  (matched by verified email). Organisation members can also be customers of a desk; they keep
  their normal access.

## Request types and forms

A request type is something people can ask for (*Get IT help*, *Request new hardware*). Each
has a name, description, icon, a group heading in the help centre, an order, the issue type it
creates, an ordered form, and optionally an approval.

The **form** maps each question to an issue field: summary, description, priority, due date, or
a custom field of type text, URL, number, date, checkbox, select or multiselect that applies to
the project and issue type (user and version fields are not offered: they would show customers
the project's people and releases). Each question has a label, help text and *required*.
Without a summary question, the request type's name becomes the summary.

An issue with a request type is a **service request**: its reporter and the people it is shared
with see it in the help centre. Agents can create issues in a service project as usual; they
are not shown to anyone in the help centre until given a request type (Service request panel →
*Request type*).

## The conversation: replies and internal notes

In a service project every comment is either

- a **reply to the customer** — shown in the help centre and emailed to the customer, or
- an **internal note** — seen only by agents.

The comment box offers **Add internal note** and **Reply to customer**; internal notes are
tinted and labelled in the issue. Comments are internal unless an agent chooses to reply:
comments from automation rules, smart commits and transition screens are internal (a transition
screen has a *Send this comment to the customer* box). Through the API, `visibility: public`
on `POST /issues/{key}/comments` (or `commentVisibility` on a transition) makes a reply; MCP's
`add_comment` takes `visibility` too, and `get_issue` labels each comment. Comments customers
write in the help centre are replies, and a reply from the customer moves a request that was
*Waiting for customer* back to *Waiting for support*.

The history records which kind each comment was (*added an internal note*, *replied to the
customer*); webhook and automation payloads for `comment.created` carry
`data.visibility`. Software projects' comments are unchanged.

Files customers attach in the help centre are visible to agents and the customer; files agents
attach are internal (labelled *agents only*).

## Queues

Queues are named, ordered searches within the project, with live counts, on the project's
**Queues** tab — the default view of a service project. The defaults are *Unassigned*,
*Assigned to me*, *Waiting for customer*, *SLA at risk*, *SLA breached* and *All open*;
project admins edit them in Project settings → Queues. Every queue row shows the request's
SLA clocks; opening a row opens the request beside the queue.

## SLAs

Two clocks run on every service request that a goal applies to:

- **Time to first response** — from creation until an agent first replies to the customer (an
  internal note does not count), or the request is resolved without a reply. It runs once.
- **Time to resolution** — from creation until the request enters a done status. Reopening it
  starts a new cycle with a fresh goal.

**Goals** (Project settings → SLAs) are tried in order; the first whose conditions all match
the request applies: priorities, request types and a query (each optional), a target (in
working minutes or hours) and a **calendar**. A request no goal matches has no clock for that
SLA. **Pause statuses** stop a clock while the request is in them (by default *Waiting for
customer* and *Pending approval*).

**Calendars** say what working time is: a time zone, working hours per weekday (several spans
a day are allowed; `24:00` ends a day at midnight) and days off. A goal without a calendar
counts around the clock. Working time follows the time zone's clock, so daylight-saving
changes move the working hours with local time.

A clock is **breached** once its working time passes the target. A job checks every minute and
raises the `sla.breached` event once per clock: automation rules can trigger on it, it is
delivered to webhooks subscribed to it, and the assignee gets an *SLA breaches* notification.

Clocks are stored per request and recomputed from the request's history whenever it changes
(status, priority, request type, comments) in the same transaction; saving the SLA settings
recomputes every open request in the background. The issue view's Service request panel shows
each clock (time left, paused, met or missed) with its goal and calendar; queues show them too.
Times are working time, so they are written in hours (`40h` is a business week), never days.

In searches (see [search.md](search.md#service-desk)):

```
"Time to first response" = breached()
"Time to resolution" < remaining("2h") AND "Time to resolution" > remaining("0m")
"Time to resolution" = paused() ORDER BY "Time to resolution"
```

## Approvals

A request type can ask for approval before the team starts work: named people and/or the
members of a group (as of when the request is raised), approved when **any one** or **all** of
them approve; one decline declines. Whoever raised the request is never one of its approvers, so
nobody approves their own request (a request whose only approver is its reporter goes ahead
without one). The request waits in **Pending approval** (its SLA clocks
paused), approvers are emailed, and they decide — with an optional comment — from the help
centre (**Approvals**, also for approvers who are not customers) or the issue's Service request
panel. The decision moves the request to the request type's *approved* status (default
*Waiting for support*) or *declined* status (default *Declined*, resolution *Won't Do*), and each
decision is recorded in the history. People cannot run the workflow's approval transitions
(*Request approval*, *Approve*, *Decline*): their condition reserves them for the service desk
(and automation rules), so nobody skips an approval by changing the status. A request that has
left *Pending approval* (an agent or a rule moved it on) no longer waits for its approval: it
drops off the approvers' lists, and a late decision is refused rather than moving it again.

## The help centre

`/portal` has its own layout for customers and anyone raising requests:

- **Get help**: the desks you can raise requests in, then a desk's request types by group,
  then the request type's form.
- **My requests**: what you raised or what was shared with you, open or closed, most recently
  updated first. A request's *last update* is the latest thing its customer can see: a reply
  from either side, a status change or a file added in the help centre. Internal notes and
  other changes agents make (the assignee, labels) don't move it.
- A **request**: status, details from the form, the conversation (replies only) with a reply
  box, files (attach yours), approvals, and who it is shared with.
- **Approvals**: requests waiting for your decision.
- **Profile**: your account, password and two-factor authentication, and request emails.
- **What's new**: a desk's page of release notes, linked from the desk (below).

It works on phones, follows the light/dark theme, and its pages are part of the accessibility
checks in the end-to-end tests.

### What's new

A help centre can tell its customers what changed in the products behind it. In Project settings
→ Help centre → *What's new*, a project admin of the desk chooses the software projects whose
release notes it shows; they can only choose projects they can browse, and not service projects.
In those projects, people with `release.manage` publish a release's notes from the release page
(*Show on the help centres' What's new*, see [releases.md](releases.md#release-notes)).

Everyone who may raise requests in the desk then finds **What's new** on the desk's page
(`/portal/KEY/whats-new`): the published notes of the chosen projects, newest first, each with
the product's and the release's names, its date (when it was released, else its planned date)
and the notes rendered as Markdown (raw HTML is not rendered, and issue keys are not links).
Nothing else about the releases reaches the help centre: no issues, no statuses, no people.
Unpublishing the notes, or removing the project from the desk, takes them off at once. Someone who
may not raise requests in the desk gets `404`, like every other portal page; the integration tests
check that customers cannot read unpublished notes, notes of projects the desk did not choose, or
another desk's notes.

## Emails

Customers get emails about their requests (notification kind **My requests**): the request was
received (with its key), an agent replied, the request is waiting for them, and it was
resolved. Approvers get **Approvals** emails when their decision is needed. Both are sent
immediately (no digest), link to the help centre and carry the usual unsubscribe link and
*Manage email notifications* (the help centre profile). People who work in the project get
the normal agent notifications instead; internal notes are never in a customer's email. See
[notifications.md](notifications.md).

## Security model

- The `customer` organisation role grants nothing in the authorisation model (`internal/authz`);
  the portal acts for the customer through a portal actor scoped to one desk, created only
  after the service desk has checked the request's visibility (reporter, participant or
  approver) or the right to raise requests.
- An HTTP allowlist keeps customers to `/api/v1/portal/…`, sign-in and their own profile,
  password, two-factor and email settings; everything else answers `403 portal_only`.
- Portal endpoints answer `404` for anything a customer may not see, whether or not it exists.
- Portal responses are built from separate, minimal types: public comments and attachments
  only, people's names and avatars without email addresses, no internal fields.
- The integration tests in `internal/integration/servicedesk_test.go` check that customers can
  never list or read other customers' requests or see internal notes — through the portal and
  agent APIs, search, live updates, emails, attachments, history, MCP and webhook payloads —
  and cannot use search or any agent endpoint.

## API

Agents: `GET`/`PATCH /projects/{key}/service-desk` (`releaseNotesProjectKeys` chooses What's
new's projects), `…/request-types`,
`…/customers`, `…/queues` (and `/queues/{id}/issues`), `GET`/`PUT /projects/{key}/slas`,
`GET`/`PATCH /issues/{key}/request` (request type, SLAs, approvals, participants),
`POST /issues/{key}/participants`, `POST /issues/{key}/approvals/{id}`.

Help centre: `GET /portal/projects`, `GET /portal/projects/{key}`,
`GET /portal/projects/{key}/whats-new`, `POST /portal/projects/{key}/requests`, `GET /portal/requests`, `GET /portal/requests/{key}`,
`POST …/comments`, `POST …/attachments`, `POST …/approval`, `GET /portal/approvals`,
`GET /portal/attachments/{id}`, and `GET`/`POST /portal/signup/{key}` (signed out).
[`api/openapi.yaml`](../api/openapi.yaml) has the details.

## Scope

Included in this first cut: service projects, request types with forms, customers (invited,
SSO or self-signed-up), the help centre, replies and internal notes, queues, SLAs with
calendars and breach events, approvals, customer and approver emails, and What's new.

Not included yet:

- **Inbound email**: customers cannot raise or answer requests by email, and replies to
  notification emails are not read. Email channels per desk, and email-to-request, are future
  work.
- A knowledge base, canned responses, customer satisfaction surveys, organisations of
  customers, and SLA reports.
- Changing a comment's visibility after posting it (delete and re-post instead); sharing an
  agent's attachment with the customer (paste a link in a reply, or ask them to attach it).
- Automation comments are always internal; there is no rule action that replies to the customer.
- The service desk relies on the workflow's status names *Pending approval* and *Waiting for
  customer* (approvals and the customer-reply rule); renaming those statuses turns those
  features off for the desk.
- Recomputing an SLA applies the current goal and calendar to the whole history of a request;
  clocks do not remember the goal that applied at the time.
- `sla.breached` is raised by a check that runs every minute. A clock that goes over its goal and
  is then stopped (the request resolved, or the first reply sent) before the check sees it, for
  example while the workers are down, raises no event; the breach still shows on the request. A
  clock that was paused meanwhile raises it when it runs again.
