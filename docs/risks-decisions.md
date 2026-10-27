# Risks and decisions

A project can keep a **risk register** and a **decision log** next to its work. Risks and
decisions are issues of special issue-type kinds, so they have keys, history, comments, watchers,
notifications, permissions, attachments, search, automation, the API and the MCP tools like any
issue. They live in their own **Risks** and **Decisions** tabs and never in planning: work links
to them instead ("this task mitigates that risk", "this story implements that decision").

## Turning it on

New projects from the **Scrum**, **Kanban** and **Bug tracking** templates start with risks and
decisions on; **Service desk** projects start without them. Project settings → General →
**Risks and decisions** turns the section on or off:

- **On** adds whatever the project is missing: the **Risk** and **Decision** issue types (keys
  `risk` and `decision`), their workflows (project workflows named Risk and Decision) and their
  fields. Turning it on again installs nothing twice.
- **Off** hides the tabs. The risks and decisions, their types, workflows and fields stay, and
  the issues stay in search; turning the section on again brings the tabs back as they were.

A project that already has a work issue type keyed `risk` or `decision` cannot turn the section
on (409 `type_conflict`) until that type is deleted or its issues move to another type.

## Kept out of planning

| Never in | Still in |
|---|---|
| Boards (and their columns: risk and decision statuses are never reported as unmapped) | Search, saved filters and the project's Issues list |
| The backlog and sprints: the API refuses a sprint for them (400) | The issue view, comments, attachments, watchers and notifications |
| The timeline | Automation rules |
| Sprint reports, burndown, burnup and velocity | Links to and from any issue |
| Flow metrics and reports: summary, cumulative flow, created vs resolved, cycle time and breakdowns, and the dashboard gadgets built on them | Time tracking |
| Service desk queues and SLA clocks | The REST API and the MCP tools |

Risks and decisions have no parent and no children: they are not part of an epic and have no
sub-tasks. Changing a work issue's type to a risk or decision type takes it out of its sprint and
its epic (both recorded in its history); an issue with sub-tasks can't change.

Delivery metrics always count work only, whatever their query says. Dashboard **issue lists**
show what their query matches; the *My work* dashboard template asks for `kind = work`.

## Issue type kinds

Every issue type has a **kind**: `work` (every type a template creates, and the default),
`risk` or `decision`. The kind is set when the type is created and fixed after; risk and decision
types are standard issue types (level 0). Settings → Issue types shows each type's kind and can
create more types of a kind, for example a *Security risk* type: it gets the kind's fields.

## Risks

| Field | Key | Values |
|---|---|---|
| Risk likelihood | `risk_likelihood` | 1 Rare, 2 Unlikely, 3 Possible, 4 Likely, 5 Almost certain |
| Risk impact | `risk_impact` | 1 Negligible, 2 Minor, 3 Moderate, 4 Major, 5 Severe |
| Risk score | `risk_score` | likelihood × impact, 1 to 25; set by Workflow |
| Risk rating | `risk_rating` | Low, Medium, High or Critical, from the score; set by Workflow |
| Risk response | `risk_response` | Avoid, Reduce, Transfer or Accept |
| Mitigation plan | `risk_mitigation` | Markdown text |
| Review date | the due date | when to look at the risk again |
| Owner | the assignee | who looks after the risk |

### Scoring

The score is the likelihood times the impact, and the rating its band:

| Rating | Score |
|---|---|
| Low | 1–4 |
| Medium | 5–9 |
| High | 10–16 |
| Critical | 20–25 |

No product of two numbers from 1 to 5 falls between 16 and 20. The score and rating are worked
out whenever the likelihood or impact changes (and recorded in the history); they are cleared
while either is missing, and setting them directly is refused (400). The likelihood and impact
must be whole numbers from 1 to 5.

### Workflow

**Open** → **Mitigating** → **Closed**. *Close* asks for a resolution — **Mitigated**,
**Accepted**, **Occurred** or **No longer relevant** — and *Reopen* brings a closed risk back.
The workflow is a project workflow like any other ([workflows.md](workflows.md)); a resolution
field on a transition screen lists the resolutions it offers with `options`.

### The Risks tab

- **Open risks**, **High and critical** (open risks scoring 10 or more) and **Reviews overdue**
  (open risks past their review date).
- The **likelihood × impact** heat map of open risks: each cell shows how many risks it holds and
  its score on its rating's colour. Pick a cell to list its risks; pick it again to show every
  cell.
- The **register**: key, risk, score and rating, owner, status, review date and mitigating work
  ("2 of 3 done"), highest score first, for open, closed or all risks.
- **New risk** opens the create dialog set to Risk, with the assessment fields and no sprint,
  epic or story points.

### Mitigating work

Work mitigates a risk with a `mitigates` link (*Integrate a second provider* mitigates *Payment
provider outage*). The risk's issue view lists its mitigating work with how much is done, and
**Create mitigating issue** opens the create dialog for an issue already linked to the risk.

## Decisions

