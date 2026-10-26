# Releases

A release (fix version) is a version of the product: a name, optional start and release dates,
a description and a state, *unreleased*, *released* or *archived*. Issues join a release by
having it as a fix version. The project's **Releases** tab lists them with their progress;
each release has its own page.

Creating, editing, shipping and deleting releases, writing their notes and linking their tags
needs the `release.manage` permission (project admins); everyone who can browse the project sees
them.

## The release page

`/projects/KEY/releases/ID`, opened from the release's name in the list:

- **Header**: name, state, dates with how they stand (*3 days left*, *2 days late*), when it was
  released, the description, and *Edit* and *Release*.
- **Git tag**: the tag the release ships as, with a link to it at GitHub or GitLab and, when the
  previous release (in the list's order) has a tag in the same repository, a link to the changes
  between the two. See [Tags](scm.md#tags).
- **Environments**: where the release has been deployed (below).
- **Progress**: issues and story points by status category (done, in progress, to do), as a bar
  and numbers, and how many issues have no story points.
- **Readiness**: what stands in the way of shipping (below).
- **Burnup**: done work against scope at the end of each day, in issues or story points, from the
  start date (or the day the release was created) to today, or to the day it was released; the
  release date is marked. Long releases are sampled to about a year of points.
- **Issues**: the release's issues, filterable by status category and type; each opens beside
  the page.
- **Release notes** (below).

### Burnup scope

Fix version changes are recorded in each issue's history (with the release ids, so a renamed
release keeps its history), including those made on transition screens and when shipping moves
unfinished issues to another release. The burnup replays them: an issue counts from the moment
it was added to the release until it was taken out, and an issue with no such change counts from
its creation. The chart's caption says which applies: *scope follows the fix version changes in
each issue's history*, or, when no issue's fix versions changed since it was created, *scope is
the release's current issues*. Changes recorded before release ids were (and imports from Jira)
are matched by the release's name.

### Readiness

The server checks the release for:

- issues that are **not done**;
- issues with **open pull or merge requests** (open or draft, not merged or closed);
- issues whose **latest pipeline failed**;
- a **release date that has passed** while the release is unreleased.

Each warning lists the issues concerned, with links to the open pull requests and the failed
pipelines. The page shows them, and so does the *Release* dialog before you confirm: shipping
stays possible (*Release anyway*). Shipping can move the unfinished issues to another unreleased
release or take them out of this one.

### Environments

A release has reached an environment when its tag was deployed there (a successful deployment
whose ref is the tag, or whose commit is the tag's), or when every one of its done issues was
deployed there (see [Deployments](scm.md#deployments)). Each environment shows when that
happened; a tag's deployment links to the deployment.

## Release notes

The notes are Markdown kept on the release. **Generate** fills them from the release's done
issues, sub-tasks left out: a section per issue type and a line `summary (KEY)` per issue.
Stories make *Features*, bugs *Bug fixes* and tasks *Improvements* (types with those keys or
names, or *feature*, *defect*, *improvement* and *enhancement*), in that order; any other type
gets a section named after its plural (*Epics*). Generating over notes that differ asks before
replacing them. Edit the result freely, then **Save notes**; **Copy** and **Download .md** take
the text as it stands in the editor.

**Show on the help centres' What's new** publishes the saved notes: service desks that chose the
project show them to their customers (see [service-desk.md](service-desk.md#whats-new)). Turning
it off takes them off at once. Publishing and unpublishing are recorded in the audit log.

## Tags and deployments

Project settings → Releases decides what a new tag does: link the release named like it always,
and, when turned on, create a release for each new tag and mark a release released when its tag
is pushed. [scm.md](scm.md#tags) has the details, and how deployments are recorded and matched to
issues.

## API

| | |
|---|---|
| `GET /projects/{key}/releases`, `POST …` | list (with `notesPublished`, `tagName`) and create |
| `GET /releases/{id}` | the page: progress, notes, tag with its links, environments, `canManage` |
| `PATCH /releases/{id}` | name, dates, state, `notes`, `notesPublished`, `tagRepositoryId` and `tagName` (both `null` unlink the tag) |
| `POST /releases/{id}/release` | ship it (`moveOpenIssuesTo`) |
| `GET /releases/{id}/readiness` | `{ready, warnings: [{kind, message, issues: [{issue, links}]}]}`; kinds `not_done`, `open_pull_requests`, `failed_pipelines`, `overdue` |
| `GET /releases/{id}/burnup` | `{from, to, releaseDate, scopeSource, days: [{date, scopeIssues, doneIssues, scopePoints, donePoints}]}` |
| `GET /releases/{id}/notes/generated` | notes generated from the done issues, not saved |
| `GET`/`PATCH /projects/{key}/release-settings` | `createFromTags`, `releaseOnTag` (project admins change them) |
| `GET /portal/projects/{key}/whats-new` | a desk's published release notes, for the help centre |

Issues carry `deployments` (one per environment) and board cards `dev.deployedTo`.
[`api/openapi.yaml`](../api/openapi.yaml) has the details.
