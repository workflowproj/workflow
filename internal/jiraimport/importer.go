package jiraimport

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/rank"
	"github.com/workflowproj/workflow/internal/tracker"
	"github.com/workflowproj/workflow/internal/workflow"
)

const (
	issuePageSize    = 50
	relationPageSize = 500
)

// importStep runs one step of the import phases, in order:
// projects → versions → sprints → issues → relations → ranks → sprint reports.
func (r *run) importStep(ctx context.Context) (bool, error) {
	if _, err := r.loadMeta(ctx); err != nil {
		return false, err
	}
	var err error
	switch r.cur.Phase {
	case "", "projects":
		err = r.importProjects(ctx)
	case "versions":
		err = r.perProject(ctx, "versions", "sprints", r.importVersions)
	case "sprints":
		err = r.perProject(ctx, "sprints", "issues", r.importSprints)
	case "issues":
		err = r.importIssuesStep(ctx)
	case "relations":
		err = r.relationsStep(ctx)
	case "ranks":
		err = r.perProject(ctx, "ranks", "reports", r.rankProject)
	case "reports":
		err = r.perProject(ctx, "reports", "done", r.sprintReports)
	}
	return r.cur.Phase == "done", err
}

// perProject runs fn for the current project, then moves to the next one or the next phase.
func (r *run) perProject(ctx context.Context, phase, next string, fn func(context.Context, *projectCtx) error) error {
	ph := r.prog.start(phase)
	ph.Total = len(r.cfg.Projects)
	if r.cur.Project < len(r.cfg.Projects) {
		pc, err := r.projectCtx(ctx, r.cur.Project)
		if err != nil {
			return err
		}
		if err := fn(ctx, pc); err != nil {
			return fmt.Errorf("%s: %w", pc.target.JiraKey, err)
		}
		r.cur.Project++
		ph.Done = r.cur.Project
	}
	if r.cur.Project >= len(r.cfg.Projects) {
		r.cur = cursor{Phase: next}
		if next != "done" {
			r.prog.start(next)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Projects

type typeInfo struct {
	id, workflowID, versionID uuid.UUID
	key, name                 string
	level                     int
	def                       *workflow.Definition
}

type sprintInfo struct {
	id    uuid.UUID
	name  string
	state string
}

// projectCtx is a target project with what issue writes need, loaded once per job execution.
type projectCtx struct {
	idx      int
	target   ProjectTarget
	project  dbq.Project
	info     Target
	types    map[string]*typeInfo // by key
	byID     map[uuid.UUID]*typeInfo
	sprints  map[int64]sprintInfo
	versions map[string]uuid.UUID
}

func (r *run) importProjects(ctx context.Context) error {
	ph := r.prog.start("projects")
	ph.Total = len(r.cfg.Projects)
	for i := range r.cfg.Projects {
		if _, err := r.ensureProject(ctx, i); err != nil {
			return fmt.Errorf("%s: %w", r.cfg.Projects[i].JiraKey, err)
		}
		ph.Done = i + 1
	}
	r.cur = cursor{Phase: "versions"}
	r.prog.start("versions")
	return nil
}

// ensureProject finds the project an earlier run created or creates it from its template.
func (r *run) ensureProject(ctx context.Context, idx int) (dbq.Project, error) {
	t := r.cfg.Projects[idx]
	q := r.s.Q
	if m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "project", JiraID: t.JiraID}); err == nil {
		if p, err := q.GetProject(ctx, m.LocalID); err == nil {
			return p, r.bumpCounter(ctx, p, t)
		}
	} else if !db.IsNoRows(err) {
		return dbq.Project{}, err
	}
	desc := ""
	if jp, err := r.client.Project(ctx, t.JiraKey); err == nil {
		desc = jp.Description
	}
	open := ""
	if t.Open {
		open = "member"
	}
	p, err := r.s.Tracker.CreateProject(ctx, r.actor, tracker.ProjectInput{Key: t.Key, Name: t.Name, Description: desc, Template: t.Template, OpenRoleKey: open})
	if err != nil {
		return p, err
	}
	if err := upsertMap(ctx, q, r.site, "project", t.JiraID, p.ID, &p.ID, &r.imp.ID, map[string]string{"key": t.JiraKey}); err != nil {
		return p, err
	}
	r.prog.count("projectsCreated", 1)
	// Keep the Jira key resolving when the project gets another key here.
	if t.JiraKey != p.Key && projectKeyPattern.MatchString(t.JiraKey) {
		if used, err := r.s.keyUsed(ctx, t.JiraKey); err == nil && !used {
			if err := q.AddProjectKeyAlias(ctx, dbq.AddProjectKeyAliasParams{Key: t.JiraKey, ProjectID: p.ID}); err != nil {
				return p, err
			}
		}
	}
	return p, r.bumpCounter(ctx, p, t)
}

// bumpCounter reserves every number Jira used, so issues created here meanwhile never take a
// number an imported issue needs.
func (r *run) bumpCounter(ctx context.Context, p dbq.Project, t ProjectTarget) error {
	if sp := r.scan.project(t.JiraID); sp != nil && sp.MaxNumber > 0 {
		return r.s.Q.ImportBumpProjectCounter(ctx, dbq.ImportBumpProjectCounterParams{ID: p.ID, Number: int32(sp.MaxNumber)})
	}
	return nil
}

func (r *run) projectCtx(ctx context.Context, idx int) (*projectCtx, error) {
	if pc, ok := r.projects[idx]; ok {
		return pc, nil
	}
	t := r.cfg.Projects[idx]
	q := r.s.Q
	m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "project", JiraID: t.JiraID})
	if err != nil {
		return nil, fmt.Errorf("project %s was not created: %w", t.JiraKey, err)
	}
	p, err := q.GetProject(ctx, m.LocalID)
	if err != nil {
		return nil, fmt.Errorf("project %s was deleted during the import", t.JiraKey)
	}
	pc := &projectCtx{idx: idx, target: t, project: p, types: map[string]*typeInfo{}, byID: map[uuid.UUID]*typeInfo{}, sprints: map[int64]sprintInfo{}, versions: map[string]uuid.UUID{}}
	if pc.info, err = r.s.projectTarget(ctx, p.ID); err != nil {
		return nil, err
	}
	types, err := q.ListIssueTypes(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, it := range types {
		def, wv, err := r.s.workflowDef(ctx, it.WorkflowID)
		if err != nil {
			return nil, err
		}
		ti := &typeInfo{id: it.ID, workflowID: it.WorkflowID, versionID: wv.ID, key: it.Key, name: it.Name, level: int(it.Level), def: def}
		pc.types[it.Key] = ti
		pc.byID[it.ID] = ti
	}
	for _, kind := range []string{"sprint", "version"} {
		after := ""
		for {
			rows, err := q.JiraMapPage(ctx, dbq.JiraMapPageParams{Site: r.site, Kind: kind, ProjectID: &p.ID, After: after, Lim: 1000})
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				after = row.JiraID
				if kind == "version" {
					pc.versions[row.JiraID] = row.LocalID
					continue
				}
				var d sprintData
				_ = json.Unmarshal(row.Data, &d)
				if sp, err := q.GetSprint(ctx, row.LocalID); err == nil {
					pc.sprints[d.JiraID] = sprintInfo{id: sp.ID, name: sp.Name, state: sp.State}
				}
			}
			if len(rows) < 1000 {
				break
			}
		}
	}
	r.projects[idx] = pc
	return pc, nil
}

