package jiraimport

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

// histRow is one issue_history row derived from a changelog item.
type histRow struct {
	actor              *uuid.UUID
	field              string
	from, to           *string
	fromRef, toRef     *string
	at                 time.Time
	toCategory         string
	historyID, itemIdx int
}

// historyRows converts an issue's changelog into history here. Status changes carry the mapped
// status ids, sprint changes the sprint ids and assignee changes the user ids, so cycle time,
// cumulative flow, time in status and sprint burndowns replay the Jira past.
func (r *run) historyRows(ctx context.Context, pc *projectCtx, v resolved, is Issue, histories []History) ([]histRow, error) {
	sorted := slices.Clone(histories)
	slices.SortStableFunc(sorted, func(a, b History) int {
		if c := a.Created.Compare(b.Created.Time); c != 0 {
			return c
		}
		ai, _ := strconv.ParseInt(a.ID, 10, 64)
		bi, _ := strconv.ParseInt(b.ID, 10, 64)
		return cmp.Compare(ai, bi)
	})
	var out []histRow
	for _, h := range sorted {
		at := cmpTime(h.Created.Time, v.created)
		actor, err := r.person(ctx, h.Author)
		if err != nil {
			return nil, err
		}
		for _, it := range h.Items {
			row, ok, err := r.historyItem(ctx, pc, v, it)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			row.actor, row.at = actor, at
			out = append(out, row)
		}
	}
	return out, nil
}

func refStr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *run) historyItem(ctx context.Context, pc *projectCtx, v resolved, it HistoryItem) (histRow, bool, error) {
	field := strings.ToLower(it.Field)
	fieldID := it.FieldID
	switch {
	case field == "status" || fieldID == "status":
		from, _ := r.statusIn(pc, v.typ, deref(it.From), deref(it.FromString), "")
		to, _ := r.statusIn(pc, v.typ, deref(it.To), deref(it.ToString), "")
		if from.ID == to.ID {
			return histRow{}, false, nil
		}
		return histRow{field: "status", from: &from.Name, to: &to.Name, fromRef: refStr(&from.ID), toRef: refStr(&to.ID), toCategory: to.Category}, true, nil
	case field == "assignee" || field == "reporter":
		from, err := r.personByID(ctx, deref(it.From), deref(it.FromString))
		if err != nil {
			return histRow{}, false, err
		}
		to, err := r.personByID(ctx, deref(it.To), deref(it.ToString))
		if err != nil {
			return histRow{}, false, err
		}
		if from == to || (from != nil && to != nil && *from == *to) {
			return histRow{}, false, nil
		}
		return histRow{field: field, from: r.userName(ctx, from), to: r.userName(ctx, to), fromRef: refStr(from), toRef: refStr(to)}, true, nil
	case (r.meta.sprintField != "" && fieldID == r.meta.sprintField) || field == "sprint":
		if v.typ.level > 0 {
			return histRow{}, false, nil
		}
		from, err := r.effectiveSprint(ctx, pc, deref(it.From))
		if err != nil {
			return histRow{}, false, err
		}
		to, err := r.effectiveSprint(ctx, pc, deref(it.To))
		if err != nil {
			return histRow{}, false, err
		}
		if (from == nil && to == nil) || (from != nil && to != nil && from.id == to.id) {
			return histRow{}, false, nil
		}
		row := histRow{field: "sprint"}
		if from != nil {
			row.from, row.fromRef = &from.name, refStr(&from.id)
		}
		if to != nil {
			row.to, row.toRef = &to.name, refStr(&to.id)
		}
		return row, true, nil
	case r.meta.pointsField != "" && (fieldID == r.meta.pointsField || (fieldID == "" && strings.EqualFold(it.Field, r.meta.pointsName))):
		from, to := points(cmpOr(deref(it.FromString), deref(it.From))), points(cmpOr(deref(it.ToString), deref(it.To)))
		if deref(from) == deref(to) {
			return histRow{}, false, nil
		}
		return histRow{field: "storyPoints", from: from, to: to}, true, nil
	case field == "priority":
		from, to := strPtr(deref(it.FromString)), strPtr(deref(it.ToString))
		if from != nil {
			from = strPtr(r.priorityByName(*from))
		}
		if to != nil {
			to = strPtr(r.priorityByName(*to))
		}
		if deref(from) == deref(to) {
			return histRow{}, false, nil
		}
		return histRow{field: "priority", from: from, to: to}, true, nil
	case field == "issuetype":
		return histRow{field: "type", from: strPtr(r.typeName(pc, deref(it.From), deref(it.FromString))), to: strPtr(r.typeName(pc, deref(it.To), deref(it.ToString)))}, true, nil
	case field == "summary" || field == "resolution" || field == "labels":
		return histRow{field: field, from: strPtr(deref(it.FromString)), to: strPtr(deref(it.ToString))}, true, nil
	case field == "fix version" || field == "fixversions":
		return histRow{field: "fixVersions", from: strPtr(deref(it.FromString)), to: strPtr(deref(it.ToString))}, true, nil
	case field == "duedate":
		return histRow{field: "dueDate", from: strPtr(dateValue(cmpOr(deref(it.From), deref(it.FromString)))), to: strPtr(dateValue(cmpOr(deref(it.To), deref(it.ToString))))}, true, nil
	case field == "description":
		return histRow{field: "description"}, true, nil
	case field == "attachment":
		return histRow{field: "attachment", from: strPtr(deref(it.FromString)), to: strPtr(deref(it.ToString))}, true, nil
	case field == "link":
		return histRow{field: "link", from: strPtr(deref(it.FromString)), to: strPtr(deref(it.ToString))}, true, nil
	case field == "parent" || field == "epic link" || field == "parent link" || field == "issueparentassociation":
		return histRow{field: "parent", from: strPtr(deref(it.FromString)), to: strPtr(deref(it.ToString))}, true, nil
	}
	return histRow{}, false, nil
}

