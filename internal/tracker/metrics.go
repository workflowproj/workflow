package tracker

import (
	"context"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

/*
 * Metrics derived from issues and their history: per-issue lead and cycle time, summaries,
 * cumulative flow, created vs resolved, breakdowns and velocity. They read the status, sprint
 * and story point changes recorded in issue_history; a scope is a set of projects (every
 * browsable one when none are named) narrowed by optional JQL.
 */

// metricIssueLimit caps the issues a history-based metric (cycle time, flow) reads; beyond it
// the newest count and the report says it was truncated.
const metricIssueLimit = 100000

// MetricScope is the issues a metric looks at.
type MetricScope struct {
	Projects []string
	JQL      string
}

// scopeWhere compiles a scope into a SQL condition over issues aliased i; extraArgs are $2...
func (s *Service) scopeWhere(ctx context.Context, actor *authz.Actor, sc MetricScope, extraArgs ...any) (string, []any, []dbq.Project, error) {
	projects, err := s.projectsByKeys(ctx, actor, sc.Projects)
	if err != nil {
		return "", nil, nil, err
	}
	ids := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
	}
	where, _, args, err := s.compileWhere(ctx, actor, ids, sc.JQL, extraArgs, nil)
	return where, args, projects, err
}

func checkDays(days int) error {
	if days < 1 || days > 365 {
		return apperr.Invalid("days", "days must be between 1 and 365")
	}
	return nil
}

// dayStart is midnight UTC of t's day.
func dayStart(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func hours(d time.Duration) float64 { return math.Round(max(d.Hours(), 0)*10) / 10 }

func median(sorted []float64) float64 { return sorted[len(sorted)/2] }

// ---------------------------------------------------------------------------
// Issue timelines from history

type historyRow = dbq.IssueHistoryForRow

// statusChange: from `at` the issue was in status `to`.
type statusChange struct {
	at time.Time
	to uuid.UUID
}

// statusTimeline is an issue's statuses over time: the first from creation (the "from" of the
// first recorded change, else the current status), then each change.
func statusTimeline(created time.Time, current uuid.UUID, hist []historyRow) []statusChange {
	first := current
	for _, h := range hist {
		if h.Field == "status" {
			if id, ok := refID(h.FromRef); ok {
				first = id
			}
			break
		}
	}
	out := []statusChange{{created, first}}
	for _, h := range hist {
		if h.Field != "status" {
			continue
		}
		if id, ok := refID(h.ToRef); ok {
			out = append(out, statusChange{h.CreatedAt, id})
		}
	}
	return out
}

// statusAt is the status at time t; false before the issue existed.
func statusAt(tl []statusChange, t time.Time) (uuid.UUID, bool) {
	if len(tl) == 0 || t.Before(tl[0].at) {
		return uuid.Nil, false
	}
	k := sort.Search(len(tl), func(k int) bool { return tl[k].at.After(t) }) - 1
	return tl[k].to, true
}

func refID(ref *string) (uuid.UUID, bool) {
	if ref == nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(*ref)
	return id, err == nil
}

// pointsAt is an issue's story points at time t, from its storyPoints changes.
func pointsAt(current *float64, hist []historyRow, t time.Time) float64 {
	var changes []historyRow
	for _, h := range hist {
		if h.Field == "storyPoints" {
			changes = append(changes, h)
		}
	}
	if len(changes) == 0 {
		return deref0(current)
	}
	v := parsePoints(changes[0].FromValue)
	for _, h := range changes {
		if h.CreatedAt.After(t) {
			break
		}
		v = parsePoints(h.ToValue)
	}
	return v
}

func parsePoints(v *string) float64 {
	if v == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(*v, 64)
	return f
}

func deref0(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func (s *Service) historyByIssue(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]historyRow, error) {
	rows, err := s.Q.IssueHistoryFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]historyRow, len(ids))
	for _, r := range rows {
		out[r.IssueID] = append(out[r.IssueID], r)
	}
	return out, nil
}