// ---------------------------------------------------------------------------
// Versions → releases

func (r *run) importVersions(ctx context.Context, pc *projectCtx) error {
	list, err := r.client.Versions(ctx, pc.target.JiraKey)
	if err != nil {
		return err
	}
	q := r.s.Q
	for pos, v := range list {
		state, releasedAt := "unreleased", (*time.Time)(nil)
		start, _ := parseJiraTime(v.StartDate)
		release, _ := parseJiraTime(v.ReleaseDate)
		switch {
		case v.Archived:
			state = "archived"
		case v.Released:
			state = "released"
			if !release.IsZero() {
				releasedAt = &release
			} else {
				now := r.s.Now()
				releasedAt = &now
			}
		}
		name := strings.TrimSpace(v.Name)
		var rel dbq.Release
		if m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "version", JiraID: v.ID}); err == nil {
			if _, err := q.GetRelease(ctx, m.LocalID); err == nil {
				rel, err = q.ImportUpdateRelease(ctx, dbq.ImportUpdateReleaseParams{ID: m.LocalID, Name: name, Description: v.Description,
					StartDate: dateOnly(start), ReleaseDate: dateOnly(release), State: state, ReleasedAt: releasedAt, Position: int32(pos + 1)})
				if err != nil {
					if db.IsUniqueViolation(err) {
						r.prog.add("warning", "version", pc.target.JiraKey, fmt.Sprintf("Version %q was renamed in Jira to a name another release already has; kept the old name.", name), "")
						continue
					}
					return err
				}
			}
		} else if !db.IsNoRows(err) {
			return err
		}
		if rel.ID == uuid.Nil {
			// A release with the same name (created by hand) is adopted.
			if existing, err := q.ImportReleaseByName(ctx, dbq.ImportReleaseByNameParams{ProjectID: pc.project.ID, Name: name}); err == nil {
				rel, err = q.ImportUpdateRelease(ctx, dbq.ImportUpdateReleaseParams{ID: existing.ID, Name: existing.Name, Description: v.Description,
					StartDate: dateOnly(start), ReleaseDate: dateOnly(release), State: state, ReleasedAt: releasedAt, Position: int32(pos + 1)})
				if err != nil {
					return err
				}
			} else if db.IsNoRows(err) {
				if rel, err = q.ImportInsertRelease(ctx, dbq.ImportInsertReleaseParams{ProjectID: pc.project.ID, Name: name, Description: v.Description,
					StartDate: dateOnly(start), ReleaseDate: dateOnly(release), State: state, ReleasedAt: releasedAt, Position: int32(pos + 1)}); err != nil {
					return err
				}
				r.prog.count("releases", 1)
			} else {
				return err
			}
		}
		if err := upsertMap(ctx, q, r.site, "version", v.ID, rel.ID, &pc.project.ID, &r.imp.ID, nil); err != nil {
			return err
		}
		pc.versions[v.ID] = rel.ID
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sprints

type sprintData struct {
	JiraID   int64      `json:"jiraId"`
	Start    *time.Time `json:"start,omitempty"`
	Complete *time.Time `json:"complete,omitempty"`
}

var sprintStateRank = map[string]int{"closed": 0, "active": 1, "future": 2}

