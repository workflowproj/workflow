package tracker

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/timetrack"
)

/*
 * Time tracking: an issue's original and remaining estimates, and the work logged against it.
 * Time spent is the sum of the worklogs. Logging work lowers the remaining estimate by the time
 * logged (never below zero) unless the request sets the remaining estimate or leaves it alone;
 * editing or deleting a worklog moves it back the same way. History records estimate changes
 * (refs carry seconds, which sprint reports replay) and each worklog added, changed or deleted.
 */

// maxDuration caps estimates and single worklogs (10,000 hours).
const maxDuration = 10_000 * 3600

// Units is the configured working day and week ("1d", "1w").
func (s *Service) Units() timetrack.Units {
	if s.Cfg == nil || s.Cfg.TimeTracking.HoursPerDay <= 0 || s.Cfg.TimeTracking.DaysPerWeek <= 0 {
		return timetrack.Default
	}
	return timetrack.Units{HoursPerDay: s.Cfg.TimeTracking.HoursPerDay, DaysPerWeek: s.Cfg.TimeTracking.DaysPerWeek}
}

func checkEstimate(field string, v *int64) error {
	if v != nil && (*v < 0 || *v > maxDuration) {
		return apperr.Invalid(field, "an estimate must be between 0 and 10,000 hours")
	}
	return nil
}

func int64PtrEqual(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// secondsRef is the machine-readable history value of a duration.
func secondsRef(v *int64) *string {
	if v == nil {
		return nil
	}
	return strp(strconv.FormatInt(*v, 10))
}

// recordEstimate adds an estimate change to an issue update's history.
func (s *Service) estimateChange(field string, from, to *int64) change {
	u := s.Units()
	return change{field: field, from: timetrack.FormatPtr(from, u), to: timetrack.FormatPtr(to, u), fromRef: secondsRef(from), toRef: secondsRef(to)}
}

// TimeSpent is the time logged on each of the issues (absent when none).
func (s *Service) TimeSpent(ctx context.Context, q *dbq.Queries, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.IssueTimeSpent(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.IssueID] = r.Seconds
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Worklogs

// AdjustAuto and AdjustLeave say what logging work does to the remaining estimate.
const (
	AdjustAuto  = "auto"
	AdjustLeave = "leave"
)

type WorklogInput struct {
	StartedAt *time.Time
	Seconds   int64
	Comment   string
	// Adjust is auto (default) or leave; Remaining, when set, wins and sets the estimate.
	Adjust    string
	Remaining *int64
}

type WorklogPatch struct {
	StartedAt *time.Time
	Seconds   *int64
	Comment   *string
	Adjust    string
	Remaining *int64
}

func checkAdjust(adjust string, remaining *int64) error {
	if adjust != "" && adjust != AdjustAuto && adjust != AdjustLeave {
		return apperr.Invalid("adjustEstimate", "adjustEstimate must be auto or leave")
	}
	return checkEstimate("remainingEstimateSeconds", remaining)
}

func checkWorklogSeconds(n int64) error {
	if n < 60 || n > maxDuration {
		return apperr.Invalid("timeSpentSeconds", "time spent must be between 1 minute and 10,000 hours")
	}
	return nil
}

func (s *Service) checkStarted(t time.Time) error {
	if t.After(s.Now().Add(24 * time.Hour)) {
		return apperr.Invalid("startedAt", "work cannot be logged more than a day ahead")
	}
	if t.Year() < 2000 {
		return apperr.Invalid("startedAt", "that date is too far back")
	}
	return nil
}

func cleanComment(c string) (string, error) {
	c = strings.TrimSpace(c)
	if len(c) > 10000 {
		return "", apperr.Invalid("comment", "a worklog comment must be at most 10,000 characters")
	}
	return c, nil
}

// adjustRemaining applies a change in time logged (delta, positive when more was logged) to
// the remaining estimate: set when explicit, unchanged when left or never estimated,
// otherwise moved by the delta and kept at zero or more.
func adjustRemaining(cur *int64, delta int64, adjust string, explicit *int64) *int64 {
	switch {
	case explicit != nil:
		v := *explicit
		return &v
	case adjust == AdjustLeave || cur == nil:
		return cur
	}
	v := max(*cur-delta, 0)
	return &v
}

// CanEditWorklog: your own worklogs while you may work on the project's issues, anyone's as a
// project admin.
func CanEditWorklog(access authz.Access, actor *authz.Actor, w dbq.Worklog) bool {
	if access.Has(authz.ProjectAdmin) {
		return true
	}
	uid := actor.UserID()
	own := uid != nil && w.AuthorID != nil && *w.AuthorID == *uid
	return own && (access.Has(authz.IssueEdit) || access.Has(authz.IssueEditOthers))
}

func (s *Service) ListWorklogs(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.Worklog, dbq.Issue, dbq.Project, authz.Access, error) {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, i, p, access, err
	}
	ws, err := s.Q.ListWorklogs(ctx, i.ID)
	return ws, i, p, access, err
}

// AddWorklog logs work on an issue; it needs permission to edit the issue.
func (s *Service) AddWorklog(ctx context.Context, actor *authz.Actor, ref string, in WorklogInput) (dbq.Worklog, dbq.Issue, dbq.Project, error) {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return dbq.Worklog{}, i, p, err
	}
	if actor.User == nil {
		return dbq.Worklog{}, i, p, apperr.Forbidden("only people can log work")
	}
	if !canEdit(access, actor, i) {
		return dbq.Worklog{}, i, p, apperr.Forbidden("you cannot log work on %s", issueKey(p, i))
	}
	if err := checkWorklogSeconds(in.Seconds); err != nil {
		return dbq.Worklog{}, i, p, err
	}
	if err := checkAdjust(in.Adjust, in.Remaining); err != nil {
		return dbq.Worklog{}, i, p, err
	}
	started := s.Now()
	if in.StartedAt != nil {
		started = *in.StartedAt
	}
	if err := s.checkStarted(started); err != nil {
		return dbq.Worklog{}, i, p, err
	}
	comment, err := cleanComment(in.Comment)
	if err != nil {
		return dbq.Worklog{}, i, p, err
	}
	var w dbq.Worklog
	var out dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		cur, err := q.GetIssueForUpdate(ctx, i.ID)
		if err != nil {
			return err
		}
		w, err = q.CreateWorklog(ctx, dbq.CreateWorklogParams{IssueID: cur.ID, AuthorID: actor.UserID(), StartedAt: started, DurationSeconds: in.Seconds, Comment: comment})
		if err != nil {
			return err
		}
		remaining := adjustRemaining(cur.RemainingEstimateSeconds, in.Seconds, in.Adjust, in.Remaining)
		out, err = s.afterWorklog(ctx, tx, q, actor, p, cur, remaining, change{field: "worklog", to: strp(timetrack.Format(in.Seconds, s.Units())), toRef: strp(w.ID.String())}, w.ID)
		return err
	})
	return w, out, p, err
}