func (s *Service) statusMap(ctx context.Context) (map[uuid.UUID]dbq.Status, error) {
	statuses, err := s.Q.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]dbq.Status, len(statuses))
	for _, st := range statuses {
		m[st.ID] = st
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// One issue

type StatusTime struct {
	Status dbq.Status
	Hours  float64
	Visits int
}

type IssueMetrics struct {
	CreatedAt                     time.Time
	StartedAt, ResolvedAt         *time.Time
	LeadTimeHours, CycleTimeHours *float64
	AgeHours                      float64
	StatusSince                   time.Time
	StatusAgeHours                float64
	TimeInStatus                  []StatusTime
	Reopened, Sprints             int
}

// IssueMetrics measures one issue from its history. Cycle time runs from first entering an
// in-progress status to resolution (or to now while it is still open).
func (s *Service) IssueMetrics(ctx context.Context, actor *authz.Actor, ref string) (IssueMetrics, error) {
	i, _, _, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return IssueMetrics{}, err
	}
	hist, err := s.historyByIssue(ctx, []uuid.UUID{i.ID})
	if err != nil {
		return IssueMetrics{}, err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return IssueMetrics{}, err
	}
	now := s.Now()
	m := IssueMetrics{CreatedAt: i.CreatedAt, ResolvedAt: i.ResolvedAt, StatusSince: i.StatusChangedAt, StatusAgeHours: hours(now.Sub(i.StatusChangedAt))}
	tl := statusTimeline(i.CreatedAt, i.StatusID, hist[i.ID])
	index := map[uuid.UUID]int{}
	for k, c := range tl {
		until := now
		if k+1 < len(tl) {
			until = tl[k+1].at
		}
		at, ok := index[c.to]
		if !ok {
			at = len(m.TimeInStatus)
			index[c.to] = at
			m.TimeInStatus = append(m.TimeInStatus, StatusTime{Status: statuses[c.to]})
		}
		m.TimeInStatus[at].Hours += max(until.Sub(c.at).Hours(), 0)
		m.TimeInStatus[at].Visits++
		cat := statuses[c.to].Category
		if cat == "in_progress" && m.StartedAt == nil {
			t := c.at
			m.StartedAt = &t
		}
		if k > 0 && statuses[tl[k-1].to].Category == "done" && cat != "done" {
			m.Reopened++
		}
	}
	for k := range m.TimeInStatus {
		m.TimeInStatus[k].Hours = math.Round(m.TimeInStatus[k].Hours*10) / 10
	}
	end := now
	if i.ResolvedAt != nil {
		end = *i.ResolvedAt
		lead := hours(end.Sub(i.CreatedAt))
		m.LeadTimeHours = &lead
	}
	if m.StartedAt != nil && (i.ResolvedAt != nil || statuses[i.StatusID].Category != "done") {
		cycle := hours(end.Sub(*m.StartedAt))
		m.CycleTimeHours = &cycle
	}
	m.AgeHours = hours(end.Sub(i.CreatedAt))
	sprints := map[uuid.UUID]bool{}
	if i.SprintID != nil {
		sprints[*i.SprintID] = true
	}
	for _, h := range hist[i.ID] {
		if id, ok := refID(h.ToRef); ok && h.Field == "sprint" {
			sprints[id] = true
		}
	}
	m.Sprints = len(sprints)
	return m, nil
}

// ---------------------------------------------------------------------------
// Summary

type MetricsSummary struct {
	Days                                                           int
	Open, Todo, InProgress, Created, Resolved, Overdue, Unassigned int
	OpenPoints, ResolvedPoints                                     float64
	MedianCycleHours                                               *float64
}