func (r *run) importSprints(ctx context.Context, pc *projectCtx) error {
	boards, err := r.client.Boards(ctx, pc.target.JiraKey)
	if err != nil {
		return err
	}
	list, _, err := r.projectSprints(ctx, boards)
	if err != nil {
		return err
	}
	slices.SortStableFunc(list, func(a, b Sprint) int {
		if c := sprintStateRank[strings.ToLower(a.State)] - sprintStateRank[strings.ToLower(b.State)]; c != 0 {
			return c
		}
		if c := a.Started().Compare(b.Started()); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	var keep []uuid.UUID
	for pos, sp := range list {
		info, err := r.upsertSprint(ctx, pc, sp, pos+1)
		if err != nil {
			return err
		}
		keep = append(keep, info.id)
	}
	if len(keep) > 0 {
		// The template's empty "Sprint 1" goes when Jira brings real sprints.
		return r.s.Q.ImportDeleteEmptyFutureSprints(ctx, dbq.ImportDeleteEmptyFutureSprintsParams{ProjectID: pc.project.ID, Keep: keep})
	}
	return nil
}

func sprintMapKey(projectJiraID string, id int64) string {
	return projectJiraID + ":" + strconv.FormatInt(id, 10)
}

func (r *run) upsertSprint(ctx context.Context, pc *projectCtx, sp Sprint, pos int) (sprintInfo, error) {
	q := r.s.Q
	state := strings.ToLower(sp.State)
	if _, ok := sprintStateRank[state]; !ok {
		state = "future"
	}
	key := sprintMapKey(pc.target.JiraID, sp.ID)
	var existing *dbq.Sprint
	if m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "sprint", JiraID: key}); err == nil {
		if s, err := q.GetSprint(ctx, m.LocalID); err == nil {
			existing = &s
			pos = int(s.Position)
		}
	} else if !db.IsNoRows(err) {
		return sprintInfo{}, err
	}
	if state == "active" {
		// One active sprint per project: Jira's parallel sprints beyond the first stay future.
		if act, err := q.ActiveSprint(ctx, pc.project.ID); err == nil && (existing == nil || act.ID != existing.ID) {
			state = "future"
			r.prog.add("warning", "sprint", pc.target.JiraKey, fmt.Sprintf("Sprint %q is active in Jira while another sprint is too; it was imported as a future sprint.", sp.Name), "")
		}
	}
	started := sp.Started()
	var completedAt *time.Time
	if state == "closed" {
		c := sp.CompleteDate.Time
		if c.IsZero() {
			c = sp.EndDate.Time
		}
		if !c.IsZero() {
			completedAt = &c
		}
	}
	name := cmpOr(strings.TrimSpace(sp.Name), fmt.Sprintf("Sprint %d", sp.ID))
	var out dbq.Sprint
	var err error
	if existing != nil {
		out, err = q.ImportUpdateSprint(ctx, dbq.ImportUpdateSprintParams{ID: existing.ID, Name: name, Goal: sp.Goal, State: state,
			StartDate: dateOnly(started), EndDate: dateOnly(sp.EndDate.Time), CompletedAt: completedAt, Position: int32(pos)})
	} else {
		out, err = q.ImportInsertSprint(ctx, dbq.ImportInsertSprintParams{ProjectID: pc.project.ID, Name: name, Goal: sp.Goal, State: state,
			StartDate: dateOnly(started), EndDate: dateOnly(sp.EndDate.Time), CompletedAt: completedAt, Position: int32(pos)})
		r.prog.count("sprints", 1)
	}
	if err != nil {
		return sprintInfo{}, err
	}
	d := sprintData{JiraID: sp.ID}
	if !started.IsZero() {
		d.Start = &started
	}
	d.Complete = completedAt
	if err := upsertMap(ctx, q, r.site, "sprint", key, out.ID, &pc.project.ID, &r.imp.ID, d); err != nil {
		return sprintInfo{}, err
	}
	info := sprintInfo{id: out.ID, name: out.Name, state: out.State}
	pc.sprints[sp.ID] = info
	return info, nil
}

