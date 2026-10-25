package sla

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
)

// weekdays9to5 is Monday to Friday, 09:00-17:00.
func weekdays9to5() []Interval {
	var out []Interval
	for d := time.Monday; d <= time.Friday; d++ {
		out = append(out, Interval{Day: d, Start: 9 * 60, End: 17 * 60})
	}
	return out
}

func mustCal(t *testing.T, zone string, hours []Interval, holidays ...string) *Calendar {
	t.Helper()
	c, err := New(zone, hours, holidays)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func at(t *testing.T, zone, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAroundTheClock(t *testing.T) {
	a := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if got := AroundTheClock.Working(a, a.Add(36*time.Hour)); got != 36*time.Hour {
		t.Fatalf("working = %s", got)
	}
	if got := AroundTheClock.Add(a, 90*time.Minute); !got.Equal(a.Add(90 * time.Minute)) {
		t.Fatalf("add = %s", got)
	}
	if got := AroundTheClock.Working(a, a.Add(-time.Hour)); got != 0 {
		t.Fatalf("backwards = %s", got)
	}
	var nilCal *Calendar
	if !nilCal.Always() || nilCal.Working(a, a.Add(time.Hour)) != time.Hour {
		t.Fatal("a nil calendar runs around the clock")
	}
}

func TestBusinessHoursWorking(t *testing.T) {
	const zone = "Europe/London"
	c := mustCal(t, zone, weekdays9to5())
	cases := []struct {
		from, to string
		want     time.Duration
	}{
		{"2026-06-01 10:00", "2026-06-01 12:30", 150 * time.Minute},  // Monday, inside hours
		{"2026-06-01 07:00", "2026-06-01 09:30", 30 * time.Minute},   // before opening
		{"2026-06-01 16:00", "2026-06-02 10:00", 2 * time.Hour},      // overnight
		{"2026-06-05 16:00", "2026-06-08 10:00", 2 * time.Hour},      // over the weekend
		{"2026-06-06 08:00", "2026-06-07 20:00", 0},                  // weekend only
		{"2026-06-01 00:00", "2026-06-08 00:00", 40 * time.Hour},     // a whole week
		{"2026-06-01 17:00", "2026-06-02 09:00", 0},                  // closed hours exactly
		{"2026-06-01 09:00", "2026-06-01 17:00", 8 * time.Hour},      // exactly one day
		{"2026-06-03 12:00", "2026-06-03 12:00", 0},                  // empty
		{"2026-06-01 09:00", "2026-07-01 09:00", 22 * 8 * time.Hour}, // June 2026 has 22 weekdays
		{"2026-05-29 16:30", "2026-06-01 09:15", 45 * time.Minute},   // Friday evening to Monday morning
		{"2026-06-01 12:00", "2026-06-01 11:00", 0},                  // backwards
		{"2026-01-05 09:00", "2026-01-12 09:00", 40 * time.Hour},     // winter week (GMT)
		{"2026-03-27 09:00", "2026-03-30 17:00", 16 * time.Hour},     // over the spring change (Sun 29 March)
		{"2026-10-23 09:00", "2026-10-26 17:00", 16 * time.Hour},     // over the autumn change (Sun 25 October)
		{"2026-03-29 00:00", "2026-03-30 00:00", 0},                  // the short Sunday itself
		{"2026-06-01 08:59", "2026-06-01 09:01", time.Minute},        // one minute in
		{"2026-06-01 16:59", "2026-06-01 17:01", time.Minute},        // one minute left
		{"2026-06-01 09:00", "2026-06-02 09:00", 8 * time.Hour},      // a day to the minute
		{"2026-06-02 09:00", "2026-06-01 09:00", 0},                  // reversed day
		{"2026-06-01 10:00", "2026-06-15 10:00", 10 * 8 * time.Hour}, // two weeks
		{"2026-06-01 10:00", "2026-06-01 10:00", 0},                  // instant
		{"2026-06-01 11:00", "2026-06-01 11:45", 45 * time.Minute},   // short span
		{"2026-06-01 17:30", "2026-06-01 23:00", 0},                  // evening
		{"2026-06-01 05:00", "2026-06-01 08:00", 0},                  // early morning
		{"2026-06-04 13:00", "2026-06-09 13:00", 3 * 8 * time.Hour},  // Thursday to Tuesday
		{"2026-06-07 23:00", "2026-06-08 09:30", 30 * time.Minute},   // Sunday night to Monday
		{"2026-06-01 09:00", "2026-06-01 09:00", 0},                  // opening instant
		{"2026-06-01 09:00", "2026-06-06 09:00", 40 * time.Hour},     // Monday to Saturday
		{"2026-06-01 13:00", "2026-06-03 13:00", 16 * time.Hour},     // two days mid-day
	}
	for _, tc := range cases {
		if got := c.Working(at(t, zone, tc.from), at(t, zone, tc.to)); got != tc.want {
			t.Errorf("Working(%s, %s) = %s, want %s", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestBusinessHoursAdd(t *testing.T) {
	const zone = "Europe/London"
	c := mustCal(t, zone, weekdays9to5())
	cases := []struct {
		from string
		d    time.Duration
		want string
	}{
		{"2026-06-01 10:00", 2 * time.Hour, "2026-06-01 12:00"},
		{"2026-06-01 16:00", 2 * time.Hour, "2026-06-02 10:00"},
		{"2026-06-05 16:00", 2 * time.Hour, "2026-06-08 10:00"},  // Friday → Monday
		{"2026-06-06 12:00", time.Hour, "2026-06-08 10:00"},      // from a Saturday
		{"2026-06-01 07:00", 0, "2026-06-01 09:00"},              // zero: the next working moment
		{"2026-06-01 10:00", 0, "2026-06-01 10:00"},              // zero inside hours
		{"2026-06-01 09:00", 8 * time.Hour, "2026-06-01 17:00"},  // ends exactly at closing
		{"2026-06-01 09:00", 40 * time.Hour, "2026-06-05 17:00"}, // one week
		{"2026-06-01 17:00", time.Minute, "2026-06-02 09:01"},
		{"2026-03-27 16:00", 2 * time.Hour, "2026-03-30 10:00"}, // over the spring change
		{"2026-10-23 16:00", 2 * time.Hour, "2026-10-26 10:00"}, // over the autumn change
	}
	for _, tc := range cases {
		got := c.Add(at(t, zone, tc.from), tc.d)
		if want := at(t, zone, tc.want); !got.Equal(want) {
			t.Errorf("Add(%s, %s) = %s, want %s", tc.from, tc.d, got.In(c.Location), want)
		}
	}
}

func TestHolidays(t *testing.T) {
	const zone = "Europe/London"
	c := mustCal(t, zone, weekdays9to5(), "2026-12-25", "2026-12-28")
	// Thursday 24 December 16:00 + 2h skips Christmas Day (Friday), the weekend and the Monday
	// holiday: Tuesday 29 December 10:00.
	got := c.Add(at(t, zone, "2026-12-24 16:00"), 2*time.Hour)
	if want := at(t, zone, "2026-12-29 10:00"); !got.Equal(want) {
		t.Fatalf("add over holidays = %s, want %s", got, want)
	}
	if w := c.Working(at(t, zone, "2026-12-21 09:00"), at(t, zone, "2027-01-04 09:00")); w != 8*8*time.Hour {
		t.Fatalf("working over the holidays = %s", w)
	}
	if c.Contains(at(t, zone, "2026-12-25 10:00")) || !c.Contains(at(t, zone, "2026-12-24 10:00")) {
		t.Fatal("contains on a holiday")
	}
}

func TestDaylightSaving(t *testing.T) {
	// Around the clock in a zone, the day the clocks change is 23 or 25 hours long.
	every := func(zone string) *Calendar {
		var h []Interval
		for d := time.Sunday; d <= time.Saturday; d++ {
			h = append(h, Interval{Day: d, Start: 0, End: 24 * 60})
		}
		return mustCal(t, zone, h)
	}
	ny := every("America/New_York")
	if w := ny.Working(at(t, "America/New_York", "2026-03-08 00:00"), at(t, "America/New_York", "2026-03-09 00:00")); w != 23*time.Hour {
		t.Fatalf("spring-forward day = %s", w)
	}
	if w := ny.Working(at(t, "America/New_York", "2026-11-01 00:00"), at(t, "America/New_York", "2026-11-02 00:00")); w != 25*time.Hour {
		t.Fatalf("fall-back day = %s", w)
	}
	// Adding 24 working hours on the spring-forward day lands an hour later on the clock.
	if got := ny.Add(at(t, "America/New_York", "2026-03-08 00:00"), 24*time.Hour); !got.Equal(at(t, "America/New_York", "2026-03-09 01:00")) {
		t.Fatalf("add over spring forward = %s", got)
	}

	// Business hours stay 09:00-17:00 local time on either side of a change: the UTC instants
	// move by an hour.
	c := mustCal(t, "America/New_York", weekdays9to5())
	fri := at(t, "America/New_York", "2026-03-06 16:00") // EST, UTC-5
	got := c.Add(fri, 2*time.Hour)                       // Monday 10:00 EDT, UTC-4
	if want := at(t, "America/New_York", "2026-03-09 10:00"); !got.Equal(want) {
		t.Fatalf("business hours over the change = %s, want %s", got, want)
	}
	if got.UTC().Hour() != 14 {
		t.Fatalf("Monday 10:00 EDT is 14:00 UTC, got %s", got.UTC())
	}
	// Same calendar seen from UTC: Monday 13:00-14:00 UTC is 09:00-10:00 EDT.
	if w := c.Working(time.Date(2026, 3, 9, 13, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 14, 0, 0, 0, time.UTC)); w != time.Hour {
		t.Fatalf("working seen from UTC = %s", w)
	}
	// Working hours that span the change (an overnight desk, 00:00-06:00 on Sundays).
	night := mustCal(t, "Europe/London", []Interval{{Day: time.Sunday, Start: 0, End: 6 * 60}})
	if w := night.Working(at(t, "Europe/London", "2026-03-28 12:00"), at(t, "Europe/London", "2026-03-30 00:00")); w != 5*time.Hour {
		t.Fatalf("00:00-06:00 on the spring Sunday is 5 hours, got %s", w)
	}
	if w := night.Working(at(t, "Europe/London", "2026-10-24 12:00"), at(t, "Europe/London", "2026-10-26 00:00")); w != 7*time.Hour {
		t.Fatalf("00:00-06:00 on the autumn Sunday is 7 hours, got %s", w)
	}
}

func TestSplitDaysAndOtherZones(t *testing.T) {
	// A lunch break, and a calendar in a zone far from UTC (dates are local).
	hours := []Interval{{Day: time.Monday, Start: 8 * 60, End: 12 * 60}, {Day: time.Monday, Start: 13 * 60, End: 17 * 60}}
	c := mustCal(t, "Pacific/Auckland", hours)
	if w := c.Working(at(t, "Pacific/Auckland", "2026-06-01 11:00"), at(t, "Pacific/Auckland", "2026-06-01 14:00")); w != 2*time.Hour {
		t.Fatalf("over lunch = %s", w)
	}
	if got := c.Add(at(t, "Pacific/Auckland", "2026-06-01 11:30"), time.Hour); !got.Equal(at(t, "Pacific/Auckland", "2026-06-01 13:30")) {
		t.Fatalf("add over lunch = %s", got)
	}
	// Monday 08:00 in Auckland is Sunday 20:00 UTC.
	if !c.Contains(time.Date(2026, 5, 31, 20, 30, 0, 0, time.UTC)) {
		t.Fatal("Monday morning in Auckland is Sunday evening in UTC")
	}
}

func TestAddAndWorkingAgree(t *testing.T) {
	// Property: Working(from, Add(from, d)) == d, and Add is the earliest such instant.
	for _, zone := range []string{"Europe/London", "America/New_York", "Australia/Sydney", "UTC"} {
		c := mustCal(t, zone, append(weekdays9to5(), Interval{Day: time.Saturday, Start: 10 * 60, End: 13 * 60}), "2026-12-25", "2026-04-03")
		rng := rand.New(rand.NewPCG(1, uint64(len(zone))))
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for range 400 {
			from := base.Add(time.Duration(rng.Int64N(int64(365 * 24 * time.Hour))))
			d := time.Duration(rng.Int64N(int64(80*time.Hour))) / time.Minute * time.Minute
			end := c.Add(from, d)
			if end.IsZero() {
				t.Fatalf("%s: no working time after %s", zone, from)
			}
			if got := c.Working(from, end); got != d {
				t.Fatalf("%s: Working(%s, Add(+%s) = %s) = %s", zone, from, d, end, got)
			}
			if d > 0 && c.Working(from, end.Add(-time.Minute)) >= d {
				t.Fatalf("%s: Add(%s, %s) = %s is not the earliest", zone, from, d, end)
			}
			// Working time is additive over any split point.
			mid := from.Add(time.Duration(rng.Int64N(int64(end.Sub(from)) + 1)))
			if c.Working(from, mid)+c.Working(mid, end) != d {
				t.Fatalf("%s: working is not additive at %s", zone, mid)
			}
		}
	}
}

func TestNewValidates(t *testing.T) {
	bad := []struct {
		zone     string
		hours    []Interval
		holidays []string
	}{
		{"Mars/Olympus", nil, nil},
		{"Local", nil, nil},
		{"", nil, nil},
		{"UTC", []Interval{{Day: time.Monday, Start: 600, End: 540}}, nil},
		{"UTC", []Interval{{Day: time.Monday, Start: 0, End: 1441}}, nil},
		{"UTC", []Interval{{Day: time.Monday, Start: 540, End: 720}, {Day: time.Monday, Start: 700, End: 800}}, nil},
		{"UTC", []Interval{{Day: 9, Start: 0, End: 60}}, nil},
		{"UTC", nil, []string{"25/12/2026"}},
	}
	for _, b := range bad {
		if _, err := New(b.zone, b.hours, b.holidays); err == nil {
			t.Errorf("New(%q, %v, %v) should fail", b.zone, b.hours, b.holidays)
		}
	}
	for s, want := range map[string]int{"09:00": 540, "00:00": 0, "24:00": 1440, "17:30": 1050} {
		if got, err := ParseClock(s); err != nil || got != want {
			t.Errorf("ParseClock(%q) = %d, %v", s, got, err)
		}
	}
	for _, s := range []string{"9", "9:0", "25:00", "24:01", "12:60", "ab:cd", "-1:00"} {
		if _, err := ParseClock(s); err == nil {
			t.Errorf("ParseClock(%q) should fail", s)
		}
	}
	if FormatClock(540) != "09:00" || FormatClock(1440) != "24:00" {
		t.Fatal("FormatClock")
	}
}

// ---------------------------------------------------------------------------
// The clock of one request

var (
	waitingSupport  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	inProgress      = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	waitingCustomer = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	pendingApproval = uuid.MustParse("00000000-0000-0000-0000-000000000004")
	resolved        = uuid.MustParse("00000000-0000-0000-0000-000000000005")
)

func st(id uuid.UUID, at time.Time) StatusChange {
	return StatusChange{At: at, StatusID: id, Done: id == resolved}
}

var pause = map[uuid.UUID]bool{waitingCustomer: true, pendingApproval: true}

func TestClockRunsAndBreaches(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tl := Timeline{Created: t0, Initial: st(waitingSupport, t0)}
	g := Goal{Target: 4 * time.Hour, Pause: pause}

	c := Compute(Resolution, tl, g, t0.Add(time.Hour))
	if c.State != Running || c.Elapsed != time.Hour || c.BreachAt == nil || !c.BreachAt.Equal(t0.Add(4*time.Hour)) {
		t.Fatalf("running: %+v", c)
	}
	if c.Breached(t0.Add(time.Hour)) || c.Remaining(t0.Add(time.Hour), nil) != 3*time.Hour {
		t.Fatalf("remaining: %s", c.Remaining(t0.Add(time.Hour), nil))
	}
	// The stored clock keeps telling the time: an hour later two hours remain.
	if r := c.Remaining(t0.Add(2*time.Hour), nil); r != 2*time.Hour {
		t.Fatalf("remaining later = %s", r)
	}
	if e := c.ElapsedAt(t0.Add(2*time.Hour), nil); e != 2*time.Hour {
		t.Fatalf("elapsed later = %s", e)
	}
	// Reaching the target exactly is not a breach; a minute past is.
	c = Compute(Resolution, tl, g, t0.Add(4*time.Hour))
	if c.Breached(t0.Add(4*time.Hour)) || c.BreachedAt != nil {
		t.Fatalf("exactly at target: %+v", c)
	}
	c = Compute(Resolution, tl, g, t0.Add(5*time.Hour))
	if !c.Breached(t0.Add(5*time.Hour)) || c.BreachedAt == nil || !c.BreachedAt.Equal(t0.Add(4*time.Hour)) {
		t.Fatalf("breached: %+v", c)
	}
	if r := c.Remaining(t0.Add(5*time.Hour), nil); r != -time.Hour {
		t.Fatalf("remaining after breach = %s", r)
	}
}

func TestClockPausesWhileWaiting(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tl := Timeline{Created: t0, Initial: st(waitingSupport, t0), Changes: []StatusChange{
		st(inProgress, t0.Add(30*time.Minute)),
		st(waitingCustomer, t0.Add(time.Hour)), // paused for 3 hours
		st(waitingSupport, t0.Add(4*time.Hour)),
	}}
	g := Goal{Target: 2 * time.Hour, Pause: pause}
	c := Compute(Resolution, tl, g, t0.Add(2*time.Hour))
	if c.State != Paused || c.Elapsed != time.Hour || c.BreachAt != nil {
		t.Fatalf("paused: %+v", c)
	}
	if c.Remaining(t0.Add(3*time.Hour), nil) != time.Hour {
		t.Fatal("a paused clock does not count down")
	}
	c = Compute(Resolution, tl, g, t0.Add(4*time.Hour+30*time.Minute))
	if c.State != Running || c.Elapsed != 90*time.Minute || !c.BreachAt.Equal(t0.Add(5*time.Hour)) {
		t.Fatalf("resumed: %+v", c)
	}
	// Created straight into a pause status (pending approval): nothing counts until it leaves.
	tl2 := Timeline{Created: t0, Initial: st(pendingApproval, t0), Changes: []StatusChange{st(waitingSupport, t0.Add(10*time.Hour))}}
	c = Compute(Resolution, tl2, g, t0.Add(11*time.Hour))
	if c.Elapsed != time.Hour || c.State != Running {
		t.Fatalf("approval wait: %+v", c)
	}
}

func TestFirstResponse(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	reply := t0.Add(45 * time.Minute)
	tl := Timeline{Created: t0, Initial: st(waitingSupport, t0), FirstReply: &reply}
	g := Goal{Target: time.Hour, Pause: pause}
	c := Compute(FirstResponse, tl, g, t0.Add(3*time.Hour))
	if c.State != Stopped || c.Elapsed != 45*time.Minute || c.Breached(t0.Add(3*time.Hour)) || !c.StoppedAt.Equal(reply) {
		t.Fatalf("met: %+v", c)
	}
	// The reply does not stop the resolution clock.
	if r := Compute(Resolution, tl, Goal{Target: 8 * time.Hour}, t0.Add(3*time.Hour)); r.State != Running || r.Elapsed != 3*time.Hour {
		t.Fatalf("resolution: %+v", r)
	}
	// A late reply is a breach.
	late := t0.Add(2 * time.Hour)
	tl.FirstReply = &late
	c = Compute(FirstResponse, tl, g, t0.Add(3*time.Hour))
	if c.State != Stopped || !c.Breached(t0.Add(3*time.Hour)) || c.BreachedAt == nil || !c.BreachedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("late: %+v", c)
	}
	// Resolved without a reply: the first response clock stops too, and a reopening does not
	// restart it.
	tl = Timeline{Created: t0, Initial: st(waitingSupport, t0), Changes: []StatusChange{st(resolved, t0.Add(20*time.Minute)), st(waitingSupport, t0.Add(time.Hour))}}
	c = Compute(FirstResponse, tl, g, t0.Add(5*time.Hour))
	if c.State != Stopped || c.Elapsed != 20*time.Minute {
		t.Fatalf("resolved without reply: %+v", c)
	}
	// A reply that has not happened yet (in the future of `now`) is ignored.
	future := t0.Add(10 * time.Hour)
	tl = Timeline{Created: t0, Initial: st(waitingSupport, t0), FirstReply: &future}
	if c := Compute(FirstResponse, tl, g, t0.Add(30*time.Minute)); c.State != Running {
		t.Fatalf("future reply: %+v", c)
	}
}

func TestResolutionCycles(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tl := Timeline{Created: t0, Initial: st(waitingSupport, t0), Changes: []StatusChange{
		st(resolved, t0.Add(5*time.Hour)), // breached (target 4h), stopped
		st(waitingSupport, t0.Add(6*time.Hour)),
	}}
	g := Goal{Target: 4 * time.Hour, Pause: pause}
	c := Compute(Resolution, tl, g, t0.Add(5*time.Hour+30*time.Minute))
	if c.State != Stopped || c.Elapsed != 5*time.Hour || !c.Breached(t0.Add(6*time.Hour)) {
		t.Fatalf("stopped late: %+v", c)
	}
	// Reopened: a new cycle starts with the full target.
	c = Compute(Resolution, tl, g, t0.Add(7*time.Hour))
	if c.State != Running || c.Elapsed != time.Hour || !c.CycleStart.Equal(t0.Add(6*time.Hour)) || c.BreachedAt != nil || c.Breached(t0.Add(7*time.Hour)) {
		t.Fatalf("new cycle: %+v", c)
	}
	// Created resolved: stopped at once.
	tl = Timeline{Created: t0, Initial: st(resolved, t0)}
	if c := Compute(Resolution, tl, g, t0.Add(time.Hour)); c.State != Stopped || c.Elapsed != 0 {
		t.Fatalf("created resolved: %+v", c)
	}
}

func TestBreachedClockResumesAfterAPause(t *testing.T) {
	// A 4-hour goal breaches on Monday at 13:00, waits for the customer from 14:00 to Tuesday
	// 09:00, then runs again: on Tuesday at 10:00 it is 2 working hours over (6 used), not 5.
	const zone = "Europe/London"
	cal := mustCal(t, zone, weekdays9to5())
	created := at(t, zone, "2026-06-08 09:00")
	tl := Timeline{Created: created, Initial: st(waitingSupport, created), Changes: []StatusChange{
		st(waitingCustomer, at(t, zone, "2026-06-08 14:00")),
		st(waitingSupport, at(t, zone, "2026-06-09 09:00")),
	}}
	g := Goal{Target: 4 * time.Hour, Calendar: cal, Pause: pause}
	now := at(t, zone, "2026-06-09 10:00")
	c := Compute(Resolution, tl, g, now)
	if c.State != Running || !c.Breached(now) || !c.BreachedAt.Equal(at(t, zone, "2026-06-08 13:00")) {
		t.Fatalf("clock: %+v", c)
	}
	if r := c.Remaining(now, cal); r != -2*time.Hour {
		t.Fatalf("remaining = %s, want -2h", r)
	}
	if e := c.ElapsedAt(now, cal); e != 6*time.Hour {
		t.Fatalf("elapsed = %s, want 6h", e)
	}
	// Later the same day the overrun keeps growing in working time only.
	if r := c.Remaining(at(t, zone, "2026-06-09 20:00"), cal); r != -9*time.Hour {
		t.Fatalf("evening remaining = %s, want -9h", r)
	}
	// Around the clock, the same story: 4h goal, 5h used before the pause, 1h after it.
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tl = Timeline{Created: t0, Initial: st(waitingSupport, t0), Changes: []StatusChange{
		st(waitingCustomer, t0.Add(5*time.Hour)),
		st(waitingSupport, t0.Add(20*time.Hour)),
	}}
	c = Compute(Resolution, tl, Goal{Target: 4 * time.Hour, Pause: pause}, t0.Add(21*time.Hour))
	if r := c.Remaining(t0.Add(21*time.Hour), nil); r != -2*time.Hour {
		t.Fatalf("around the clock: remaining = %s, want -2h", r)
	}
}

func TestSubUndoesAdd(t *testing.T) {
	for _, zone := range []string{"Europe/London", "America/New_York", "UTC"} {
		c := mustCal(t, zone, weekdays9to5(), "2026-12-25")
		rng := rand.New(rand.NewPCG(7, uint64(len(zone))))
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for range 300 {
			from := base.Add(time.Duration(rng.Int64N(int64(365 * 24 * time.Hour))))
			d := time.Duration(rng.Int64N(int64(60*time.Hour))) / time.Minute * time.Minute
			back := c.Sub(from, d)
			if got := c.Working(back, from); got != d {
				t.Fatalf("%s: Working(Sub(%s, %s) = %s, from) = %s", zone, from, d, back, got)
			}
		}
	}
}

func TestClockInBusinessHours(t *testing.T) {
	const zone = "Europe/London"
	cal := mustCal(t, zone, weekdays9to5())
	created := at(t, zone, "2026-06-05 15:00") // Friday afternoon
	tl := Timeline{Created: created, Initial: st(waitingSupport, created), Changes: []StatusChange{
		st(waitingCustomer, at(t, zone, "2026-06-05 16:00")), // an hour used, then paused
		st(waitingSupport, at(t, zone, "2026-06-08 08:00")),  // the customer answers before opening
	}}
	g := Goal{Target: 4 * time.Hour, Calendar: cal, Pause: pause}
	now := at(t, zone, "2026-06-08 10:30")
	c := Compute(Resolution, tl, g, now)
	if c.State != Running || c.Elapsed != 150*time.Minute {
		t.Fatalf("elapsed = %s (%+v)", c.Elapsed, c)
	}
	// 90 minutes left from 10:30 → 12:00.
	if want := at(t, zone, "2026-06-08 12:00"); !c.BreachAt.Equal(want) {
		t.Fatalf("breach at %s, want %s", c.BreachAt.In(cal.Location), want)
	}
	if r := c.Remaining(at(t, zone, "2026-06-08 11:00"), cal); r != time.Hour {
		t.Fatalf("remaining = %s", r)
	}
	// Monday evening, after the deadline: 5 working hours over (12:00-17:00), none overnight.
	late := at(t, zone, "2026-06-08 23:00")
	c = Compute(Resolution, tl, g, late)
	if !c.Breached(late) || c.Remaining(late, cal) != -5*time.Hour {
		t.Fatalf("late: remaining %s, %+v", c.Remaining(late, cal), c)
	}
	if c.ElapsedAt(late, cal) != 9*time.Hour {
		t.Fatalf("elapsed later = %s", c.ElapsedAt(late, cal))
	}
	// A request created on Saturday starts counting on Monday at 09:00.
	sat := at(t, zone, "2026-06-06 11:00")
	c = Compute(Resolution, Timeline{Created: sat, Initial: st(waitingSupport, sat)}, g, at(t, zone, "2026-06-07 20:00"))
	if c.Elapsed != 0 || !c.BreachAt.Equal(at(t, zone, "2026-06-08 13:00")) {
		t.Fatalf("weekend request: %+v", c)
	}
}

func TestClockAcrossDaylightSaving(t *testing.T) {
	const zone = "Europe/London"
	cal := mustCal(t, zone, weekdays9to5())
	// Created Friday 27 March 2026 16:00 GMT with an 8-hour goal; the clocks go forward on Sunday.
	created := at(t, zone, "2026-03-27 16:00")
	g := Goal{Target: 8 * time.Hour, Calendar: cal}
	c := Compute(Resolution, Timeline{Created: created, Initial: st(waitingSupport, created)}, g, at(t, zone, "2026-03-30 09:30"))
	if c.Elapsed != 90*time.Minute {
		t.Fatalf("elapsed = %s", c.Elapsed)
	}
	// 1 hour on Friday + 7 on Monday: Monday 16:00 BST, which is 15:00 UTC.
	if want := at(t, zone, "2026-03-30 16:00"); !c.BreachAt.Equal(want) || c.BreachAt.UTC().Hour() != 15 {
		t.Fatalf("breach at %s", c.BreachAt.UTC())
	}
	// Around the clock, the same goal is 8 elapsed hours regardless of the change.
	sat := at(t, zone, "2026-03-28 22:00")
	c = Compute(Resolution, Timeline{Created: sat, Initial: st(waitingSupport, sat)}, Goal{Target: 8 * time.Hour}, sat.Add(time.Hour))
	if !c.BreachAt.Equal(sat.Add(8*time.Hour)) || c.BreachAt.In(cal.Location).Hour() != 7 {
		t.Fatalf("around the clock over the change: %s", c.BreachAt.In(cal.Location))
	}
}

func TestEventsBeforeCreationAndAfterNow(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tl := Timeline{Created: t0, Initial: st(waitingSupport, t0), Changes: []StatusChange{
		st(inProgress, t0.Add(-time.Minute)), // clock skew: treated as at creation
		st(resolved, t0.Add(10*time.Hour)),   // not yet happened at now
	}}
	c := Compute(Resolution, tl, Goal{Target: 4 * time.Hour}, t0.Add(2*time.Hour))
	if c.State != Running || c.Elapsed != 2*time.Hour {
		t.Fatalf("%+v", c)
	}
}

func TestMetrics(t *testing.T) {
	if !FirstResponse.Valid() || !Resolution.Valid() || Metric("other").Valid() {
		t.Fatal("Valid")
	}
	if FirstResponse.Name() != "Time to first response" || Resolution.Name() != "Time to resolution" {
		t.Fatal("Name")
	}
}
