package tracker

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

/*
 * A release's page: its progress by status category, a burnup replayed from issue history,
 * readiness warnings, release notes, the Git tag it ships as and the environments it reached.
 * Deployments and tags arrive through the scm package; this file reads them.
 */

// releaseFor loads a release in a project the actor can browse ("not found" otherwise).
func (s *Service) releaseFor(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Release, dbq.Project, authz.Access, error) {
	r, err := s.Q.GetRelease(ctx, id)
	if err != nil {
		return r, dbq.Project{}, authz.Access{}, apperr.NotFound("release")
	}
	p, err := s.Q.GetProject(ctx, r.ProjectID)
	if err != nil {
		return r, p, authz.Access{}, err
	}
	access, err := s.requireAccess(ctx, actor, p, authz.ProjectBrowse)
	if err != nil {
		if apperr.IsNotFound(err) {
			return r, p, access, apperr.NotFound("release")
		}
		return r, p, access, err
	}
	return r, p, access, nil
}

// releaseIssues loads the issues whose fix versions include the release, in key order.
func (s *Service) releaseIssues(ctx context.Context, q *dbq.Queries, releaseID uuid.UUID) ([]dbq.Issue, error) {
	ids, err := q.ReleaseIssueIDs(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	issues, err := q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(issues, func(a, b dbq.Issue) int { return cmp.Compare(a.Number, b.Number) })
	return issues, nil
}

// setReleaseExtras applies the notes and tag parts of a create or update, in its transaction.
func (s *Service) setReleaseExtras(ctx context.Context, q *dbq.Queries, p dbq.Project, r dbq.Release, in ReleaseInput) (dbq.Release, error) {
	if in.Notes != nil || in.NotesPublished != nil {
		notes, published := r.Notes, r.NotesPublishedAt != nil
		if in.Notes != nil {
			notes = strings.TrimRight(*in.Notes, " \t\r\n")
			if len(notes) > 100000 {
				return r, apperr.Invalid("notes", "release notes are limited to 100,000 characters")
			}
		}
		if in.NotesPublished != nil {
			published = *in.NotesPublished
		}
		if published && strings.TrimSpace(notes) == "" {
			return r, apperr.Invalid("notesPublished", "write or generate the release notes before publishing them")
		}
		var err error
		if r, err = q.SetReleaseNotes(ctx, dbq.SetReleaseNotesParams{ID: r.ID, Notes: notes, Published: published}); err != nil {
			return r, err
		}
	}
	if !in.TagRepositoryID.Set && !in.TagName.Set {
		return r, nil
	}
	repoID, name := r.TagRepositoryID, ""
	if r.TagName != nil {
		name = *r.TagName
	}
	if in.TagRepositoryID.Set {
		repoID = in.TagRepositoryID.Value
	}
	if in.TagName.Set {
		name = ""
		if in.TagName.Value != nil {
			name = strings.TrimSpace(*in.TagName.Value)
		}
	}
	// Clearing either half unlinks the tag.
	if in.TagName.Set && name == "" || in.TagRepositoryID.Set && in.TagRepositoryID.Value == nil {
		return q.SetReleaseTag(ctx, dbq.SetReleaseTagParams{ID: r.ID})
	}
	if name == "" {
		return r, apperr.Invalid("tagName", "name the tag")
	}
	if strings.ContainsFunc(name, unicode.IsSpace) || strings.ContainsAny(name, "~^:?*[\\") || strings.Contains(name, "..") || len(name) > 250 {
		return r, apperr.Invalid("tagName", "%q is not a valid Git tag name", name)
	}
	if repoID == nil {
		return r, apperr.Invalid("tagRepositoryId", "choose the tag's repository")
	}
	linked, err := q.RepositoryProjects(ctx, *repoID)
	if err != nil {
		return r, err
	}
	if !slices.ContainsFunc(linked, func(lp dbq.Project) bool { return lp.ID == p.ID }) {
		return r, apperr.Invalid("tagRepositoryId", "the repository is not connected to %s", p.Key)
	}
	var sha *string
	if r.TagSha != nil && r.TagName != nil && *r.TagName == name && r.TagRepositoryID != nil && *r.TagRepositoryID == *repoID {
		sha = r.TagSha
	}
	return q.SetReleaseTag(ctx, dbq.SetReleaseTagParams{ID: r.ID, RepositoryID: repoID, TagName: &name, TagSha: sha})
}

// moveOpenReleaseIssues takes a release's unfinished issues out of it (into moveTo, when given)
// as it ships, recording the fix version change on each so burnups keep their history.
func (s *Service) moveOpenReleaseIssues(ctx context.Context, q *dbq.Queries, actor *authz.Actor, releaseID uuid.UUID, moveTo *uuid.UUID) error {
	issues, err := s.releaseIssues(ctx, q, releaseID)
	if err != nil {
		return err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return err
	}
	before := map[uuid.UUID][]dbq.Release{}
	for _, i := range issues {
		if statuses[i.StatusID].Category == "done" {
			continue
		}
		if before[i.ID], err = q.IssueReleases(ctx, i.ID); err != nil {
			return err
		}
	}
	if err := q.MoveOpenReleaseIssues(ctx, dbq.MoveOpenReleaseIssuesParams{ReleaseID: releaseID, TargetReleaseID: moveTo}); err != nil {
		return err
	}
	for id, prev := range before {
		after, err := q.IssueReleases(ctx, id)
		if err != nil {
			return err
		}
		if releaseRefs(prev) == releaseRefs(after) {
			continue
		}
		if err := s.history(ctx, q, id, actor, "fixVersions", strp(releaseNames(prev)), strp(releaseNames(after)), strp(releaseRefs(prev)), strp(releaseRefs(after))); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// The release page

// ReleaseDetail is a release with its progress, notes, tag and the environments it reached.
func (s *Service) ReleaseDetail(ctx context.Context, actor *authz.Actor, id uuid.UUID) (api.ReleaseDetail, error) {
	r, p, access, err := s.releaseFor(ctx, actor, id)
	if err != nil {
		return api.ReleaseDetail{}, err
	}
	issues, err := s.releaseIssues(ctx, s.Q, r.ID)
	if err != nil {
		return api.ReleaseDetail{}, err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return api.ReleaseDetail{}, err
	}
	prog := api.ReleaseProgress{}
	var done []uuid.UUID
	for _, i := range issues {
		pts := deref0(i.StoryPoints)
		if i.StoryPoints == nil {
			prog.Unestimated++
		}
		prog.Issues.Total++
		prog.Points.Total += pts
		switch statuses[i.StatusID].Category {
		case "done":
			prog.Issues.Done++
			prog.Points.Done += pts
			done = append(done, i.ID)
		case "in_progress":
			prog.Issues.InProgress++
			prog.Points.InProgress += pts
		default:
			prog.Issues.Todo++
			prog.Points.Todo += pts
		}
	}
	v := ReleaseView(r, len(issues), len(done))
	d := api.ReleaseDetail{
		Id: v.Id, Name: v.Name, Description: v.Description, State: v.State, Position: v.Position, StartDate: v.StartDate,
		ReleaseDate: v.ReleaseDate, ReleasedAt: v.ReleasedAt, IssueCount: v.IssueCount, DoneCount: v.DoneCount,
		NotesPublished: v.NotesPublished, NotesPublishedAt: v.NotesPublishedAt, TagName: v.TagName, TagRepositoryId: v.TagRepositoryId,
		ProjectKey: p.Key, Notes: r.Notes, Progress: prog, Environments: []api.ReleaseEnvironment{},
		CanManage: access.Has(authz.ReleaseManage),
	}
	reached := map[string]api.ReleaseEnvironment{}
	if r.TagRepositoryID != nil && r.TagName != nil {
		tag, err := s.releaseTag(ctx, r)
		if err != nil {
			return d, err
		}
		d.Tag = tag
		deps, err := s.Q.TagDeployments(ctx, dbq.TagDeploymentsParams{RepositoryID: *r.TagRepositoryID, Tag: *r.TagName, Sha: deref(r.TagSha)})
		if err != nil {
			return d, err
		}
		// Newest first, so the last one seen per environment is when the tag first got there.
		for _, dep := range deps {
			u := dep.Url
			reached[strings.ToLower(dep.Environment)] = api.ReleaseEnvironment{Environment: dep.Environment, ReachedAt: dep.StatusAt, Via: api.ReleaseEnvironmentViaTag, Url: &u}
		}
	}
	// Otherwise an environment is reached once every done issue was deployed there.
	if len(done) > 0 {
		rows, err := s.Q.IssueEnvironments(ctx, done)
		if err != nil {
			return d, err
		}
		type envIssues struct {
			name  string
			n     int
			at    time.Time
			count map[uuid.UUID]bool
		}
		byEnv := map[string]*envIssues{}
		for _, row := range rows {
			e := byEnv[row.EnvKey]
			if e == nil {
				e = &envIssues{name: row.Environment, count: map[uuid.UUID]bool{}}
				byEnv[row.EnvKey] = e
			}
			e.count[row.IssueID] = true
			if row.FirstAt.After(e.at) {
				e.at = row.FirstAt
			}
		}
		for key, e := range byEnv {
			if _, ok := reached[key]; ok || len(e.count) < len(done) {
				continue
			}
			reached[key] = api.ReleaseEnvironment{Environment: e.name, ReachedAt: e.at, Via: api.ReleaseEnvironmentViaIssues}
		}
	}
	for _, e := range reached {
		d.Environments = append(d.Environments, e)
	}
	slices.SortFunc(d.Environments, func(a, b api.ReleaseEnvironment) int { return a.ReachedAt.Compare(b.ReachedAt) })
	return d, nil
}

// releaseTag describes a release's tag with links to it at the provider and, when the previous
// release has a tag in the same repository, to the changes since.
func (s *Service) releaseTag(ctx context.Context, r dbq.Release) (*api.ReleaseTag, error) {
	repo, err := s.Q.GetScmRepository(ctx, *r.TagRepositoryID)
	if err != nil {
		return nil, nil // the repository is gone; the foreign key clears the link
	}
	c, err := s.Q.GetScmConnection(ctx, repo.ConnectionID)
	if err != nil {
		return nil, err
	}
	t := &api.ReleaseTag{
		Name: *r.TagName, Sha: r.TagSha, LinkedAt: r.TagLinkedAt, Url: TagURL(c.Provider, repo.WebUrl, *r.TagName),
		Repository: api.RepositoryRef{Id: repo.ID, Provider: api.ScmProvider(c.Provider), FullPath: repo.FullPath, WebUrl: repo.WebUrl},
	}
	prev, err := s.Q.PreviousTaggedRelease(ctx, dbq.PreviousTaggedReleaseParams{ProjectID: r.ProjectID, RepositoryID: r.TagRepositoryID, ID: r.ID, Position: r.Position})
	switch {
	case db.IsNoRows(err):
	case err != nil:
		return nil, err
	case prev.TagName != nil:
		u := CompareURL(c.Provider, repo.WebUrl, *prev.TagName, *r.TagName)
		t.PreviousTag, t.CompareUrl = prev.TagName, &u
	}
	return t, nil
}

// TagURL is a tag's page at the provider.
func TagURL(provider, repoURL, tag string) string {
	if provider == "gitlab" {
		return repoURL + "/-/tags/" + url.PathEscape(tag)
	}
	return repoURL + "/tree/" + url.PathEscape(tag)
}

// CompareURL shows the changes between two refs at the provider.
func CompareURL(provider, repoURL, base, head string) string {
	if provider == "gitlab" {
		return repoURL + "/-/compare/" + url.PathEscape(base) + "..." + url.PathEscape(head)
	}
	return repoURL + "/compare/" + url.PathEscape(base) + "..." + url.PathEscape(head)
}

// ---------------------------------------------------------------------------
// Burnup

// fixVersionChange: from `at` the issue was (in) or was not in the release.
type fixVersionChange struct {
	at time.Time
	in bool
}

// releaseMember is an issue in the burnup: its fix version changes for the release, statuses and
// points over time.
type releaseMember struct {
	issue    dbq.Issue
	initial  bool
	changes  []fixVersionChange
	timeline []statusChange
	hist     []historyRow
}

func (m releaseMember) inAt(t time.Time) bool {
	if t.Before(m.issue.CreatedAt) {
		return false
	}
	in := m.initial
	for _, c := range m.changes {
		if c.at.After(t) {
			break
		}
		in = c.in
	}
	return in
}

// namesRelease reports whether a fix version history value names the release: by id when the
// change recorded ids, else by name (history from before ids were recorded, and imports).
func namesRelease(r dbq.Release, byRef bool, ref, value *string) bool {
	if byRef {
		return ref != nil && slices.Contains(strings.Split(*ref, ","), r.ID.String())
	}
	if value == nil {
		return false
	}
	return slices.ContainsFunc(strings.Split(*value, ", "), func(n string) bool { return strings.EqualFold(strings.TrimSpace(n), r.Name) })
}

// ReleaseBurnup replays the release day by day: which issues had it as a fix version (from the
// fix version changes in their history; an issue without any was in it since its creation), their
// story points and whether they were done, at the end of each day.
func (s *Service) ReleaseBurnup(ctx context.Context, actor *authz.Actor, id uuid.UUID) (api.ReleaseBurnup, error) {
	r, p, _, err := s.releaseFor(ctx, actor, id)
	if err != nil {
		return api.ReleaseBurnup{}, err
	}
	current, err := s.Q.ReleaseIssueIDs(ctx, r.ID)
	if err != nil {
		return api.ReleaseBurnup{}, err
	}
	rows, err := s.Q.FixVersionHistory(ctx, dbq.FixVersionHistoryParams{ProjectID: p.ID, ReleaseID: r.ID.String(), Name: r.Name})
	if err != nil {
		return api.ReleaseBurnup{}, err
	}
	changes := map[uuid.UUID][]fixVersionChange{}
	initial := map[uuid.UUID]bool{}
	for _, h := range rows {
		byRef := h.FromRef != nil || h.ToRef != nil
		from, to := namesRelease(r, byRef, h.FromRef, h.FromValue), namesRelease(r, byRef, h.ToRef, h.ToValue)
		if _, seen := changes[h.IssueID]; !seen {
			changes[h.IssueID] = []fixVersionChange{}
			initial[h.IssueID] = from
		}
		if from != to {
			changes[h.IssueID] = append(changes[h.IssueID], fixVersionChange{at: h.CreatedAt, in: to})
		}
	}
	ids := slices.Clone(current)
	source := api.ReleaseBurnupScopeSourceCurrent
	for issueID, cs := range changes {
		if len(cs) > 0 || initial[issueID] {
			source = api.ReleaseBurnupScopeSourceHistory
			if !slices.Contains(ids, issueID) {
				ids = append(ids, issueID)
			}
		}
	}
	issues, err := s.Q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return api.ReleaseBurnup{}, err
	}
	hist, err := s.historyByIssue(ctx, ids)
	if err != nil {
		return api.ReleaseBurnup{}, err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return api.ReleaseBurnup{}, err
	}
	members := make([]releaseMember, 0, len(issues))
	for _, i := range issues {
		m := releaseMember{issue: i, timeline: statusTimeline(i.CreatedAt, i.StatusID, hist[i.ID]), hist: hist[i.ID]}
		if cs, ok := changes[i.ID]; ok {
			m.initial, m.changes = initial[i.ID], cs
		} else {
			m.initial = slices.Contains(current, i.ID)
		}
		members = append(members, m)
	}

	now := s.Now()
	from := dayStart(r.CreatedAt)
	if r.StartDate != nil {
		from = dayStart(*r.StartDate)
	}
	to := dayStart(now)
	end := now
	if r.ReleasedAt != nil && r.ReleasedAt.Before(now) {
		to, end = dayStart(*r.ReleasedAt), *r.ReleasedAt
	}
	if from.After(to) {
		from = to
	}
	out := api.ReleaseBurnup{From: openapi_types.Date{Time: from}, To: openapi_types.Date{Time: to}, ReleaseDate: dateView(r.ReleaseDate), ScopeSource: source, Days: []api.ReleaseBurnupDay{}}
	// Long-running releases are sampled so the chart keeps under about a year of points.
	days := int(to.Sub(from).Hours()/24) + 1
	step := max(1, (days+365)/366)
	for d := 0; d < days; d += step {
		if d+step >= days {
			d = days - 1 // always end on the last day
		}
		day := from.AddDate(0, 0, d)
		at := day.AddDate(0, 0, 1)
		if at.After(end) {
			at = end
		}
		pt := api.ReleaseBurnupDay{Date: openapi_types.Date{Time: day}}
		for _, m := range members {
			if !m.inAt(at) {
				continue
			}
			pts := pointsAt(m.issue.StoryPoints, m.hist, at)
			pt.ScopeIssues++
			pt.ScopePoints += pts
			if st, ok := statusAt(m.timeline, at); ok && statuses[st].Category == "done" {
				pt.DoneIssues++
				pt.DonePoints += pts
			}
		}
		out.Days = append(out.Days, pt)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Readiness

// ReleaseReadiness lists what stands in the way of shipping: issues not done, issues with open
// pull requests, issues whose latest pipeline failed, and a release date that has passed.
func (s *Service) ReleaseReadiness(ctx context.Context, actor *authz.Actor, id uuid.UUID) (api.ReleaseReadiness, error) {
	r, _, _, err := s.releaseFor(ctx, actor, id)
	if err != nil {
		return api.ReleaseReadiness{}, err
	}
	issues, err := s.releaseIssues(ctx, s.Q, r.ID)
	if err != nil {
		return api.ReleaseReadiness{}, err
	}
	out := api.ReleaseReadiness{Warnings: []api.ReadinessWarning{}}
	ids := make([]uuid.UUID, len(issues))
	for k, i := range issues {
		ids[k] = i.ID
	}
	l, err := s.loadLookups(ctx, issues)
	if err != nil {
		return out, err
	}
	prs, err := s.Q.OpenPullRequestsOf(ctx, ids)
	if err != nil {
		return out, err
	}
	pipes, err := s.Q.LatestPipelinesOf(ctx, ids)
	if err != nil {
		return out, err
	}
	links := map[uuid.UUID][]api.ReadinessLink{}
	for _, pr := range prs {
		links[pr.IssueID] = append(links[pr.IssueID], api.ReadinessLink{Title: pr.Title, Url: pr.Url, State: pr.State, Repository: pr.FullPath + "#" + pr.ExternalID})
	}
	failed := map[uuid.UUID]api.ReadinessLink{}
	for _, pl := range pipes {
		if pl.State == "failed" {
			failed[pl.IssueID] = api.ReadinessLink{Title: pl.Title, Url: pl.Url, State: pl.State, Repository: ""}
		}
	}
	var notDone, withPRs, failing []api.ReadinessIssue
	for _, i := range issues {
		ref := l.ref(i)
		if l.statuses[i.StatusID].Category != "done" {
			notDone = append(notDone, api.ReadinessIssue{Issue: ref, Links: []api.ReadinessLink{}})
		}
		if ls := links[i.ID]; len(ls) > 0 {
			withPRs = append(withPRs, api.ReadinessIssue{Issue: ref, Links: ls})
		}
		if f, ok := failed[i.ID]; ok {
			failing = append(failing, api.ReadinessIssue{Issue: ref, Links: []api.ReadinessLink{f}})
		}
	}
	add := func(kind api.ReadinessWarningKind, msg string, is []api.ReadinessIssue) {
		if is == nil {
			is = []api.ReadinessIssue{}
		}
		out.Warnings = append(out.Warnings, api.ReadinessWarning{Kind: kind, Message: msg, Issues: is})
	}
	if len(notDone) > 0 {
		add(api.ReadinessWarningKindNotDone, plural(len(notDone), "issue is", "issues are")+" not done.", notDone)
	}
	if len(withPRs) > 0 {
		add(api.ReadinessWarningKindOpenPullRequests, plural(len(withPRs), "issue has an open pull request", "issues have open pull requests")+".", withPRs)
	}
	if len(failing) > 0 {
		add(api.ReadinessWarningKindFailedPipelines, plural(len(failing), "issue's latest pipeline", "issues' latest pipelines")+" failed.", failing)
	}
	if r.State == "unreleased" && r.ReleaseDate != nil && dayStart(*r.ReleaseDate).Before(dayStart(s.Now())) {
		add(api.ReadinessWarningKindOverdue, fmt.Sprintf("The release date, %s, has passed.", r.ReleaseDate.Format("2 January 2006")), notDone)
	}
	out.Ready = len(out.Warnings) == 0
	return out, nil
}

// plural writes "1 issue is" or "3 issues are".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// ---------------------------------------------------------------------------
// Release notes

// noteSections map issue types to release note headings, in the order the notes list them.
// Other types get a section named after their plural, after these.
var noteSections = []struct {
	title string
	types []string
}{
	{"Features", []string{"story", "feature", "new_feature", "user_story"}},
	{"Bug fixes", []string{"bug", "defect"}},
	{"Improvements", []string{"task", "improvement", "enhancement"}},
}

// noteSection is the heading an issue type's done issues go under, and its order.
func noteSection(t dbq.IssueType) (string, int) {
	norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_") }
	for k, sec := range noteSections {
		if slices.Contains(sec.types, norm(t.Key)) || slices.Contains(sec.types, norm(t.Name)) {
			return sec.title, k
		}
	}
	return pluralName(t.Name), len(noteSections)
}

// pluralName makes an English plural of a type's name: Epic → Epics, Story → Stories.
func pluralName(name string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	switch {
	case name == "":
		return "Other"
	case strings.HasSuffix(lower, "y") && len(lower) > 1 && !strings.ContainsRune("aeiou", rune(lower[len(lower)-2])):
		return name[:len(name)-1] + "ies"
	case strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") || strings.HasSuffix(lower, "z") ||
		strings.HasSuffix(lower, "ch") || strings.HasSuffix(lower, "sh"):
		return name + "es"
	}
	return name + "s"
}

var noteEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`)

// ReleaseNotes writes Markdown release notes for done issues: a section per issue type, a line
// "summary (KEY)" per issue, sub-tasks left out.
func ReleaseNotes(p dbq.Project, issues []dbq.Issue, types map[uuid.UUID]dbq.IssueType) (string, int) {
	type section struct {
		title string
		order int
		level int32
		lines []string
	}
	sections := map[string]*section{}
	n := 0
	for _, i := range issues {
		t := types[i.TypeID]
		if t.Level < 0 {
			continue
		}
		title, order := noteSection(t)
		sec := sections[title]
		if sec == nil {
			sec = &section{title: title, order: order, level: t.Level}
			sections[title] = sec
		}
		sec.lines = append(sec.lines, fmt.Sprintf("- %s (%s)", noteEscaper.Replace(strings.TrimSpace(i.Summary)), issueKey(p, i)))
		n++
	}
	list := make([]*section, 0, len(sections))
	for _, sec := range sections {
		list = append(list, sec)
	}
	slices.SortFunc(list, func(a, b *section) int {
		return cmp.Or(cmp.Compare(a.order, b.order), cmp.Compare(b.level, a.level), cmp.Compare(a.title, b.title))
	})
	var b strings.Builder
	for k, sec := range list {
		if k > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## " + sec.title + "\n\n" + strings.Join(sec.lines, "\n") + "\n")
	}
	return b.String(), n
}

// GenerateReleaseNotes drafts notes from the release's done issues (nothing is saved).
func (s *Service) GenerateReleaseNotes(ctx context.Context, actor *authz.Actor, id uuid.UUID) (api.GeneratedReleaseNotes, error) {
	r, p, _, err := s.releaseFor(ctx, actor, id)
	if err != nil {
		return api.GeneratedReleaseNotes{}, err
	}
	issues, err := s.releaseIssues(ctx, s.Q, r.ID)
	if err != nil {
		return api.GeneratedReleaseNotes{}, err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return api.GeneratedReleaseNotes{}, err
	}
	var done []dbq.Issue
	var typeIDs []uuid.UUID
	for _, i := range issues {
		if statuses[i.StatusID].Category == "done" {
			done = append(done, i)
			typeIDs = append(typeIDs, i.TypeID)
		}
	}
	ts, err := s.Q.GetIssueTypesByIDs(ctx, typeIDs)
	if err != nil {
		return api.GeneratedReleaseNotes{}, err
	}
	types := map[uuid.UUID]dbq.IssueType{}
	for _, t := range ts {
		types[t.ID] = t
	}
	notes, n := ReleaseNotes(p, done, types)
	return api.GeneratedReleaseNotes{Notes: notes, IssueCount: n}, nil
}

// ---------------------------------------------------------------------------
// Settings and tags

// ReleaseSettings says what new Git tags do to a project's releases (off unless set).
func (s *Service) ReleaseSettings(ctx context.Context, actor *authz.Actor, ref string) (api.ReleaseSettings, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return api.ReleaseSettings{}, err
	}
	st, err := s.releaseSettings(ctx, s.Q, p.ID)
	return api.ReleaseSettings{CreateFromTags: st.CreateFromTags, ReleaseOnTag: st.ReleaseOnTag}, err
}

func (s *Service) releaseSettings(ctx context.Context, q *dbq.Queries, projectID uuid.UUID) (dbq.ProjectReleaseSetting, error) {
	st, err := q.GetReleaseSettings(ctx, projectID)
	if db.IsNoRows(err) {
		return dbq.ProjectReleaseSetting{ProjectID: projectID}, nil
	}
	return st, err
}

// UpdateReleaseSettings changes them (project admins).
func (s *Service) UpdateReleaseSettings(ctx context.Context, actor *authz.Actor, ref string, in api.ReleaseSettingsPatch) (api.ReleaseSettings, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return api.ReleaseSettings{}, err
	}
	st, err := s.releaseSettings(ctx, s.Q, p.ID)
	if err != nil {
		return api.ReleaseSettings{}, err
	}
	params := dbq.UpsertReleaseSettingsParams{ProjectID: p.ID, CreateFromTags: st.CreateFromTags, ReleaseOnTag: st.ReleaseOnTag}
	if in.CreateFromTags != nil {
		params.CreateFromTags = *in.CreateFromTags
	}
	if in.ReleaseOnTag != nil {
		params.ReleaseOnTag = *in.ReleaseOnTag
	}
	if st, err = s.Q.UpsertReleaseSettings(ctx, params); err != nil {
		return api.ReleaseSettings{}, err
	}
	Audit(ctx, s.Q, actor, "release.settings_update", "project", p.ID.String(), &p.ID, map[string]any{"createFromTags": st.CreateFromTags, "releaseOnTag": st.ReleaseOnTag})
	return api.ReleaseSettings{CreateFromTags: st.CreateFromTags, ReleaseOnTag: st.ReleaseOnTag}, nil
}

// TagPushed handles a new Git tag in a repository connected to the project: the release named
// like it (exactly, or with or without a leading "v") is linked to it, or created first when the
// project creates releases from tags; with "release on tag", an unreleased release is marked
// released (its unfinished issues keep it as their fix version). A release already linked to
// another tag keeps that link. It reports whether a release was linked.
func (s *Service) TagPushed(ctx context.Context, actor *authz.Actor, p dbq.Project, repo dbq.ScmRepository, tag, sha string) (bool, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return false, nil
	}
	linked := false
	err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		st, err := s.releaseSettings(ctx, q, p.ID)
		if err != nil {
			return err
		}
		r, err := q.ReleaseForTag(ctx, dbq.ReleaseForTagParams{ProjectID: p.ID, Tag: tag})
		switch {
		case db.IsNoRows(err) && !st.CreateFromTags:
			return nil
		case db.IsNoRows(err):
			name := tag
			if len(name) > 80 {
				return nil // longer than a release name may be
			}
			if r, err = q.CreateRelease(ctx, dbq.CreateReleaseParams{ProjectID: p.ID, Name: name}); err != nil {
				return err
			}
			Audit(ctx, q, actor, "release.create_from_tag", "release", r.ID.String(), &p.ID, map[string]any{"name": r.Name, "tag": tag, "repository": repo.FullPath})
		case err != nil:
			return err
		}
		var shaPtr *string
		if sha != "" {
			shaPtr = &sha
		}
		switch {
		case r.TagName == nil:
			if r, err = q.SetReleaseTag(ctx, dbq.SetReleaseTagParams{ID: r.ID, RepositoryID: &repo.ID, TagName: &tag, TagSha: shaPtr}); err != nil {
				return err
			}
		case *r.TagName == tag && r.TagRepositoryID != nil && *r.TagRepositoryID == repo.ID:
			if sha != "" && deref(r.TagSha) != sha {
				if err := q.SetReleaseTagSHA(ctx, dbq.SetReleaseTagSHAParams{ID: r.ID, TagSha: shaPtr}); err != nil {
					return err
				}
			}
		default:
			return nil // linked to another tag already
		}
		linked = true
		if st.ReleaseOnTag && r.State == "unreleased" {
			date := r.ReleaseDate
			if date == nil {
				today := dayStart(s.Now())
				date = &today
			}
			if r, err = q.UpdateRelease(ctx, dbq.UpdateReleaseParams{ID: r.ID, Name: r.Name, Description: r.Description, StartDate: r.StartDate, ReleaseDate: date, State: "released"}); err != nil {
				return err
			}
			Audit(ctx, q, actor, "release.ship", "release", r.ID.String(), &p.ID, map[string]any{"name": r.Name, "tag": tag})
			return s.emit(ctx, tx, actor, p, nil, "release.released", map[string]any{"releaseId": r.ID, "name": r.Name, "tag": tag})
		}
		return publishOnly(ctx, tx, actor, p, nil)
	})
	return linked, err
}

// ---------------------------------------------------------------------------
// Deployments on the issue view

// issueDeployments is one deployment per environment the issue went to: the latest successful
// one containing it, else the latest that failed.
func (s *Service) issueDeployments(ctx context.Context, issueID uuid.UUID) ([]api.IssueDeployment, error) {
	rows, err := s.Q.IssueDeployments(ctx, issueID)
	if err != nil {
		return nil, err
	}
	best := map[string]dbq.IssueDeploymentsRow{}
	var order []string
	for _, d := range rows { // newest first
		key := strings.ToLower(d.Environment)
		cur, seen := best[key]
		if !seen {
			order = append(order, key)
		}
		if !seen || cur.Status != "success" && d.Status == "success" {
			best[key] = d
		}
	}
	out := make([]api.IssueDeployment, 0, len(order))
	for _, key := range order {
		d := best[key]
		out = append(out, api.IssueDeployment{
			Id: d.ID, Environment: d.Environment, Status: api.DeploymentStatus(d.Status), DeployedAt: d.StatusAt, Url: d.Url,
			Ref: d.Ref, Sha: d.Sha, Deployer: d.Deployer,
			Repository: api.RepositoryRef{Id: d.RepositoryID, Provider: api.ScmProvider(d.Provider), FullPath: d.FullPath, WebUrl: d.RepoWebUrl},
		})
	}
	return out, nil
}