| Field | Key | |
|---|---|---|
| Decision context | `decision_context` | the situation and the forces that call for a decision (Markdown) |
| Options considered | `decision_options` | the alternatives weighed (Markdown) |
| Decision | `decision_outcome` | what was decided, and why (Markdown) |
| Consequences | `decision_consequences` | what becomes easier or harder (Markdown) |
| Decided on | `decision_date` | set to the day the decision is accepted or rejected, unless it already has a date |

### Workflow

**Proposed** → *Accept* → **Accepted**, or *Reject* → **Rejected**; an accepted decision is
*Superseded* by a newer one; *Reopen* returns any of them to Proposed. The resolution follows the
outcome (Accepted, Rejected, Superseded).

### Superseding

On an accepted decision, **Supersede** picks the decision that replaces it, or creates one, then
links the newer decision to it with `supersedes` and marks it Superseded in one step
(`POST /issues/{key}/supersede`). The workflow's *Supersede* transition is only offered once a
newer decision supersedes it.

### The Decisions tab

The log lists each decision with its status, when it was decided, the decisions that supersede it
and the work implementing it, newest first and filtered by status. **New decision** opens the
create dialog set to Decision. A decision's issue view shows its record and **Export as
Markdown** downloads it as an architecture decision record:

```markdown
# PLAT-41: Store sessions in PostgreSQL

- Status: Superseded
- Date: 2026-10-01
- Superseded by: PLAT-57 (Store sessions in Redis)

## Context

Sessions live in memory and are lost on restart.

## Options considered

…

## Decision

…

## Consequences

…
```

## Links

| Type | Outward | Inward | Joins |
|---|---|---|---|
| `mitigates` | mitigates | is mitigated by | a work issue to the risk it mitigates |
| `implements` | implements | is implemented by | a work issue to the decision it implements |
| `supersedes` | supersedes | is superseded by | a decision to the earlier decision it replaces |

Other pairs are refused (400). Link from either end: on a risk, *is mitigated by* picks the work.

## Search

The `kind` field (`work`, `risk` or `decision`) finds every risk or decision whatever its type,
and the fields can be used by name or key ([search.md](search.md)):

```
type = risk AND "Risk score" >= 10
kind = risk AND "Risk rating" in (High, Critical) AND statusCategory != Done
kind = risk AND due < startOfDay() AND statusCategory != Done          reviews overdue
kind = risk AND assignee = currentUser() ORDER BY due
"Risk likelihood" = 4 AND "Risk impact" = 5                             one cell of the matrix
issue in linkedIssues("PLAT-40", "is mitigated by")                    the work mitigating a risk
kind = risk AND issueLinkType != "is mitigated by" AND statusCategory != Done
kind = decision AND status = Accepted AND "Decided on" >= startOfYear()
issue in linkedIssues("PLAT-41", "is superseded by")                   what replaced a decision
```

## Automation

Project settings → Automation offers **Remind risk owners about reviews due this week**: every
Monday at 08:00 (UTC) it notifies the owner of each open risk whose review date is in the next
seven days or has passed — a `schedule` trigger with the query `kind = risk AND statusCategory !=
Done AND due <= 7d AND assignee is not EMPTY` and a `notify` action to the assignee. See
[automation.md](automation.md).

## API

| Endpoint | |
|---|---|
| `POST /projects` | `risksDecisions` turns the section on or off for the new project (default: the template's) |
| `PATCH /projects/{key}` | `risksDecisions: true` installs what is missing and shows the section; `false` hides it |
| `GET /projects/{key}/risks` | the register: `summary` (open, high or critical, reviews overdue, unassessed), the 25-cell `matrix` of open risks, and `risks` with `likelihood`, `impact`, `score`, `rating`, `response` and `mitigation` (`{total, done}`) |
| `GET /projects/{key}/decisions` | the log: `decisions` with `decidedOn`, `supersedes`, `supersededBy` and `implementation` (`{total, done}`) |
| `POST /issues/{key}/supersede` | `{supersededByKey, version?, comment?}`: link and transition in one step |
| `GET /projects/{key}/issue-types` | each type's `kind`; `POST` takes `kind` |

Issues carry their type's `kind` (`type.kind`), and the fields go in `fields` by key:

```sh
curl -X POST $WFM/api/v1/issues -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d '{"projectKey":"PLAT","typeKey":"risk","summary":"Payment provider outage","dueDate":"2026-11-30",
       "fields":{"risk_likelihood":4,"risk_impact":5,"risk_response":"Reduce"}}'
curl -X POST $WFM/api/v1/issues/PLAT-58/links -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d '{"type":"mitigates","targetKey":"PLAT-40"}'
```

The fields' keys are reserved: `GET /fields` marks them `reserved`, and creating, changing or
deleting them answers 409 `field_reserved`.

Assistants use the same issues through the [MCP tools](mcp.md): `create_issue` and `update_issue`
take `fields`, and `link_issues` takes `mitigates`, `implements` and `supersedes` (or the inward
wording, *is mitigated by*).