// sprintFor resolves a Jira sprint to the project's sprint, creating sprints known only from
// issues (boards of other projects, deleted boards).
func (r *run) sprintFor(ctx context.Context, pc *projectCtx, sp Sprint) (*sprintInfo, error) {
	if info, ok := pc.sprints[sp.ID]; ok {
		return &info, nil
	}
	if sp.Name == "" {
		full, err := r.client.Sprint(ctx, sp.ID)
		if err != nil {
			if IsNotFound(err) || isStatus(err, 403, 400) {
				return nil, nil
			}
			return nil, err
		}
		sp = full
	}
	info, err := r.upsertSprint(ctx, pc, sp, len(pc.sprints)+1)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// currentSprint is the sprint an issue is in now: its open sprint, else the last closed one.
func currentSprint(list []Sprint) *Sprint {
	var best *Sprint
	for i := range list {
		sp := &list[i]
		st := strings.ToLower(sp.State)
		switch {
		case best == nil:
			best = sp
		case st == "active" && strings.ToLower(best.State) != "active":
			best = sp
		case st == "future" && strings.ToLower(best.State) == "closed":
			best = sp
		case st == strings.ToLower(best.State) && sp.ID > best.ID:
			best = sp
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Issues

type issueData struct {
	Key         string     `json:"key"`
	Updated     string     `json:"updated"`
	Fingerprint string     `json:"fp"`
	Rank        string     `json:"rank,omitempty"`
	Level       int        `json:"level"`
	Parent      *IssueRef  `json:"parent,omitempty"`
	EpicKey     string     `json:"epicKey,omitempty"`
	Links       []linkData `json:"links,omitempty"`
}

type linkData struct {
	Type    string   `json:"type"`
	Outward bool     `json:"outward"`
	Other   IssueRef `json:"other"`
}

var errSkipIssue = errors.New("skip issue")

func (r *run) importIssuesStep(ctx context.Context) error {
	ph := r.prog.start("issues")
	if ph.Total == 0 {
		for _, p := range r.cfg.Projects {
			if sp := r.scan.project(p.JiraID); sp != nil {
				ph.Total += sp.Issues
			}
		}
	}
	if r.cur.Project >= len(r.cfg.Projects) {
		r.cur = cursor{Phase: "relations"}
		return nil
	}
	pc, err := r.projectCtx(ctx, r.cur.Project)
	if err != nil {
		return err
	}
	fields := []string{"summary", "description", "issuetype", "status", "priority", "assignee", "reporter", "creator", "labels",
		"components", "duedate", "created", "updated", "resolutiondate", "resolution", "parent", "fixVersions", "issuelinks",
		"attachment", "comment"}
	fields = append(fields, r.meta.customFields()...)
	page, err := r.client.Search(ctx, jqlProject(pc.target.JiraKey, r.cur.After), fields, "changelog", "", issuePageSize)
	if err != nil {
		return err
	}
	for _, is := range page.Issues {
		if err := r.importIssue(ctx, pc, is); err != nil {
			return fmt.Errorf("%s: %w", is.Key, err)
		}
		r.cur.After = max(r.cur.After, is.Number())
		ph.Done++
		r.heartbeat(ctx)
	}
	if page.Last || len(page.Issues) == 0 {
		r.cur.Project++
		r.cur.After = 0
		if r.cur.Project >= len(r.cfg.Projects) {
			r.cur = cursor{Phase: "relations"}
			r.prog.start("relations")
		}
	}
	return nil
}

// resolved holds an issue's values mapped onto this side.
type resolved struct {
	typ                *typeInfo
	status             workflow.Status
	assignee, reporter *uuid.UUID
	sprint             *uuid.UUID
	points             *float64
	start, due         *time.Time
	labels             []string
	resolution         *string
	resolvedAt         *time.Time
	created, updated   time.Time
	statusChangedAt    time.Time
}

type pendingAttachment struct {
	jiraID string
	params dbq.ImportInsertAttachmentParams
}

func (r *run) importIssue(ctx context.Context, pc *projectCtx, is Issue) error {
	q := r.s.Q
	f, err := is.Decode()
	if err != nil {
		return err
	}
	number := is.Number()
	key := fmt.Sprintf("%s-%d", pc.project.Key, number)
	data := issueData{Key: is.Key, Updated: f.Updated.UTC().Format(time.RFC3339Nano), Fingerprint: r.finger}
	if r.meta.rankField != "" {
		data.Rank = is.String(r.meta.rankField)
	}
	var existing *dbq.Issue
	if m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "issue", JiraID: is.ID}); err == nil {
		if i, err := q.GetIssue(ctx, m.LocalID); err == nil {
			existing = &i
			var prev issueData
			_ = json.Unmarshal(m.Data, &prev)
			if prev.Updated == data.Updated && prev.Fingerprint == data.Fingerprint {
				// Unchanged since the last run: keep only a moved rank.
				r.prog.count("issuesUnchanged", 1)
				if prev.Rank != data.Rank {
					prev.Rank = data.Rank
					return upsertMap(ctx, q, r.site, "issue", is.ID, i.ID, &pc.project.ID, &r.imp.ID, prev)
				}
				return nil
			}
			if i.ProjectID != pc.project.ID {
				// Its types and statuses belong to the other project: leave it as it is.
				r.prog.add("warning", "moved", pc.target.JiraKey, "Issues moved between Jira projects since an earlier import were left where they are.", is.Key)
				r.prog.count("issuesSkipped", 1)
				return nil
			}
		}
	} else if !db.IsNoRows(err) {
		return err
	}

	v, err := r.resolve(ctx, pc, is, f, key)
	if err != nil {
		return err
	}
	data.Level = v.typ.level
	data.Parent = f.Parent
	if r.meta.epicLinkField != "" {
		data.EpicKey = is.String(r.meta.epicLinkField)
	}
	for _, l := range f.IssueLinks {
		ld := linkData{Type: l.Type.Name}
		switch {
		case l.OutwardIssue != nil:
			ld.Outward, ld.Other = true, *l.OutwardIssue
		case l.InwardIssue != nil:
			ld.Other = *l.InwardIssue
		default:
			continue
		}
		data.Links = append(data.Links, ld)
	}

	// Full changelog and comments when the search result has only the first page.
	histories := []History{}
	if is.Changelog != nil {
		histories = is.Changelog.Histories
		if is.Changelog.Total > len(histories) || is.Changelog.StartAt > 0 {
			if histories, err = r.client.Changelog(ctx, is.ID); err != nil {
				return err
			}
		}
	} else if histories, err = r.client.Changelog(ctx, is.ID); err != nil {
		return err
	}
	var comments []Comment
	if f.Comment != nil {
		comments = f.Comment.Comments
		if f.Comment.Total > len(comments) {
			if comments, err = r.client.Comments(ctx, is.ID); err != nil {
				return err
			}
		}
	}

	issueID := uuid.New()
	if existing != nil {
		issueID = existing.ID
	}
	// Attachments are stored before the transaction; a failed transaction removes them again.
	byName, pending, err := r.attachments(ctx, pc, issueID, existing != nil, is, f.Attachment)
	if err != nil {
		return err
	}
	cleanup := func() {
		for _, p := range pending {
			_ = r.s.Tracker.Storage.Delete(context.WithoutCancel(ctx), p.params.ObjectKey)
		}
	}
	opts := TextOptions{Attachment: func(name string) (string, bool) {
		if id, ok := byName[name]; ok {
			return "/api/v1/attachments/" + id.String(), true
		}
		return "", false
	}}
	description := RichText(f.Description, opts)
	rows, err := r.historyRows(ctx, pc, v, is, histories)
	if err != nil {
		cleanup()
		return err
	}
	for _, h := range rows {
		if h.field == "status" {
			v.statusChangedAt = h.at
		}
	}
	if v.status.Category == "done" && v.resolvedAt == nil {
		at := v.updated
		for _, h := range rows {
			if h.field == "status" && h.toCategory == "done" {
				at = h.at
			}
		}
		v.resolvedAt = &at
	}
	type commentRow struct {
		author   *uuid.UUID
		body     string
		created  time.Time
		editedAt *time.Time
	}
	var commentRows []commentRow
	for _, c := range comments {
		author, err := r.person(ctx, c.Author)
		if err != nil {
			cleanup()
			return err
		}
		body := RichText(c.Body, opts)
		if strings.TrimSpace(body) == "" {
			continue
		}
		cr := commentRow{author: author, body: body, created: cmpTime(c.Created.Time, v.created)}
		if !c.Updated.IsZero() && c.Updated.Sub(c.Created.Time) > time.Second {
			u := c.Updated.Time
			cr.editedAt = &u
		}
		commentRows = append(commentRows, cr)
	}
	summary := strings.TrimSpace(f.Summary)
	if len(summary) > 255 {
		summary = summary[:252] + "..."
	}
	if summary == "" {
		summary = is.Key
	}

	err = db.Tx(ctx, r.s.Pool, func(_ pgxTx, q *dbq.Queries) error {
		var issue dbq.Issue
		var err error
		if existing == nil {
			if other, err := q.GetIssueByNumber(ctx, dbq.GetIssueByNumberParams{ProjectID: pc.project.ID, Number: int32(number)}); err == nil {
				_ = other
				r.prog.add("error", "number", pc.target.JiraKey, fmt.Sprintf("%s already exists here (created outside the import); the Jira issue was skipped.", key), is.Key)
				return errSkipIssue
			} else if !db.IsNoRows(err) {
				return err
			}
			if err := q.ImportBumpProjectCounter(ctx, dbq.ImportBumpProjectCounterParams{ID: pc.project.ID, Number: int32(number)}); err != nil {
				return err
			}
			maxRank, err := q.ImportMaxRank(ctx, pc.project.ID)
			if err != nil {
				return err
			}
			rk, err := rank.After(maxRank)
			if err != nil {
				return err
			}
			issue, err = q.ImportInsertIssue(ctx, dbq.ImportInsertIssueParams{
				ID: issueID, ProjectID: pc.project.ID, Number: int32(number), TypeID: v.typ.id, Summary: summary, Description: description,
				StatusID: v.status.ID, WorkflowVersionID: v.typ.versionID, Priority: r.priority(f.Priority), AssigneeID: v.assignee,
				ReporterID: v.reporter, SprintID: v.sprint, StoryPoints: v.points, StartDate: v.start, DueDate: v.due, Labels: v.labels,
				Resolution: v.resolution, ResolvedAt: v.resolvedAt, Rank: rk, CreatedAt: v.created, UpdatedAt: v.updated,
				StatusChangedAt: v.statusChangedAt,
			})
			if err != nil {
				return err
			}
			for _, w := range []*uuid.UUID{v.reporter, v.assignee} {
				if w != nil && r.active[*w] {
					if err := q.AddWatcher(ctx, dbq.AddWatcherParams{IssueID: issue.ID, UserID: *w}); err != nil {
						return err
					}
				}
			}
			r.prog.count("issuesCreated", 1)
		} else {
			issue, err = q.ImportUpdateIssue(ctx, dbq.ImportUpdateIssueParams{
				ID: existing.ID, TypeID: v.typ.id, Summary: summary, Description: description, StatusID: v.status.ID,
				WorkflowVersionID: v.typ.versionID, Priority: r.priority(f.Priority), AssigneeID: v.assignee, ReporterID: v.reporter,
				SprintID: v.sprint, StoryPoints: v.points, StartDate: v.start, DueDate: v.due, Labels: v.labels,
				Resolution: v.resolution, ResolvedAt: v.resolvedAt, CreatedAt: v.created, UpdatedAt: v.updated,
			})
			if err != nil {
				return err
			}
			if err := q.ImportSetStatusChangedAt(ctx, dbq.ImportSetStatusChangedAtParams{ID: issue.ID, StatusChangedAt: v.statusChangedAt}); err != nil {
				return err
			}
			if err := q.ClearIssueReleases(ctx, issue.ID); err != nil {
				return err
			}
			r.prog.count("issuesUpdated", 1)
		}
		for _, fv := range f.FixVersions {
			if rid, ok := pc.versions[fv.ID]; ok {
				if err := q.AddIssueRelease(ctx, dbq.AddIssueReleaseParams{IssueID: issue.ID, ReleaseID: rid}); err != nil {
					return err
				}
			}
		}
		if err := q.ImportDeleteHistory(ctx, issue.ID); err != nil {
			return err
		}
		if err := q.ImportInsertHistory(ctx, dbq.ImportInsertHistoryParams{IssueID: issue.ID, ActorID: v.reporter, Field: "created", ToValue: &summary, CreatedAt: v.created}); err != nil {
			return err
		}
		for _, h := range rows {
			if err := q.ImportInsertHistory(ctx, dbq.ImportInsertHistoryParams{IssueID: issue.ID, ActorID: h.actor, Field: h.field,
				FromValue: h.from, ToValue: h.to, FromRef: h.fromRef, ToRef: h.toRef, CreatedAt: h.at}); err != nil {
				return err
			}
		}
		r.prog.count("historyEntries", len(rows))
		if err := q.ImportDeleteComments(ctx, issue.ID); err != nil {
			return err
		}
		for _, c := range commentRows {
			if err := q.ImportInsertComment(ctx, dbq.ImportInsertCommentParams{IssueID: issue.ID, AuthorID: c.author, Body: c.body, EditedAt: c.editedAt, CreatedAt: c.created}); err != nil {
				return err
			}
		}
		r.prog.count("comments", len(commentRows))
		for _, p := range pending {
			if _, err := q.ImportInsertAttachment(ctx, p.params); err != nil {
				return err
			}
			if err := upsertMap(ctx, q, r.site, "attachment", p.jiraID, p.params.ID, &pc.project.ID, &r.imp.ID, nil); err != nil {
				return err
			}
			r.prog.count("attachments", 1)
			r.prog.count("attachmentBytes", int(p.params.SizeBytes))
		}
		return upsertMap(ctx, q, r.site, "issue", is.ID, issue.ID, &pc.project.ID, &r.imp.ID, data)
	})
	if err != nil {
		cleanup()
		if errors.Is(err, errSkipIssue) {
			r.prog.count("issuesSkipped", 1)
			return nil
		}
		return err
	}
	return nil
}

func cmpTime(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}

// resolve maps an issue's values: type, status (in the type's workflow), people, sprint,
// points, labels, dates and resolution.
func (r *run) resolve(ctx context.Context, pc *projectCtx, is Issue, f Fields, key string) (resolved, error) {
	var v resolved
	v.typ = r.typeFor(pc, f.IssueType)
	if v.typ == nil {
		return v, fmt.Errorf("project %s has no issue types", pc.project.Key)
	}
	var fellBack bool
	v.status, fellBack = r.statusIn(pc, v.typ, f.Status.ID, f.Status.Name, f.Status.Category())
	if fellBack {
		r.prog.add("info", "status", pc.target.JiraKey, fmt.Sprintf("%s issues in Jira's %q were imported as %q: the %s workflow has no %q.",
			v.typ.name, f.Status.Name, v.status.Name, v.typ.def.Name, cmpOr(pc.target.Statuses[f.Status.ID], f.Status.Name)), is.Key)
	}
	var err error
	if v.assignee, err = r.person(ctx, f.Assignee); err != nil {
		return v, err
	}
	reporter := f.Reporter
	if reporter == nil {
		reporter = f.Creator
	}
	if v.reporter, err = r.person(ctx, reporter); err != nil {
		return v, err
	}
	if v.typ.level <= 0 && r.meta.sprintField != "" {
		if sp := currentSprint(is.Sprints(r.meta.sprintField)); sp != nil {
			info, err := r.sprintFor(ctx, pc, *sp)
			if err != nil {
				return v, err
			}
			if info != nil {
				v.sprint = &info.id
			}
		}
	}
	if r.meta.pointsField != "" {
		if p := is.Float(r.meta.pointsField); p != nil && *p >= 0 && *p <= 1000 {
			v.points = p
		}
	}
	if r.meta.startDateField != "" {
		if t, err := parseJiraTime(is.String(r.meta.startDateField)); err == nil {
			v.start = dateOnly(t)
		}
	}
	if t, err := parseJiraTime(f.DueDate); err == nil {
		v.due = dateOnly(t)
	}
	labels := slices.Clone(f.Labels)
	if r.cfg.ComponentsAsLabels {
		for _, c := range f.Components {
			labels = append(labels, "component:"+c.Name)
		}
	}
	var dropped bool
	v.labels, dropped = normalizeLabels(labels)
	if dropped {
		r.prog.add("info", "labels", pc.target.JiraKey, "Issues with more than 30 labels kept the first 30.", is.Key)
	}
	v.created = cmpTime(f.Created.Time, r.s.Now())
	v.updated = cmpTime(f.Updated.Time, v.created)
	v.statusChangedAt = v.created
	if v.status.Category == "done" {
		name := "Done"
		if f.Resolution != nil && f.Resolution.Name != "" {
			name = f.Resolution.Name
		}
		v.resolution = &name
		if !f.ResolutionDate.IsZero() {
			t := f.ResolutionDate.Time
			v.resolvedAt = &t
		}
	}
	_ = key
	return v, nil
}

// typeFor maps a Jira issue type: the mapping, else by hierarchy level and name.
func (r *run) typeFor(pc *projectCtx, it IssueType) *typeInfo {
	if k := pc.target.Types[it.ID]; k != "" {
		if ti, ok := pc.types[k]; ok {
			return ti
		}
	}
	if ti, ok := pc.types[defaultType(pc.info, it.Name, it.Level())]; ok {
		return ti
	}
	for _, ti := range pc.types {
		return ti
	}
	return nil
}

// statusIn finds the status for a Jira status in a type's workflow: the mapped status, else the
// Jira name, else the first status of the mapped category. fellBack reports the last two.
func (r *run) statusIn(pc *projectCtx, ti *typeInfo, jiraID, jiraName, jiraCategory string) (workflow.Status, bool) {
	if st, ok := r.meta.statuses[jiraID]; ok {
		jiraName, jiraCategory = cmpOr(st.Name, jiraName), st.Category()
	}
	mapped := pc.target.Statuses[jiraID]
	if mapped == "" {
		mapped = defaultStatus(pc.info, jiraName, jiraCategory)
	}
	if st, ok := ti.def.StatusByName(mapped); ok {
		return st, false
	}
	if st, ok := ti.def.StatusByName(jiraName); ok {
		return st, true
	}
	category := jiraCategory
	for _, ts := range pc.info.Statuses {
		if strings.EqualFold(ts.Name, mapped) {
			category = ts.Category
		}
	}
	for _, st := range ti.def.Statuses {
		if st.Category == category {
			return st, true
		}
	}
	return ti.def.Initial(), true
}

// priority maps a Jira priority (by id, else by name).
func (r *run) priority(p *Priority) string {
	if p == nil {
		return "medium"
	}
	if v := r.cfg.Priorities[p.ID]; slices.Contains(priorityNames, v) {
		return v
	}
	return defaultPriority(p.Name)
}

func (r *run) priorityByName(name string) string {
	for id, p := range r.meta.priorities {
		if strings.EqualFold(p.Name, name) {
			return r.priority(&Priority{ID: id, Name: p.Name})
		}
	}
	return defaultPriority(name)
}

// normalizeLabels applies the label rules here: no spaces, at most 50 characters, at most 30.
func normalizeLabels(in []string) ([]string, bool) {
	out := []string{}
	for _, l := range in {
		l = strings.Join(strings.Fields(strings.TrimSpace(l)), "-")
		if l == "" {
			continue
		}
		if len(l) > 50 {
			l = l[:50]
		}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	if len(out) > 30 {
		return out[:30], true
	}
	return out, false
}

// attachments downloads the attachments not imported yet and returns every attachment of the
// issue by file name (for links in the description and comments).
func (r *run) attachments(ctx context.Context, pc *projectCtx, issueID uuid.UUID, exists bool, is Issue, list []Attachment) (map[string]uuid.UUID, []pendingAttachment, error) {
	byName := map[string]uuid.UUID{}
	if exists {
		have, err := r.s.Q.ImportIssueAttachments(ctx, issueID)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range have {
			byName[a.Filename] = a.ID
		}
	}
	if !r.cfg.Attachments {
		return byName, nil, nil
	}
	maxBytes := r.s.Cfg.AttachmentMaxBytes
	var pending []pendingAttachment
	for _, a := range list {
		if m, err := r.s.Q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "attachment", JiraID: a.ID}); err == nil {
			if att, err := r.s.Q.GetAttachment(ctx, m.LocalID); err == nil && att.IssueID == issueID {
				byName[a.Filename] = att.ID
				continue
			}
		} else if !db.IsNoRows(err) {
			return nil, nil, err
		}
		if a.Size > maxBytes {
			r.prog.add("warning", "attachment", pc.target.JiraKey, fmt.Sprintf("Attachments larger than %s were skipped.", humanBytes(maxBytes)), is.Key+" "+a.Filename)
			r.prog.count("attachmentsSkipped", 1)
			continue
		}
		uploader, err := r.person(ctx, a.Author)
		if err != nil {
			return nil, nil, err
		}
		name := path.Base(strings.ReplaceAll(a.Filename, "\\", "/"))
		if name == "." || name == "/" || name == "" {
			name = "attachment"
		}
		ct := cmpOr(a.MimeType, "application/octet-stream")
		objectKey := fmt.Sprintf("attachments/%s/%s/%s", pc.project.ID, issueID, uuid.NewString())
		size, err := r.download(ctx, a, objectKey, ct, maxBytes)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, err
			}
			r.prog.add("warning", "attachment", pc.target.JiraKey, "Some attachments could not be downloaded: "+err.Error(), is.Key+" "+a.Filename)
			r.prog.count("attachmentsSkipped", 1)
			continue
		}
		id := uuid.New()
		byName[a.Filename] = id
		pending = append(pending, pendingAttachment{jiraID: a.ID, params: dbq.ImportInsertAttachmentParams{
			ID: id, IssueID: issueID, UploaderID: uploader, Filename: name, ContentType: ct, SizeBytes: size, ObjectKey: objectKey,
			CreatedAt: cmpTime(a.Created.Time, r.s.Now()),
		}})
	}
	return byName, pending, nil
}

