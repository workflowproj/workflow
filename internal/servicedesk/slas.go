package servicedesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/search"
	"github.com/workflowproj/workflow/internal/sla"
	"github.com/workflowproj/workflow/internal/tracker"
)

// hoursJSON is how calendar hours are stored: [{day, start, end}] with "09:00" times.
type hoursJSON struct {
	Day   int    `json:"day"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// calendar builds a stored calendar (a broken one counts as around the clock, which never
// hides a breach).
func calendar(c dbq.SlaCalendar) *sla.Calendar {
	var hs []hoursJSON
	_ = json.Unmarshal(c.Hours, &hs)
	var hours []sla.Interval
	for _, h := range hs {
		start, err1 := sla.ParseClock(h.Start)
		end, err2 := sla.ParseClock(h.End)
		if err1 == nil && err2 == nil {
			hours = append(hours, sla.Interval{Day: time.Weekday(h.Day), Start: start, End: end})
		}
	}
	var holidays []string
	for _, d := range c.Holidays {
		holidays = append(holidays, d.Format(time.DateOnly))
	}
	cal, err := sla.New(c.TimeZone, hours, holidays)
	if err != nil {
		return sla.AroundTheClock
	}
	return cal
}

// slaConfig is a project's SLA configuration, loaded for computing clocks.
type slaConfig struct {
	metrics   map[sla.Metric]dbq.SlaMetric
	goals     map[sla.Metric][]dbq.SlaGoal
	calendars map[uuid.UUID]*sla.Calendar
	names     map[uuid.UUID]string
}

func loadSLAConfig(ctx context.Context, q *dbq.Queries, projectID uuid.UUID) (*slaConfig, error) {
	c := &slaConfig{metrics: map[sla.Metric]dbq.SlaMetric{}, goals: map[sla.Metric][]dbq.SlaGoal{}, calendars: map[uuid.UUID]*sla.Calendar{}, names: map[uuid.UUID]string{}}
	ms, err := q.ListSLAMetrics(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		c.metrics[sla.Metric(m.Metric)] = m
	}
	gs, err := q.ListSLAGoals(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		c.goals[sla.Metric(g.Metric)] = append(c.goals[sla.Metric(g.Metric)], g)
	}
	cals, err := q.ListSLACalendars(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, cal := range cals {
		c.calendars[cal.ID] = calendar(cal)
		c.names[cal.ID] = cal.Name
	}
	return c, nil
}

func (c *slaConfig) calendar(id *uuid.UUID) *sla.Calendar {
	if id == nil {
		return sla.AroundTheClock
	}
	if cal, ok := c.calendars[*id]; ok {
		return cal
	}
	return sla.AroundTheClock
}

// Calendars lists every SLA calendar for JQL's remaining() (tracker.SLACalendars).
func (s *Service) Calendars(ctx context.Context, q *dbq.Queries) ([]search.SLACalendar, error) {
	rows, err := q.AllSLACalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]search.SLACalendar, 0, len(rows))
	for _, r := range rows {
		cal := calendar(r)
		out = append(out, search.SLACalendar{ID: r.ID, Deadline: cal.Add})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Computing a request's clocks

// timeline reads what happened to a request: its status changes and the first reply to the
// customer.
func timeline(ctx context.Context, q *dbq.Queries, i dbq.Issue) (sla.Timeline, error) {
	t := sla.Timeline{Created: i.CreatedAt}
	rows, err := q.StatusChanges(ctx, i.ID)
	if err != nil {
		return t, err
	}
	initial := i.StatusID
	if len(rows) > 0 && rows[0].FromRef != nil {
		if id, err := uuid.Parse(*rows[0].FromRef); err == nil {
			initial = id
		}
	}
	type change struct {
		at time.Time
		id uuid.UUID
	}
	var changes []change
	ids := []uuid.UUID{initial, i.StatusID}
	last := initial
	for _, r := range rows {
		if r.ToRef == nil {
			continue
		}
		id, err := uuid.Parse(*r.ToRef)
		if err != nil {
			continue
		}
		changes = append(changes, change{r.CreatedAt, id})
		ids = append(ids, id)
		last = id
	}
	// A status set without history (a workflow migration) still counts, from the last update.
	if last != i.StatusID {
		changes = append(changes, change{i.UpdatedAt, i.StatusID})
	}
	sts, err := q.GetStatusesByIDs(ctx, ids)
	if err != nil {
		return t, err
	}
	done := map[uuid.UUID]bool{}
	for _, st := range sts {
		done[st.ID] = st.Category == "done"
	}
	t.Initial = sla.StatusChange{At: i.CreatedAt, StatusID: initial, Done: done[initial]}
	for _, c := range changes {
		t.Changes = append(t.Changes, sla.StatusChange{At: c.at, StatusID: c.id, Done: done[c.id]})
	}
	if at, err := q.FirstPublicReply(ctx, i.ID); err == nil {
		t.FirstReply = &at
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return t, err
	}
	return t, nil
}

// pickGoal returns the first goal whose conditions match the request.
func (s *Service) pickGoal(ctx context.Context, tx pgx.Tx, i dbq.Issue, requestType *uuid.UUID, goals []dbq.SlaGoal) (*dbq.SlaGoal, error) {
	for k := range goals {
		g := goals[k]
		if len(g.Priorities) > 0 && !slices.Contains(g.Priorities, i.Priority) {
			continue
		}
		if len(g.RequestTypeIds) > 0 && (requestType == nil || !slices.Contains(g.RequestTypeIds, *requestType)) {
			continue
		}
		if strings.TrimSpace(g.Jql) != "" {
			ok, err := s.Tracker.MatchesTx(ctx, tx, i.ProjectID, i.ID, g.Jql)
			if err != nil {
				s.Log.Warn("SLA goal query failed", "goal", g.ID, "error", err)
				continue
			}
			if !ok {
				continue
			}
		}
		return &g, nil
	}
	return nil, nil
}

// recompute works out a request's SLA clocks from its history and stores them, reading through
// q (inside the transaction that changed the request).
func (s *Service) recompute(ctx context.Context, tx pgx.Tx, q *dbq.Queries, issueID uuid.UUID) error {
	i, err := q.GetIssue(ctx, issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	cfg, err := loadSLAConfig(ctx, q, i.ProjectID)
	if err != nil {
		return err
	}
	var requestType *uuid.UUID
	if sr, err := q.GetServiceRequest(ctx, i.ID); err == nil {
		requestType = sr.RequestTypeID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var tl *sla.Timeline
	now := s.Now()
	for _, m := range sla.Metrics {
		mc, ok := cfg.metrics[m]
		var goal *dbq.SlaGoal
		if ok {
			if goal, err = s.pickGoal(ctx, tx, i, requestType, cfg.goals[m]); err != nil {
				return err
			}
		}
		if goal == nil {
			if err := q.DeleteIssueSLA(ctx, dbq.DeleteIssueSLAParams{IssueID: i.ID, Metric: string(m)}); err != nil {
				return err
			}
			continue
		}
		if tl == nil {
			t, err := timeline(ctx, q, i)
			if err != nil {
				return err
			}
			tl = &t
		}
		pause := map[uuid.UUID]bool{}
		for _, id := range mc.PauseStatusIds {
			pause[id] = true
		}
		cal := cfg.calendar(goal.CalendarID)
		c := sla.Compute(m, *tl, sla.Goal{Target: time.Duration(goal.TargetMinutes) * time.Minute, Calendar: cal, Pause: pause}, now)
		breachAt := c.BreachAt
		if breachAt != nil && breachAt.IsZero() {
			breachAt = nil // the calendar has no working time left
		}
		calID := goal.CalendarID
		if calID != nil && cfg.calendars[*calID] == nil {
			calID = nil
		}
		if err := q.UpsertIssueSLA(ctx, dbq.UpsertIssueSLAParams{
			IssueID: i.ID, Metric: string(m), ProjectID: i.ProjectID, GoalID: &goal.ID, CalendarID: calID,
			TargetSeconds: int64(c.Target / time.Second), State: string(c.State), ElapsedSeconds: int64(c.Elapsed / time.Second),
			CycleStartedAt: c.CycleStart, StoppedAt: c.StoppedAt, BreachAt: breachAt, Breached: c.Breached(now),
			BreachedAt: c.BreachedAt, ComputedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RecomputeProject recomputes the clocks of a project's open requests (jobs.SLARecompute).
func (s *Service) RecomputeProject(ctx context.Context, j jobs.SLARecompute) error {
	ids, err := s.Q.OpenRequestIDs(ctx, j.ProjectID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error { return s.recompute(ctx, tx, q, id) }); err != nil {
			return err
		}
	}
	return nil
}

// CheckBreaches marks running clocks that went past their goal and raises sla.breached for each,
// which automation rules and the assignee's notifications follow (jobs.SLACheck, every minute).
func (s *Service) CheckBreaches(ctx context.Context) error {
	now := s.Now()
	due, err := s.Q.DueSLABreaches(ctx, dbq.DueSLABreachesParams{Now: &now, Lim: 500})
	if err != nil {
		return err
	}
	for _, d := range due {
		err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
			i, err := q.GetIssueForUpdate(ctx, d.IssueID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			} else if err != nil {
				return err
			}
			// Recompute first: the clock may have paused or its goal changed meanwhile.
			if err := s.recompute(ctx, tx, q, i.ID); err != nil {
				return err
			}
			rows, err := q.IssueSLAsFor(ctx, []uuid.UUID{i.ID})
			if err != nil {
				return err
			}
			k := slices.IndexFunc(rows, func(r dbq.IssueSla) bool { return r.Metric == d.Metric })
			if k < 0 {
				return nil
			}
			r := rows[k]
			if r.State != string(sla.Running) || r.BreachNotifiedAt != nil || r.BreachAt == nil || !r.BreachAt.Before(s.Now()) {
				return nil
			}
			if err := q.MarkSLABreachNotified(ctx, dbq.MarkSLABreachNotifiedParams{IssueID: i.ID, Metric: r.Metric}); err != nil {
				return err
			}
			p, err := q.GetProject(ctx, i.ProjectID)
			if err != nil {
				return err
			}
			name := sla.Metric(r.Metric).Name()
			if m, err := q.ListSLAMetrics(ctx, p.ID); err == nil {
				for _, x := range m {
					if x.Metric == r.Metric && x.Name != "" {
						name = x.Name
					}
				}
			}
			breachedAt := r.BreachAt
			if r.BreachedAt != nil {
				breachedAt = r.BreachedAt // a clock that resumed over its target measures from a later moment
			}
			return s.Tracker.Emit(ctx, tx, authz.SystemActor(), p, &i, "sla.breached", map[string]any{"metric": r.Metric, "sla": name, "breachedAt": breachedAt})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Configuration

// slaDefaults are a new service project's SLAs: business hours, priority-based goals, paused
// while waiting for the customer or an approval.
func (s *Service) seedSLAs(ctx context.Context, q *dbq.Queries, p dbq.Project) error {
	hours := []hoursJSON{}
	for d := 1; d <= 5; d++ {
		hours = append(hours, hoursJSON{Day: d, Start: "09:00", End: "17:00"})
	}
	raw, _ := json.Marshal(hours)
	cal, err := q.UpsertSLACalendar(ctx, dbq.UpsertSLACalendarParams{ProjectID: p.ID, Name: "Business hours", TimeZone: "UTC", Hours: raw, Holidays: []time.Time{}})
	if err != nil {
		return err
	}
	var pause []uuid.UUID
	for _, name := range []string{statusWaitingForCustomer, statusPendingApproval} {
		if st, err := q.GetStatusByName(ctx, name); err == nil {
			pause = append(pause, st.ID)
		}
	}
	goals := map[sla.Metric][]dbq.CreateSLAGoalParams{
		sla.FirstResponse: {
			{Priorities: []string{"highest", "high"}, TargetMinutes: 60},
			{TargetMinutes: 4 * 60},
		},
		sla.Resolution: {
			{Priorities: []string{"highest"}, TargetMinutes: 4 * 60},
			{Priorities: []string{"high"}, TargetMinutes: 8 * 60},
			{TargetMinutes: 5 * 8 * 60},
		},
	}
	for _, m := range sla.Metrics {
		if err := q.UpsertSLAMetric(ctx, dbq.UpsertSLAMetricParams{ProjectID: p.ID, Metric: string(m), Name: m.Name(), PauseStatusIds: pause}); err != nil {
			return err
		}
		for k, g := range goals[m] {
			g.ProjectID, g.Metric, g.Position, g.CalendarID = p.ID, string(m), int32(k), &cal.ID
			if g.Priorities == nil {
				g.Priorities = []string{}
			}
			g.RequestTypeIds = []uuid.UUID{}
			if _, err := q.CreateSLAGoal(ctx, g); err != nil {
				return err
			}
		}
	}
	return nil
}

// SLAConfig returns a desk's calendars and SLA goals (agents).
func (s *Service) SLAConfig(ctx context.Context, actor *authz.Actor, ref string) (api.SlaConfig, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return api.SlaConfig{}, err
	}
	return s.slaConfigView(ctx, s.Q, p)
}

func (s *Service) slaConfigView(ctx context.Context, q *dbq.Queries, p dbq.Project) (api.SlaConfig, error) {
	out := api.SlaConfig{Calendars: []api.SlaCalendar{}, Metrics: []api.SlaMetricConfig{}}
	cals, err := q.ListSLACalendars(ctx, p.ID)
	if err != nil {
		return out, err
	}
	names := map[uuid.UUID]string{}
	for _, c := range cals {
		names[c.ID] = c.Name
		var hs []hoursJSON
		_ = json.Unmarshal(c.Hours, &hs)
		v := api.SlaCalendar{Id: &c.ID, Name: c.Name, TimeZone: c.TimeZone, Hours: []api.SlaHours{}, Holidays: []string{}}
		for _, h := range hs {
			v.Hours = append(v.Hours, api.SlaHours{Day: h.Day, Start: h.Start, End: h.End})
		}
		for _, d := range c.Holidays {
			v.Holidays = append(v.Holidays, d.Format(time.DateOnly))
		}
		out.Calendars = append(out.Calendars, v)
	}
	cfg, err := loadSLAConfig(ctx, q, p.ID)
	if err != nil {
		return out, err
	}
	var statusIDs []uuid.UUID
	for _, m := range cfg.metrics {
		statusIDs = append(statusIDs, m.PauseStatusIds...)
	}
	sts, err := q.GetStatusesByIDs(ctx, statusIDs)
	if err != nil {
		return out, err
	}
	statusName := map[uuid.UUID]string{}
	for _, st := range sts {
		statusName[st.ID] = st.Name
	}
	for _, m := range sla.Metrics {
		mc, ok := cfg.metrics[m]
		if !ok {
			continue
		}
		v := api.SlaMetricConfig{Metric: api.SlaMetric(m), Name: mc.Name, PauseStatuses: []string{}, Goals: []api.SlaGoal{}}
		for _, id := range mc.PauseStatusIds {
			if n, ok := statusName[id]; ok {
				v.PauseStatuses = append(v.PauseStatuses, n)
			}
		}
		for _, g := range cfg.goals[m] {
			gv := api.SlaGoal{TargetMinutes: int(g.TargetMinutes)}
			if len(g.Priorities) > 0 {
				ps := make([]api.Priority, len(g.Priorities))
				for k, pr := range g.Priorities {
					ps[k] = api.Priority(pr)
				}
				gv.Priorities = &ps
			}
			if len(g.RequestTypeIds) > 0 {
				ids := g.RequestTypeIds
				gv.RequestTypeIds = &ids
			}
			if g.Jql != "" {
				gv.Jql = ptr(g.Jql)
			}
			if g.CalendarID != nil {
				if n, ok := names[*g.CalendarID]; ok {
					gv.Calendar = ptr(n)
				}
			}
			v.Goals = append(v.Goals, gv)
		}
		out.Metrics = append(out.Metrics, v)
	}
	return out, nil
}

// UpdateSLAConfig replaces a desk's calendars (matched by name) and goals, and recomputes its
// open requests' clocks in the background (project admins).
func (s *Service) UpdateSLAConfig(ctx context.Context, actor *authz.Actor, ref string, in api.SlaConfig) (api.SlaConfig, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return api.SlaConfig{}, err
	}
	// Validate everything before writing anything.
	type calIn struct {
		name, zone string
		hours      []hoursJSON
		holidays   []time.Time
	}
	var cals []calIn
	seenCal := map[string]bool{}
	for k, c := range in.Calendars {
		where := fmt.Sprintf("calendars[%d]", k)
		name := strings.TrimSpace(c.Name)
		if name == "" || len(name) > 80 {
			return api.SlaConfig{}, apperr.Invalid(where+".name", "a calendar needs a name of at most 80 characters")
		}
		if seenCal[strings.ToLower(name)] {
			return api.SlaConfig{}, apperr.Invalid(where+".name", "two calendars are called %q", name)
		}
		seenCal[strings.ToLower(name)] = true
		var intervals []sla.Interval
		hours := []hoursJSON{}
		for _, h := range c.Hours {
			start, err := sla.ParseClock(h.Start)
			if err != nil {
				return api.SlaConfig{}, apperr.Invalid(where+".hours", "%v", err)
			}
			end, err := sla.ParseClock(h.End)
			if err != nil {
				return api.SlaConfig{}, apperr.Invalid(where+".hours", "%v", err)
			}
			intervals = append(intervals, sla.Interval{Day: time.Weekday(h.Day), Start: start, End: end})
			hours = append(hours, hoursJSON{Day: h.Day, Start: sla.FormatClock(start), End: sla.FormatClock(end)})
		}
		if _, err := sla.New(c.TimeZone, intervals, c.Holidays); err != nil {
			return api.SlaConfig{}, apperr.Invalid(where, "%s: %v", name, err)
		}
		holidays := []time.Time{}
		for _, h := range c.Holidays {
			d, _ := time.Parse(time.DateOnly, strings.TrimSpace(h))
			holidays = append(holidays, d)
		}
		cals = append(cals, calIn{name: name, zone: strings.TrimSpace(c.TimeZone), hours: hours, holidays: holidays})
	}
	types, err := s.Q.ListRequestTypes(ctx, p.ID)
	if err != nil {
		return api.SlaConfig{}, err
	}
	seenMetric := map[api.SlaMetric]bool{}
	for k, m := range in.Metrics {
		where := fmt.Sprintf("metrics[%d]", k)
		if !sla.Metric(m.Metric).Valid() {
			return api.SlaConfig{}, apperr.Invalid(where+".metric", "metric is first_response or resolution")
		}
		if seenMetric[m.Metric] {
			return api.SlaConfig{}, apperr.Invalid(where+".metric", "%s is configured twice", m.Metric)
		}
		seenMetric[m.Metric] = true
		for _, name := range m.PauseStatuses {
			if _, err := s.Q.GetStatusByName(ctx, name); err != nil {
				return api.SlaConfig{}, apperr.Invalid(where+".pauseStatuses", "unknown status %q", name)
			}
		}
		for j, g := range m.Goals {
			gw := fmt.Sprintf("%s.goals[%d]", where, j)
			if g.TargetMinutes < 1 || g.TargetMinutes > 5256000 {
				return api.SlaConfig{}, apperr.Invalid(gw+".targetMinutes", "targets are 1 minute to 10 years")
			}
			for _, pr := range deref(g.Priorities) {
				if !pr.Valid() {
					return api.SlaConfig{}, apperr.Invalid(gw+".priorities", "unknown priority %q", pr)
				}
			}
			for _, id := range deref(g.RequestTypeIds) {
				if !slices.ContainsFunc(types, func(rt dbq.RequestType) bool { return rt.ID == id }) {
					return api.SlaConfig{}, apperr.Invalid(gw+".requestTypeIds", "request type %s is not in %s", id, p.Key)
				}
			}
			if j := strings.TrimSpace(deref(g.Jql)); j != "" {
				if err := s.Tracker.ValidateJQL(ctx, actor, j); err != nil {
					return api.SlaConfig{}, err
				}
			}
			if c := strings.TrimSpace(deref(g.Calendar)); c != "" && !seenCal[strings.ToLower(c)] {
				return api.SlaConfig{}, apperr.Invalid(gw+".calendar", "no calendar called %q", c)
			}
		}
	}
	var out api.SlaConfig
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		calID := map[string]uuid.UUID{}
		keep := []uuid.UUID{}
		for _, c := range cals {
			raw, _ := json.Marshal(c.hours)
			row, err := q.UpsertSLACalendar(ctx, dbq.UpsertSLACalendarParams{ProjectID: p.ID, Name: c.name, TimeZone: c.zone, Hours: raw, Holidays: c.holidays})
			if err != nil {
				return err
			}
			calID[strings.ToLower(c.name)] = row.ID
			keep = append(keep, row.ID)
		}
		if err := q.DeleteSLAGoals(ctx, p.ID); err != nil {
			return err
		}
		if err := q.DeleteSLACalendarsExcept(ctx, dbq.DeleteSLACalendarsExceptParams{ProjectID: p.ID, Keep: keep}); err != nil {
			return err
		}
		if err := q.DeleteSLAMetrics(ctx, p.ID); err != nil {
			return err
		}
		for _, m := range in.Metrics {
			var pause []uuid.UUID
			for _, name := range m.PauseStatuses {
				st, err := q.GetStatusByName(ctx, name)
				if err != nil {
					return err
				}
				pause = append(pause, st.ID)
			}
			if pause == nil {
				pause = []uuid.UUID{}
			}
			name := strings.TrimSpace(m.Name)
			if name == "" {
				name = sla.Metric(m.Metric).Name()
			}
			if err := q.UpsertSLAMetric(ctx, dbq.UpsertSLAMetricParams{ProjectID: p.ID, Metric: string(m.Metric), Name: name, PauseStatusIds: pause}); err != nil {
				return err
			}
			for j, g := range m.Goals {
				params := dbq.CreateSLAGoalParams{
					ProjectID: p.ID, Metric: string(m.Metric), Position: int32(j), Priorities: []string{}, RequestTypeIds: []uuid.UUID{},
					Jql: strings.TrimSpace(deref(g.Jql)), TargetMinutes: int32(g.TargetMinutes),
				}
				for _, pr := range deref(g.Priorities) {
					params.Priorities = append(params.Priorities, string(pr))
				}
				params.RequestTypeIds = append(params.RequestTypeIds, deref(g.RequestTypeIds)...)
				if c := strings.TrimSpace(deref(g.Calendar)); c != "" {
					id := calID[strings.ToLower(c)]
					params.CalendarID = &id
				}
				if _, err := q.CreateSLAGoal(ctx, params); err != nil {
					return err
				}
			}
		}
		if _, err := s.Jobs.InsertTx(ctx, tx, jobs.SLARecompute{ProjectID: p.ID}, nil); err != nil {
			return err
		}
		tracker.Audit(ctx, q, actor, "service_desk.slas", "project", p.ID.String(), &p.ID, map[string]any{"calendars": len(cals), "metrics": len(in.Metrics)})
		var err error
		out, err = s.slaConfigView(ctx, q, p)
		return err
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Views

// slaViews renders stored clocks as of now.
func (s *Service) slaViews(ctx context.Context, rows []dbq.IssueSla) []api.Sla {
	out := []api.Sla{}
	if len(rows) == 0 {
		return out
	}
	cfgs := map[uuid.UUID]*slaConfig{}
	now := s.Now()
	for _, r := range rows {
		cfg, ok := cfgs[r.ProjectID]
		if !ok {
			var err error
			if cfg, err = loadSLAConfig(ctx, s.Q, r.ProjectID); err != nil {
				s.Log.Warn("SLA configuration", "project", r.ProjectID, "error", err)
				cfg = &slaConfig{calendars: map[uuid.UUID]*sla.Calendar{}, names: map[uuid.UUID]string{}, metrics: map[sla.Metric]dbq.SlaMetric{}}
			}
			cfgs[r.ProjectID] = cfg
		}
		out = append(out, slaView(r, cfg, now))
	}
	return out
}

func slaView(r dbq.IssueSla, cfg *slaConfig, now time.Time) api.Sla {
	cal := cfg.calendar(r.CalendarID)
	c := sla.Clock{
		State: sla.State(r.State), Elapsed: time.Duration(r.ElapsedSeconds) * time.Second, Target: time.Duration(r.TargetSeconds) * time.Second,
		CycleStart: r.CycleStartedAt, StoppedAt: r.StoppedAt, BreachAt: r.BreachAt, BreachedAt: r.BreachedAt,
	}
	name := sla.Metric(r.Metric).Name()
	if m, ok := cfg.metrics[sla.Metric(r.Metric)]; ok && m.Name != "" {
		name = m.Name
	}
	v := api.Sla{
		Metric: api.SlaMetric(r.Metric), Name: name, State: api.SlaState(r.State), TargetSeconds: r.TargetSeconds,
		ElapsedSeconds: int64(c.ElapsedAt(now, cal) / time.Second), RemainingSeconds: int64(c.Remaining(now, cal) / time.Second),
		Breached: c.Breached(now), BreachAt: r.BreachAt, StoppedAt: r.StoppedAt,
	}
	if r.CalendarID != nil {
		if n, ok := cfg.names[*r.CalendarID]; ok {
			v.Calendar = &n
		}
	}
	return v
}