func (s *Service) MetricsSummary(ctx context.Context, actor *authz.Actor, sc MetricScope, days int) (MetricsSummary, error) {
	if err := checkDays(days); err != nil {
		return MetricsSummary{}, err
	}
	now := s.Now()
	since := now.AddDate(0, 0, -days)
	where, args, _, err := s.scopeWhere(ctx, actor, sc, since, dayStart(now))
	if err != nil {
		return MetricsSummary{}, err
	}
	out := MetricsSummary{Days: days}
	err = s.Pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE st.category <> 'done'),
		count(*) FILTER (WHERE st.category = 'todo'),
		count(*) FILTER (WHERE st.category = 'in_progress'),
		count(*) FILTER (WHERE i.created_at >= $2),
		count(*) FILTER (WHERE i.resolved_at >= $2),
		count(*) FILTER (WHERE st.category <> 'done' AND i.due_date < $3::date),
		count(*) FILTER (WHERE st.category <> 'done' AND i.assignee_id IS NULL),
		coalesce(sum(i.story_points) FILTER (WHERE st.category <> 'done'), 0)::float8,
		coalesce(sum(i.story_points) FILTER (WHERE i.resolved_at >= $2), 0)::float8
		FROM issues i JOIN statuses st ON st.id = i.status_id WHERE `+where, args...).Scan(
		&out.Open, &out.Todo, &out.InProgress, &out.Created, &out.Resolved, &out.Overdue, &out.Unassigned, &out.OpenPoints, &out.ResolvedPoints)
	if err != nil {
		return out, err
	}
	ct, err := s.CycleTimeFor(ctx, actor, sc, days)
	if err != nil {
		return out, err
	}
	if len(ct.Issues) > 0 {
		out.MedianCycleHours = &ct.Median
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Cycle time

type CycleTimeEntry struct {
	Key, ProjectKey, Summary string
	Hours                    float64
	ResolvedAt               time.Time
}

type CycleTimeReport struct {
	Days                 int
	Average, Median, P85 float64
	Issues               []CycleTimeEntry
	// Truncated: more issues were resolved than metricIssueLimit; the most recent count.
	Truncated bool
}

// CycleTime is the cycle time report of one project.
func (s *Service) CycleTime(ctx context.Context, actor *authz.Actor, ref string, days int) (CycleTimeReport, error) {
	return s.CycleTimeFor(ctx, actor, MetricScope{Projects: []string{ref}}, days)
}

// CycleTimeFor measures, from issue history, the time from first entering an in-progress status
// to resolution, for issues resolved in the last `days` days (from creation when they never
// were in progress).
func (s *Service) CycleTimeFor(ctx context.Context, actor *authz.Actor, sc MetricScope, days int) (CycleTimeReport, error) {
	rep := CycleTimeReport{Days: days, Issues: []CycleTimeEntry{}}
	if err := checkDays(days); err != nil {
		return rep, err
	}
	where, args, projects, err := s.scopeWhere(ctx, actor, sc, s.Now().AddDate(0, 0, -days))
	if err != nil {
		return rep, err
	}
	// Work starts when the issue first entered an in-progress status (else when it was created).
	rows, err := s.Pool.Query(ctx, `SELECT i.id, i.project_id, i.number, i.summary, i.resolved_at,
			coalesce((SELECT min(h.created_at) FROM issue_history h JOIN statuses st ON st.id::text = h.to_ref
				WHERE h.issue_id = i.id AND h.field = 'status' AND st.category = 'in_progress'), i.created_at)
		FROM issues i WHERE i.resolved_at >= $2 AND `+where+` ORDER BY i.resolved_at DESC, i.id LIMIT `+strconv.Itoa(metricIssueLimit+1), args...)
	if err != nil {
		return rep, err
	}
	type resolved struct {
		ID, ProjectID   uuid.UUID
		Number          int32
		Summary         string
		Resolved, Start time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (resolved, error) {
		var v resolved
		err := r.Scan(&v.ID, &v.ProjectID, &v.Number, &v.Summary, &v.Resolved, &v.Start)
		return v, err
	})
	if err != nil {
		return rep, err
	}
	if len(list) > metricIssueLimit {
		list, rep.Truncated = list[:metricIssueLimit], true
	}
	slices.Reverse(list)
	keys := map[uuid.UUID]string{}
	for _, p := range projects {
		keys[p.ID] = p.Key
	}
	var all []float64
	for _, v := range list {
		hrs := hours(v.Resolved.Sub(v.Start))
		all = append(all, hrs)
		pk := keys[v.ProjectID]
		rep.Issues = append(rep.Issues, CycleTimeEntry{Key: pk + "-" + strconv.Itoa(int(v.Number)), ProjectKey: pk, Summary: v.Summary, Hours: hrs, ResolvedAt: v.Resolved})
	}
	if len(all) > 0 {
		sum := 0.0
		for _, h := range all {
			sum += h
		}
		sort.Float64s(all)
		rep.Average = math.Round(sum/float64(len(all))*10) / 10
		rep.Median = median(all)
		rep.P85 = all[min(len(all)-1, int(math.Ceil(0.85*float64(len(all))))-1)]
	}
	return rep, nil
}

// ---------------------------------------------------------------------------
// Created vs resolved

type CRBucket struct {
	Start                   time.Time
	Created, Resolved, Open int
}

// CreatedResolved counts issues created and resolved per day or week (weeks start on Monday)
// over the window, and how many were open at the end of each.
func (s *Service) CreatedResolved(ctx context.Context, actor *authz.Actor, sc MetricScope, days int, interval string) ([]CRBucket, error) {
	if err := checkDays(days); err != nil {
		return nil, err
	}
	step := 1
	switch interval {
	case "", "day":
	case "week":
		step = 7
	default:
		return nil, apperr.Invalid("interval", "interval must be day or week")
	}
	where, args, _, err := s.scopeWhere(ctx, actor, sc)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT i.created_at, i.resolved_at FROM issues i WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	var created, resolved []time.Time
	for rows.Next() {
		var c time.Time
		var r *time.Time
		if err := rows.Scan(&c, &r); err != nil {
			rows.Close()
			return nil, err
		}
		created = append(created, c)
		if r != nil {
			resolved = append(resolved, *r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(created, func(a, b time.Time) int { return a.Compare(b) })
	slices.SortFunc(resolved, func(a, b time.Time) int { return a.Compare(b) })
	before := func(list []time.Time, t time.Time) int {
		return sort.Search(len(list), func(k int) bool { return !list[k].Before(t) })
	}
	today := dayStart(s.Now())
	start := today.AddDate(0, 0, -(days - 1))
	if step == 7 {
		start = start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
	}
	var out []CRBucket
	for b := start; !b.After(today); b = b.AddDate(0, 0, step) {
		end := b.AddDate(0, 0, step)
		out = append(out, CRBucket{
			Start:    b,
			Created:  before(created, end) - before(created, b),
			Resolved: before(resolved, end) - before(resolved, b),
			Open:     before(created, end) - before(resolved, end),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Cumulative flow

type FlowSeries struct {
	Key, Label, Category string
	Counts               []int
}

var categoryOrder = map[string]int{"todo": 0, "in_progress": 1, "done": 2}
var categoryLabel = map[string]string{"todo": "To do", "in_progress": "In progress", "done": "Done"}

// CumulativeFlow counts the issues in each status category (or status) at the end of each day.
// Issues resolved before the window are left out, so the done band is work finished in it.
func (s *Service) CumulativeFlow(ctx context.Context, actor *authz.Actor, sc MetricScope, days int, by string) (dates []time.Time, out []FlowSeries, truncated bool, err error) {
	if err := checkDays(days); err != nil {
		return nil, nil, false, err
	}
	if by == "" {
		by = "category"
	}
	if by != "category" && by != "status" {
		return nil, nil, false, apperr.Invalid("by", "by must be category or status")
	}
	now := s.Now()
	today := dayStart(now)
	start := today.AddDate(0, 0, -(days - 1))
	where, args, _, err := s.scopeWhere(ctx, actor, sc, start)
	if err != nil {
		return nil, nil, false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT i.id, i.status_id, i.created_at FROM issues i
		WHERE (i.resolved_at IS NULL OR i.resolved_at >= $2) AND `+where+` ORDER BY i.created_at DESC, i.id LIMIT `+strconv.Itoa(metricIssueLimit+1), args...)
	if err != nil {
		return nil, nil, false, err
	}
	type flowIssue struct {
		ID, Status uuid.UUID
		Created    time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (flowIssue, error) {
		var v flowIssue
		err := r.Scan(&v.ID, &v.Status, &v.Created)
		return v, err
	})
	if err != nil {
		return nil, nil, false, err
	}
	if len(list) > metricIssueLimit {
		list, truncated = list[:metricIssueLimit], true
	}
	ids := make([]uuid.UUID, len(list))
	for k, v := range list {
		ids[k] = v.ID
	}
	hist, err := s.historyByIssue(ctx, ids)
	if err != nil {
		return nil, nil, false, err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d)
	}
	series := map[string]*FlowSeries{}
	keyOf := func(st dbq.Status) string {
		if by == "category" {
			return st.Category
		}
		return st.ID.String()
	}
	if by == "category" {
		for cat := range categoryOrder {
			series[cat] = &FlowSeries{Key: cat, Label: categoryLabel[cat], Category: cat, Counts: make([]int, len(dates))}
		}
	}
	// Each day counts the issue's status at its end (or now, today). A status holds from one
	// change to the next, which covers a run of days: add +1 at the run's first day and -1 after
	// its last, then sum, so the work is per status change rather than per issue per day.
	ends := make([]time.Time, len(dates))
	for d, day := range dates {
		ends[d] = day.AddDate(0, 0, 1)
		if ends[d].After(now) {
			ends[d] = now
		}
	}
	firstDayEndingFrom := func(t time.Time) int {
		return sort.Search(len(ends), func(d int) bool { return !ends[d].Before(t) })
	}
	diffs := map[string][]int{}
	for _, v := range list {
		tl := statusTimeline(v.Created, v.Status, hist[v.ID])
		for k, c := range tl {
			from, to := firstDayEndingFrom(c.at), len(dates)
			if k+1 < len(tl) {
				to = firstDayEndingFrom(tl[k+1].at)
			}
			if from >= to {
				continue
			}
			st := statuses[c.to]
			key := keyOf(st)
			if series[key] == nil {
				series[key] = &FlowSeries{Key: key, Label: st.Name, Category: st.Category, Counts: make([]int, len(dates))}
			}
			if diffs[key] == nil {
				diffs[key] = make([]int, len(dates)+1)
			}
			diffs[key][from]++
			diffs[key][to]--
		}
	}
	for key, diff := range diffs {
		run := 0
		for d := range dates {
			run += diff[d]
			series[key].Counts[d] = run
		}
	}
	out = make([]FlowSeries, 0, len(series))
	for _, fs := range series {
		out = append(out, *fs)
	}
	slices.SortFunc(out, func(a, b FlowSeries) int {
		if c := categoryOrder[a.Category] - categoryOrder[b.Category]; c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return dates, out, truncated, nil
}

// ---------------------------------------------------------------------------
// Breakdown

type BreakdownGroup struct {
	Key, Label, Category, Color string
	Count                       int
	Points                      float64
	User                        *dbq.User
}

var breakdownNone = map[string]string{
	"assignee": "Unassigned", "label": "No label", "epic": "No epic", "sprint": "No sprint", "release": "No release",
}

var priorityOrder = []string{"highest", "high", "medium", "low", "lowest"}

// breakdownKeys is, per field, the group an issue counts in (NULL is the "None" group) and any
// join it needs. An issue with several labels or releases joins to several rows. Epics are the
// parent when it is epic-level, else the grandparent (sub-tasks).
var breakdownKeys = map[string]struct{ join, key string }{
	"status":         {"", "i.status_id::text"},
	"statusCategory": {"JOIN statuses bst ON bst.id = i.status_id", "bst.category"},
	"priority":       {"", "i.priority"},
	"type":           {"", "i.type_id::text"},
	"assignee":       {"", "i.assignee_id::text"},
	"sprint":         {"", "i.sprint_id::text"},
	"project":        {"", "i.project_id::text"},
	"label":          {"LEFT JOIN LATERAL unnest(i.labels) bl(label) ON true", "bl.label"},
	"release":        {"LEFT JOIN issue_releases bir ON bir.issue_id = i.id", "bir.release_id::text"},
	"epic": {`LEFT JOIN issues bp ON bp.id = i.parent_id LEFT JOIN issue_types bpt ON bpt.id = bp.type_id
		LEFT JOIN issues bgp ON bgp.id = bp.parent_id LEFT JOIN issue_types bgpt ON bgpt.id = bgp.type_id`,
		"(CASE WHEN bpt.level >= 1 THEN bp.id WHEN bgpt.level >= 1 THEN bgp.id END)::text"},
}

// Breakdown groups the matching issues by a field. Groups are largest first, except priority and
// status category, which keep their natural order; "None" comes last. An issue with several
// labels or releases counts in each. The database does the counting, so every matching issue
// counts however many there are.
func (s *Service) Breakdown(ctx context.Context, actor *authz.Actor, sc MetricScope, by string) ([]BreakdownGroup, int, float64, error) {
	k, ok := breakdownKeys[by]
	if !ok {
		return nil, 0, 0, apperr.Invalid("by", "unknown breakdown field %q", by)
	}
	where, args, projects, err := s.scopeWhere(ctx, actor, sc)
	if err != nil {
		return nil, 0, 0, err
	}
	var total int
	var totalPoints float64
	if err := s.Pool.QueryRow(ctx, `SELECT count(*), coalesce(sum(i.story_points), 0)::float8 FROM issues i WHERE `+where, args...).Scan(&total, &totalPoints); err != nil {
		return nil, 0, 0, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+k.key+`, count(*), coalesce(sum(i.story_points), 0)::float8
		FROM issues i `+k.join+` WHERE `+where+` GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return nil, 0, 0, err
	}
	type row struct {
		Key    *string
		Count  int
		Points float64
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var v row
		err := r.Scan(&v.Key, &v.Count, &v.Points)
		return v, err
	})
	if err != nil {
		return nil, 0, 0, err
	}

	groups := map[string]*BreakdownGroup{}
	var order []string
	add := func(v row, key, label string, fill func(*BreakdownGroup)) {
		g := groups[key]
		if g == nil {
			g = &BreakdownGroup{Key: key, Label: label}
			if fill != nil {
				fill(g)
			}
			groups[key] = g
			order = append(order, key)
		}
		g.Count += v.Count
		g.Points += v.Points
	}
	none := func(v row) { add(v, "", cmpOr(breakdownNone[by], "None"), nil) }
	// ids are the groups' keys as ids, for looking up names.
	ids := func() []uuid.UUID {
		var out []uuid.UUID
		for _, v := range list {
			if v.Key != nil {
				if id, err := uuid.Parse(*v.Key); err == nil {
					out = append(out, id)
				}
			}
		}
		return out
	}
	// each calls f with every group's id, or counts it as None when it has no id or f says so.
	each := func(f func(v row, id uuid.UUID) bool) {
		for _, v := range list {
			id, err := uuid.Nil, error(nil)
			if v.Key != nil {
				id, err = uuid.Parse(*v.Key)
			}
			if v.Key == nil || err != nil || !f(v, id) {
				none(v)
			}
		}
	}

	switch by {
	case "status":
		statuses, err := s.statusMap(ctx)
		if err != nil {
			return nil, 0, 0, err
		}
		each(func(v row, id uuid.UUID) bool {
			st := statuses[id]
			add(v, st.ID.String(), st.Name, func(g *BreakdownGroup) { g.Category = st.Category })
			return true
		})
	case "statusCategory":
		for _, v := range list {
			cat := deref(v.Key)
			add(v, cat, categoryLabel[cat], func(g *BreakdownGroup) { g.Category = cat })
		}
	case "priority":
		for _, v := range list {
			p := deref(v.Key)
			add(v, p, strings.ToUpper(p[:1])+p[1:], nil)
		}
	case "type":
		// Types are per project; the same key in several projects is one group.
		types, err := s.Q.GetIssueTypesByIDs(ctx, ids())
		if err != nil {
			return nil, 0, 0, err
		}
		byID := map[uuid.UUID]dbq.IssueType{}
		for _, t := range types {
			byID[t.ID] = t
		}
		each(func(v row, id uuid.UUID) bool {
			t := byID[id]
			add(v, t.Key, t.Name, func(g *BreakdownGroup) { g.Color = t.Color })
			return true
		})
	case "assignee":
		users, err := s.Q.GetUsersByIDs(ctx, ids())
		if err != nil {
			return nil, 0, 0, err
		}
		byID := map[uuid.UUID]dbq.User{}
		for _, u := range users {
			byID[u.ID] = u
		}
		each(func(v row, id uuid.UUID) bool {
			u, ok := byID[id]
			if ok {
				add(v, u.ID.String(), u.DisplayName, func(g *BreakdownGroup) { g.User = &u })
			}
			return ok
		})
	case "label":
		for _, v := range list {
			if v.Key == nil {
				none(v)
				continue
			}
			add(v, *v.Key, *v.Key, nil)
		}
	case "sprint":
		sprints, err := s.Q.GetSprintsByIDs(ctx, ids())
		if err != nil {
			return nil, 0, 0, err
		}
		byID := map[uuid.UUID]dbq.Sprint{}
		for _, sp := range sprints {
			byID[sp.ID] = sp
		}
		each(func(v row, id uuid.UUID) bool {
			sp, ok := byID[id]
			if ok {
				add(v, sp.ID.String(), sp.Name, nil)
			}
			return ok
		})
	case "release":
		releases, err := s.Q.GetReleasesByIDs(ctx, ids())
		if err != nil {
			return nil, 0, 0, err
		}
		names := map[uuid.UUID]string{}
		for _, r := range releases {
			names[r.ID] = r.Name
		}
		each(func(v row, id uuid.UUID) bool {
			add(v, id.String(), names[id], nil)
			return true
		})
	case "epic":
		list, err := s.Q.GetIssuesByIDs(ctx, ids())
		if err != nil {
			return nil, 0, 0, err
		}
		epics := make(map[uuid.UUID]dbq.Issue, len(list))
		for _, e := range list {
			epics[e.ID] = e
		}
		keys, err := s.projectKeysOf(ctx, epics, projects)
		if err != nil {
			return nil, 0, 0, err
		}
		each(func(v row, id uuid.UUID) bool {
			e, ok := epics[id]
			if ok {
				add(v, keys[e.ProjectID]+"-"+strconv.Itoa(int(e.Number)), e.Summary, nil)
			}
			return ok
		})
	case "project":
		byID := map[uuid.UUID]dbq.Project{}
		for _, p := range projects {
			byID[p.ID] = p
		}
		each(func(v row, id uuid.UUID) bool {
			p := byID[id]
			add(v, p.Key, p.Name, nil)
			return true
		})
	}
	out := make([]BreakdownGroup, 0, len(order))
	for _, k := range order {
		g := groups[k]
		g.Points = math.Round(g.Points*100) / 100
		out = append(out, *g)
	}
	rank := func(g BreakdownGroup) int {
		switch by {
		case "priority":
			return slices.Index(priorityOrder, g.Key)
		case "statusCategory":
			return categoryOrder[g.Key]
		}
		return 0
	}
	slices.SortStableFunc(out, func(a, b BreakdownGroup) int {
		if (a.Key == "") != (b.Key == "") {
			if a.Key == "" {
				return 1
			}
			return -1
		}
		if c := rank(a) - rank(b); c != 0 {
			return c
		}
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if c := strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label)); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out, total, math.Round(totalPoints*100) / 100, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func dbqUser(byID map[uuid.UUID]dbq.User, id *uuid.UUID) (dbq.User, bool) {
	if id == nil {
		return dbq.User{}, false
	}
	u, ok := byID[*id]
	return u, ok
}

// projectKeysOf maps the projects of the given issues to their keys.
func (s *Service) projectKeysOf(ctx context.Context, issues map[uuid.UUID]dbq.Issue, known []dbq.Project) (map[uuid.UUID]string, error) {
	keys := make(map[uuid.UUID]string, len(known))
	for _, p := range known {
		keys[p.ID] = p.Key
	}
	var missing []uuid.UUID
	for _, i := range issues {
		if _, ok := keys[i.ProjectID]; !ok && !slices.Contains(missing, i.ProjectID) {
			missing = append(missing, i.ProjectID)
		}
	}
	if len(missing) > 0 {
		ps, err := s.Q.ListProjectsByIDs(ctx, missing)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			keys[p.ID] = p.Key
		}
	}
	return keys, nil
}