// download streams one attachment into storage, enforcing the size limit on the bytes read.
func (r *run) download(ctx context.Context, a Attachment, key, contentType string, maxBytes int64) (int64, error) {
	body, err := r.client.OpenAttachment(ctx, a)
	if err != nil {
		return 0, err
	}
	defer body.Close()
	counter := &countingReader{r: io.LimitReader(body, maxBytes+1)}
	if err := r.s.Tracker.Storage.Put(ctx, key, counter, a.Size, contentType); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	if counter.n > maxBytes {
		_ = r.s.Tracker.Storage.Delete(ctx, key)
		return 0, fmt.Errorf("larger than %s", humanBytes(maxBytes))
	}
	return counter.n, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ---------------------------------------------------------------------------
// Relations: parents and links, once every issue exists

func (r *run) relationsStep(ctx context.Context) error {
	ph := r.prog.start("relations")
	if r.cur.Project >= len(r.cfg.Projects) {
		r.cur = cursor{Phase: "ranks"}
		r.prog.start("ranks")
		return nil
	}
	if r.cur.Project == 0 && r.cur.AfterID == "" {
		ph.Total = r.prog.Counters["issuesCreated"] + r.prog.Counters["issuesUpdated"] + r.prog.Counters["issuesUnchanged"]
		ph.Done = 0
	}
	pc, err := r.projectCtx(ctx, r.cur.Project)
	if err != nil {
		return err
	}
	q := r.s.Q
	rows, err := q.JiraMapPage(ctx, dbq.JiraMapPageParams{Site: r.site, Kind: "issue", ProjectID: &pc.project.ID, After: r.cur.AfterID, Lim: relationPageSize})
	if err != nil {
		return err
	}
	for _, row := range rows {
		var d issueData
		_ = json.Unmarshal(row.Data, &d)
		if err := r.relate(ctx, pc, row.LocalID, d); err != nil {
			return fmt.Errorf("%s: %w", d.Key, err)
		}
		r.cur.AfterID = row.JiraID
		ph.Done++
	}
	if len(rows) < relationPageSize {
		r.cur.Project++
		r.cur.AfterID = ""
	}
	return nil
}

// localIssue finds the issue here for a Jira issue reference: by id through the mapping, else
// by key (keys keep their numbers).
func (r *run) localIssue(ctx context.Context, ref IssueRef) (*dbq.Issue, error) {
	q := r.s.Q
	if ref.ID != "" {
		if m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "issue", JiraID: ref.ID}); err == nil {
			if i, err := q.GetIssue(ctx, m.LocalID); err == nil {
				return &i, nil
			}
		} else if !db.IsNoRows(err) {
			return nil, err
		}
	}
	if ref.Key == "" {
		return nil, nil
	}
	k := strings.LastIndexByte(ref.Key, '-')
	n, err := strconv.ParseInt(ref.Key[k+1:], 10, 32) // issue numbers are int32
	if k <= 0 || err != nil {
		return nil, nil
	}
	for i, t := range r.cfg.Projects {
		if t.JiraKey != ref.Key[:k] {
			continue
		}
		pc, err := r.projectCtx(ctx, i)
		if err != nil {
			return nil, err
		}
		if is, err := q.GetIssueByNumber(ctx, dbq.GetIssueByNumberParams{ProjectID: pc.project.ID, Number: int32(n)}); err == nil {
			return &is, nil
		}
	}
	return nil, nil
}