// afterWorklog stores the new remaining estimate (bumping the issue's version), records the
// worklog and estimate changes and publishes the update.
func (s *Service) afterWorklog(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, p dbq.Project, cur dbq.Issue, remaining *int64, logged change, worklogID uuid.UUID) (dbq.Issue, error) {
	out, err := q.SetRemainingEstimate(ctx, dbq.SetRemainingEstimateParams{ID: cur.ID, RemainingEstimateSeconds: remaining})
	if err != nil {
		return out, err
	}
	changes := []change{logged}
	fields := []string{"timeSpent"}
	if !int64PtrEqual(cur.RemainingEstimateSeconds, remaining) {
		changes = append(changes, s.estimateChange("remainingEstimate", cur.RemainingEstimateSeconds, remaining))
		fields = append(fields, "remainingEstimate")
	}
	for _, c := range changes {
		if err := s.history(ctx, q, cur.ID, actor, c.field, c.from, c.to, c.fromRef, c.toRef); err != nil {
			return out, err
		}
	}
	return out, s.emit(ctx, tx, actor, p, &out, "issue.updated", map[string]any{"fields": fields, "worklogId": worklogID})
}

// worklogForEdit loads a worklog the actor may change.
func (s *Service) worklogForEdit(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Worklog, dbq.Issue, dbq.Project, error) {
	w, err := s.Q.GetWorklog(ctx, id)
	if err != nil {
		return w, dbq.Issue{}, dbq.Project{}, apperr.NotFound("worklog")
	}
	i, p, access, err := s.IssueFor(ctx, actor, w.IssueID.String(), authz.ProjectBrowse)
	if err != nil {
		if apperr.IsNotFound(err) {
			return w, i, p, apperr.NotFound("worklog")
		}
		return w, i, p, err
	}
	if !CanEditWorklog(access, actor, w) {
		return w, i, p, apperr.Forbidden("you can only change your own work logs")
	}
	return w, i, p, nil
}

