package search

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceDeskFields(t *testing.T) {
	now := time.Date(2026, 6, 5, 16, 0, 0, 0, time.UTC) // a Friday
	cal := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	calls := 0
	env := Env{Now: now, SLACalendars: func() ([]SLACalendar, error) {
		calls++
		// A calendar whose working time resumes on Monday: 2h from Friday 16:00 is Monday 10:00.
		return []SLACalendar{{ID: cal, Deadline: func(from time.Time, d time.Duration) time.Time {
			return time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
		}}}, nil
	}}
	cases := map[string][]string{
		`"Time to resolution" = breached()`:          {"issue_slas s", "s.metric = 'resolution'", "s.breached OR (s.state = 'running' AND s.breach_at < $2::timestamptz)"},
		`timeToFirstResponse = running()`:            {"s.metric = 'first_response'", "s.state = 'running'"},
		`"Time to first response" != paused()`:       {"NOT coalesce((EXISTS", "s.state = 'paused'"},
		`"time to resolution" in (completed())`:      {"s.state = 'stopped'"},
		`"Time to resolution" is EMPTY`:              {"NOT EXISTS (SELECT 1 FROM issue_slas s WHERE s.issue_id = i.id AND s.metric = 'resolution')"},
		`"Request type" = "Get IT help"`:             {"request_types rt", "lower(rt.name) = lower($2::text)"},
		`requestType is EMPTY`:                       {"NOT EXISTS (SELECT 1 FROM service_requests sr", "request_type_id IS NOT NULL"},
		`approval = pending()`:                       {"request_approvals ra", "ra.status = $2::text"},
		`approval = pendingBy(currentUser())`:        {"aa.decision IS NULL", "aa.user_id IN ($2::uuid)"},
		`approval is EMPTY`:                          {"NOT EXISTS (SELECT 1 FROM request_approvals"},
		`participant = currentUser()`:                {"request_participants rp", "rp.user_id IN ($2::uuid)"},
		`project = SD ORDER BY "Time to resolution"`: {"i.project_id IN"},
	}
	uid := uuid.New()
	env.UserID = &uid
	for src, wants := range cases {
		c := mustCompile(t, src, env)
		for _, w := range wants {
			if !strings.Contains(c.Where, w) {
				t.Errorf("%s:\n  %s\nmissing %q", src, c.Where, w)
			}
		}
	}
	if calls != 0 {
		t.Fatal("calendars are only loaded for remaining()")
	}

	// remaining(): paused clocks compare their time left, running clocks their breach time with
	// each calendar's deadline (and now + d around the clock).
	c := mustCompile(t, `"Time to resolution" < remaining("2h")`, env)
	for _, w := range []string{
		"s.state = 'paused' AND s.target_seconds - s.elapsed_seconds < ",
		"s.calendar_id IS NULL AND s.breach_at < ",
		"s.calendar_id = ",
	} {
		if !strings.Contains(c.Where, w) {
			t.Errorf("remaining: %s\nmissing %q", c.Where, w)
		}
	}
	var sawSecs, sawClock, sawCal, sawID bool
	for _, a := range c.Args {
		switch v := a.(type) {
		case int64:
			sawSecs = sawSecs || v == 7200
		case time.Time:
			sawClock = sawClock || v.Equal(now.Add(2*time.Hour))
			sawCal = sawCal || v.Equal(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
		case uuid.UUID:
			sawID = sawID || v == cal
		}
	}
	if !sawSecs || !sawClock || !sawCal || !sawID || calls != 1 {
		t.Fatalf("remaining args %v (calls %d)", c.Args, calls)
	}
	// A day is 24 hours here (calendar time), and a bare duration means remaining().
	c = mustCompile(t, `timeToFirstResponse >= 1d`, env)
	if !strings.Contains(c.Where, "s.target_seconds - s.elapsed_seconds >= ") || !containsArg(c.Args, int64(86400)) {
		t.Fatalf("bare duration: %s %v", c.Where, c.Args)
	}

	// Ordering by an SLA sorts by breach time.
	c = mustCompile(t, `project = SD ORDER BY "Time to resolution" ASC`, env)
	if !strings.HasPrefix(c.OrderBy, "(SELECT CASE s.state WHEN 'running' THEN s.breach_at") {
		t.Fatalf("order by: %s", c.OrderBy)
	}

	for _, bad := range []string{
		`"Time to resolution" = "late"`,
		`"Time to resolution" = overdue()`,
		`"Time to resolution" < remaining()`,
		`"Time to resolution" < remaining("soon")`,
		`"Time to resolution" < breached()`,
		`"Time to resolution" ~ "x"`,
		`approval = "yes"`,
		`approval = pendingBy()`,
		`approval = later()`,
	} {
		if _, err := CompileString(bad, env, 1, nil); err == nil {
			t.Errorf("%s should not compile", bad)
		}
	}
}

func containsArg(args []any, want any) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