func (r *run) relate(ctx context.Context, pc *projectCtx, id uuid.UUID, d issueData) error {
	q := r.s.Q
	child, err := q.GetIssue(ctx, id)
	if err != nil {
		return nil // deleted here since
	}
	ct := pc.byID[child.TypeID]
	parentRef := d.Parent
	if parentRef == nil && d.EpicKey != "" {
		parentRef = &IssueRef{Key: d.EpicKey}
	}
	if parentRef != nil && ct != nil {
		parent, err := r.localIssue(ctx, *parentRef)
		if err != nil {
			return err
		}
		switch {
		case parent == nil:
			r.prog.add("warning", "parent", pc.target.JiraKey, "Parents outside the imported projects were not linked.", d.Key+" → "+parentRef.Key)
		case parent.ProjectID != child.ProjectID:
			r.prog.add("warning", "parent", pc.target.JiraKey, "Parents in another project were not linked (parents must be in the same project here).", d.Key+" → "+parentRef.Key)
		default:
			pt := pc.byID[parent.TypeID]
			if pt == nil || pt.level != ct.level+1 {
				r.prog.add("warning", "parent", pc.target.JiraKey, "Parent links that do not fit the epic → story → sub-task hierarchy (after type mapping) were dropped.", d.Key)
			} else if child.ParentID == nil || *child.ParentID != parent.ID || ct.level < 0 {
				if err := q.ImportSetParent(ctx, dbq.ImportSetParentParams{ID: child.ID, ParentID: &parent.ID, Subtask: ct.level < 0}); err != nil {
					return err
				}
				r.prog.count("parents", 1)
			}
		}
	} else if ct != nil && ct.level < 0 && child.ParentID == nil {
		r.prog.add("warning", "parent", pc.target.JiraKey, "Sub-tasks without a parent issue were imported without one.", d.Key)
	}
	for _, l := range d.Links {
		other, err := r.localIssue(ctx, l.Other)
		if err != nil {
			return err
		}
		if other == nil {
			r.prog.add("info", "link", pc.target.JiraKey, "Links to issues outside the imported projects were skipped.", d.Key+" → "+l.Other.Key)
			continue
		}
		if other.ID == child.ID {
			continue
		}
		lt, known := linkType(l.Type)
		if !known {
			r.prog.add("info", "link", pc.target.JiraKey, fmt.Sprintf("Links of type %q were imported as \"relates to\".", l.Type), "")
		}
		src, dst := child.ID, other.ID
		if !l.Outward {
			src, dst = dst, src
		}
		if lt == "relates" && src.String() > dst.String() {
			// "relates" is symmetric: store it once.
			src, dst = dst, src
		}
		if err := q.ImportLinkIssues(ctx, dbq.ImportLinkIssuesParams{SourceID: src, TargetID: dst, LinkType: lt, CreatedAt: child.CreatedAt}); err != nil {
			return err
		}
		r.prog.count("links", 1)
	}
	return nil
}