func (s *Service) UpdateWorklog(ctx context.Context, actor *authz.Actor, id uuid.UUID, in WorklogPatch) (dbq.Worklog, dbq.Issue, dbq.Project, error) {
	_, i, p, err := s.worklogForEdit(ctx, actor, id)
	if err != nil {
		return dbq.Worklog{}, i, p, err
	}
	if in.Seconds != nil {
		if err := checkWorklogSeconds(*in.Seconds); err != nil {
			return dbq.Worklog{}, i, p, err
		}
	}
	if in.StartedAt != nil {
		if err := s.checkStarted(*in.StartedAt); err != nil {
			return dbq.Worklog{}, i, p, err
		}
	}
	if err := checkAdjust(in.Adjust, in.Remaining); err != nil {
		return dbq.Worklog{}, i, p, err
	}
	var w dbq.Worklog
	var out dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		cur, err := q.GetIssueForUpdate(ctx, i.ID)
		if err != nil {
			return err
		}
		before, err := q.GetWorklog(ctx, id)
		if err != nil {
			return apperr.NotFound("worklog")
		}
		upd := dbq.UpdateWorklogParams{ID: id, StartedAt: before.StartedAt, DurationSeconds: before.DurationSeconds, Comment: before.Comment}
		if in.StartedAt != nil {
			upd.StartedAt = *in.StartedAt
		}
		if in.Seconds != nil {
			upd.DurationSeconds = *in.Seconds
		}
		if in.Comment != nil {
			if upd.Comment, err = cleanComment(*in.Comment); err != nil {
				return err
			}
		}
		if w, err = q.UpdateWorklog(ctx, upd); err != nil {
			return err
		}
		if upd.DurationSeconds == before.DurationSeconds && upd.StartedAt.Equal(before.StartedAt) && in.Remaining == nil {
			// Only the comment changed: nothing the issue shows moved.
			return publishOnly(ctx, tx, actor, p, &cur)
		}
		u := s.Units()
		remaining := adjustRemaining(cur.RemainingEstimateSeconds, upd.DurationSeconds-before.DurationSeconds, in.Adjust, in.Remaining)
		logged := change{field: "worklog", from: strp(timetrack.Format(before.DurationSeconds, u)), to: strp(timetrack.Format(upd.DurationSeconds, u)), fromRef: strp(id.String()), toRef: strp(id.String())}
		out, err = s.afterWorklog(ctx, tx, q, actor, p, cur, remaining, logged, id)
		return err
	})
	return w, out, p, err
}

// DeleteWorklog removes a worklog; by default its time goes back on the remaining estimate.
func (s *Service) DeleteWorklog(ctx context.Context, actor *authz.Actor, id uuid.UUID, adjust string, remaining *int64) error {
	_, i, p, err := s.worklogForEdit(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := checkAdjust(adjust, remaining); err != nil {
		return err
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		cur, err := q.GetIssueForUpdate(ctx, i.ID)
		if err != nil {
			return err
		}
		w, err := q.GetWorklog(ctx, id)
		if err != nil {
			return apperr.NotFound("worklog")
		}
		if err := q.DeleteWorklog(ctx, id); err != nil {
			return err
		}
		next := adjustRemaining(cur.RemainingEstimateSeconds, -w.DurationSeconds, adjust, remaining)
		_, err = s.afterWorklog(ctx, tx, q, actor, p, cur, next, change{field: "worklog", from: strp(timetrack.Format(w.DurationSeconds, s.Units())), fromRef: strp(id.String())}, id)
		return err
	})
}

// ---------------------------------------------------------------------------
// Timesheet

type TimesheetQuery struct {
	From, To time.Time
	Projects []string
	UserID   *uuid.UUID
	TimeZone string
}

type TimesheetIssue struct {
	Issue dbq.Issue
	Days  []int64
	Total int64
}

type TimesheetPerson struct {
	User   *dbq.User
	Days   []int64
	Total  int64
	Issues []TimesheetIssue
}

type Timesheet struct {
	From, To  time.Time
	TimeZone  string
	Dates     []time.Time
	DayTotals []int64
	Total     int64
	People    []TimesheetPerson
	// Lookups for rendering the issues.
	lookups *lookups
}

// IssueRef renders one of the timesheet's issues.
func (t Timesheet) IssueRef(i dbq.Issue) api.IssueRef { return t.lookups.ref(i) }