func points(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return strPtr(strconv.FormatFloat(f, 'f', -1, 64))
}

func dateValue(s string) string {
	if len(s) >= 10 {
		if _, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return s[:10]
		}
	}
	return s
}

// typeName shows a Jira issue type by the name of the type it maps to here.
func (r *run) typeName(pc *projectCtx, id, name string) string {
	if k := pc.target.Types[id]; k != "" {
		if ti, ok := pc.types[k]; ok {
			return ti.name
		}
	}
	if sp := r.scan.project(pc.target.JiraID); sp != nil {
		for _, t := range sp.Types {
			if strings.EqualFold(t.Name, name) {
				if ti, ok := pc.types[pc.target.Types[t.ID]]; ok {
					return ti.name
				}
			}
		}
	}
	return name
}

// effectiveSprint turns a changelog sprint value ("12, 15": every sprint the issue was in) into
// the one sprint it was in here: the most recent (highest id), as Jira keeps closed sprints in
// the list when work carries over.
func (r *run) effectiveSprint(ctx context.Context, pc *projectCtx, value string) (*sprintInfo, error) {
	var best int64
	for _, part := range strings.Split(value, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > best {
			best = id
		}
	}
	if best == 0 {
		return nil, nil
	}
	return r.sprintFor(ctx, pc, Sprint{ID: best})
}

// ---------------------------------------------------------------------------
// Sprint reports

// sprintReports fills the commitment and completion snapshots of imported sprints that started,
// replaying the imported history: committed = in the sprint when it started (with the points
// then), completed = in it and done when it was completed.
func (r *run) sprintReports(ctx context.Context, pc *projectCtx) error {
	q := r.s.Q
	statuses, err := q.ListStatuses(ctx)
	if err != nil {
		return err
	}
	category := map[uuid.UUID]string{}
	for _, st := range statuses {
		category[st.ID] = st.Category
	}
	after := ""
	for {
		rows, err := q.JiraMapPage(ctx, dbq.JiraMapPageParams{Site: r.site, Kind: "sprint", ProjectID: &pc.project.ID, After: after, Lim: 200})
		if err != nil {
			return err
		}
		for _, row := range rows {
			after = row.JiraID
			if err := r.sprintSnapshot(ctx, row, category); err != nil {
				return err
			}
		}
		if len(rows) < 200 {
			return nil
		}
	}
}