func linkType(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "blocks", "blocker", "block":
		return "blocks", true
	case "duplicate", "duplicates":
		return "duplicates", true
	case "cloners", "clones", "cloner":
		return "clones", true
	case "relates", "relate", "relates to":
		return "relates", true
	}
	return "relates", false
}

// ---------------------------------------------------------------------------
// Ranks

// rankProject orders the imported issues as Jira ranks them (LexoRank strings sort bytewise).
// Without a rank field (no Jira Software) issues keep the order they were imported in.
func (r *run) rankProject(ctx context.Context, pc *projectCtx) error {
	if r.meta.rankField == "" {
		return nil
	}
	q := r.s.Q
	ids, err := q.JiraMapIssuesByRank(ctx, dbq.JiraMapIssuesByRankParams{Site: r.site, ProjectID: &pc.project.ID})
	if err != nil || len(ids) == 0 {
		return err
	}
	// Already in order: leave ranks alone so ordering done here survives unchanged re-runs.
	current, err := q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	slices.SortFunc(current, func(a, b dbq.Issue) int { return strings.Compare(a.Rank, b.Rank) })
	same := len(current) == len(ids)
	for k := range current {
		if !same || current[k].ID != ids[k] {
			same = false
			break
		}
	}
	if same {
		return nil
	}
	return db.Tx(ctx, r.s.Pool, func(_ pgxTx, q *dbq.Queries) error {
		cur, err := q.ImportMaxRank(ctx, pc.project.ID)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if cur, err = rank.After(cur); err != nil {
				return err
			}
			if err := q.ImportSetRank(ctx, dbq.ImportSetRankParams{ID: id, Rank: cur}); err != nil {
				return err
			}
		}
		return nil
	})
}