// Timesheet totals the time logged per person, issue and day on issues the actor can browse.
func (s *Service) Timesheet(ctx context.Context, actor *authz.Actor, in TimesheetQuery) (Timesheet, error) {
	from, to := dayStart(in.From), dayStart(in.To)
	if to.Before(from) {
		return Timesheet{}, apperr.Invalid("to", "the last day is before the first")
	}
	if to.Sub(from) > 366*24*time.Hour {
		return Timesheet{}, apperr.Invalid("to", "a timesheet covers at most 366 days")
	}
	tz := strings.TrimSpace(in.TimeZone)
	if tz == "" {
		tz = "UTC"
	}
	if err := s.checkTimeZone(ctx, tz); err != nil {
		return Timesheet{}, err
	}
	projects, err := s.projectsByKeys(ctx, actor, in.Projects)
	if err != nil {
		return Timesheet{}, err
	}
	ids := make([]uuid.UUID, len(projects))
	for k, p := range projects {
		ids[k] = p.ID
	}
	out := Timesheet{From: from, To: to, TimeZone: tz}
	index := map[string]int{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		index[d.Format(time.DateOnly)] = len(out.Dates)
		out.Dates = append(out.Dates, d)
	}
	out.DayTotals = make([]int64, len(out.Dates))
	rows, err := s.Q.TimesheetRows(ctx, dbq.TimesheetRowsParams{Tz: tz, ProjectIds: ids, FromDay: from, ToDay: to, AuthorID: in.UserID})
	if err != nil {
		return out, err
	}
	people := map[uuid.UUID]*TimesheetPerson{}
	issues := map[uuid.UUID]map[uuid.UUID]*TimesheetIssue{}
	var order []uuid.UUID
	var issueIDs, userIDs []uuid.UUID
	for _, r := range rows {
		k, ok := index[r.Day.Format(time.DateOnly)]
		if !ok {
			continue
		}
		author := uuid.Nil
		if r.AuthorID != nil {
			author = *r.AuthorID
		}
		person := people[author]
		if person == nil {
			person = &TimesheetPerson{Days: make([]int64, len(out.Dates))}
			people[author] = person
			issues[author] = map[uuid.UUID]*TimesheetIssue{}
			order = append(order, author)
			if author != uuid.Nil {
				userIDs = append(userIDs, author)
			}
		}
		ti := issues[author][r.IssueID]
		if ti == nil {
			ti = &TimesheetIssue{Issue: dbq.Issue{ID: r.IssueID}, Days: make([]int64, len(out.Dates))}
			issues[author][r.IssueID] = ti
			issueIDs = append(issueIDs, r.IssueID)
		}
		ti.Days[k] += r.Seconds
		ti.Total += r.Seconds
		person.Days[k] += r.Seconds
		person.Total += r.Seconds
		out.DayTotals[k] += r.Seconds
		out.Total += r.Seconds
	}
	loaded, err := s.Q.GetIssuesByIDs(ctx, issueIDs)
	if err != nil {
		return out, err
	}
	byID := make(map[uuid.UUID]dbq.Issue, len(loaded))
	for _, i := range loaded {
		byID[i.ID] = i
	}
	if out.lookups, err = s.loadLookups(ctx, loaded); err != nil {
		return out, err
	}
	users, err := s.Q.GetUsersByIDs(ctx, userIDs)
	if err != nil {
		return out, err
	}
	userByID := make(map[uuid.UUID]dbq.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	for _, author := range order {
		person := people[author]
		if u, ok := userByID[author]; ok {
			person.User = &u
		}
		for _, ti := range issues[author] {
			ti.Issue = byID[ti.Issue.ID]
			person.Issues = append(person.Issues, *ti)
		}
		slices.SortFunc(person.Issues, func(a, b TimesheetIssue) int {
			if a.Total != b.Total {
				return cmp.Compare(b.Total, a.Total)
			}
			return strings.Compare(issueKey(out.lookups.projects[a.Issue.ProjectID], a.Issue), issueKey(out.lookups.projects[b.Issue.ProjectID], b.Issue))
		})
		out.People = append(out.People, *person)
	}
	name := func(p TimesheetPerson) string {
		if p.User == nil {
			return "￿"
		}
		return strings.ToLower(p.User.DisplayName)
	}
	slices.SortStableFunc(out.People, func(a, b TimesheetPerson) int {
		if a.Total != b.Total {
			return cmp.Compare(b.Total, a.Total)
		}
		return strings.Compare(name(a), name(b))
	})
	return out, nil
}

// checkTimeZone asks PostgreSQL, whose zone database computes the days, whether it knows tz.
func (s *Service) checkTimeZone(ctx context.Context, tz string) error {
	var ok bool
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_timezone_names WHERE name = $1) OR upper($1) = 'UTC'", tz).Scan(&ok); err != nil {
		if db.IsNoRows(err) {
			ok = false
		} else {
			return err
		}
	}
	if !ok {
		return apperr.Invalid("tz", "unknown time zone %q (use an IANA name such as Europe/London)", tz)
	}
	return nil
}

// remainingAt is an issue's remaining estimate (seconds, 0 when unset) at time t, replayed
// from its remainingEstimate history, whose refs carry seconds.
func remainingAt(current *int64, hist []historyRow, t time.Time) int64 {
	var changes []historyRow
	for _, h := range hist {
		if h.Field == "remainingEstimate" {
			changes = append(changes, h)
		}
	}
	if len(changes) == 0 {
		return deref64(current)
	}
	v := parseSeconds(changes[0].FromRef)
	for _, h := range changes {
		if h.CreatedAt.After(t) {
			break
		}
		v = parseSeconds(h.ToRef)
	}
	return v
}

func parseSeconds(ref *string) int64 {
	if ref == nil {
		return 0
	}
	n, _ := strconv.ParseInt(*ref, 10, 64)
	return n
}

func deref64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