func (r *run) sprintSnapshot(ctx context.Context, row dbq.JiraImportMap, category map[uuid.UUID]string) error {
	q := r.s.Q
	sp, err := q.GetSprint(ctx, row.LocalID)
	if err != nil || sp.State == "future" {
		return nil
	}
	var d sprintData
	_ = json.Unmarshal(row.Data, &d)
	start := d.Start
	if start == nil {
		start = sp.StartDate
	}
	if start == nil {
		return nil
	}
	ids, err := q.IssuesEverInSprint(ctx, dbq.IssuesEverInSprintParams{SprintID: &sp.ID, Committed: []uuid.UUID{}})
	if err != nil {
		return err
	}
	issues, err := q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	hist, err := q.IssueHistoryFor(ctx, ids)
	if err != nil {
		return err
	}
	byIssue := map[uuid.UUID][]dbq.IssueHistoryForRow{}
	for _, h := range hist {
		byIssue[h.IssueID] = append(byIssue[h.IssueID], h)
	}
	committed, completed := []uuid.UUID{}, []uuid.UUID{}
	points := 0.0
	end := d.Complete
	if end == nil {
		end = sp.CompletedAt
	}
	for _, i := range issues {
		h := byIssue[i.ID]
		if !i.CreatedAt.After(*start) && refAt(i.SprintID, h, "sprint", *start) == sp.ID {
			committed = append(committed, i.ID)
			points += pointsAt(i.StoryPoints, h, *start)
		}
		if sp.State == "closed" && end != nil && refAt(i.SprintID, h, "sprint", *end) == sp.ID &&
			category[refAt(&i.StatusID, h, "status", *end)] == "done" {
			completed = append(completed, i.ID)
		}
	}
	return q.ImportSprintSnapshot(ctx, dbq.ImportSprintSnapshotParams{ID: sp.ID, CommittedIssueIds: committed, CommittedPoints: points, CompletedIssueIds: completed})
}

// refAt is the value (status or sprint id) a field had at t: the "from" of its first change
// before any change, then each change's "to".
func refAt(current *uuid.UUID, hist []dbq.IssueHistoryForRow, field string, t time.Time) uuid.UUID {
	var changes []dbq.IssueHistoryForRow
	for _, h := range hist {
		if h.Field == field {
			changes = append(changes, h)
		}
	}
	parse := func(s *string) uuid.UUID {
		if s == nil {
			return uuid.Nil
		}
		id, _ := uuid.Parse(*s)
		return id
	}
	if len(changes) == 0 {
		if current == nil {
			return uuid.Nil
		}
		return *current
	}
	v := parse(changes[0].FromRef)
	for _, h := range changes {
		if h.CreatedAt.After(t) {
			break
		}
		v = parse(h.ToRef)
	}
	return v
}

func pointsAt(current *float64, hist []dbq.IssueHistoryForRow, t time.Time) float64 {
	var changes []dbq.IssueHistoryForRow
	for _, h := range hist {
		if h.Field == "storyPoints" {
			changes = append(changes, h)
		}
	}
	val := func(s *string) float64 {
		if s == nil {
			return 0
		}
		f, _ := strconv.ParseFloat(*s, 64)
		return f
	}
	if len(changes) == 0 {
		if current == nil {
			return 0
		}
		return *current
	}
	v := val(changes[0].FromValue)
	for _, h := range changes {
		if h.CreatedAt.After(t) {
			break
		}
		v = val(h.ToValue)
	}
	return v
}
