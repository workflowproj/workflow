// Package sla measures service-level agreements: the calendars clocks run in (around the clock,
// or working hours in a time zone with holidays off) and the clock of one request, computed from
// what happened to it (status changes, the first reply to the customer). Everything here is pure,
// so the clock is easy to test; the servicedesk package loads requests and stores the results.
package sla

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // calendars name IANA zones, also in minimal container images
)

// Interval is a stretch of working time on one weekday, in minutes after local midnight.
type Interval struct {
	Day   time.Weekday
	Start int // 0-1439
	End   int // after Start, at most 1440 (midnight at the end of the day)
}

// Calendar says when SLA clocks run.
type Calendar struct {
	// Location is the zone the hours and holidays are in.
	Location *time.Location
	// Hours are the working intervals of the week; none means around the clock.
	Hours []Interval
	// Holidays are whole days off (local dates, "2006-01-02").
	Holidays map[string]bool
}

// AroundTheClock is the 24×7 calendar.
var AroundTheClock = &Calendar{Location: time.UTC}

// Always reports whether the clock runs around the clock.
func (c *Calendar) Always() bool { return c == nil || len(c.Hours) == 0 }

func (c *Calendar) loc() *time.Location {
	if c == nil || c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// maxDays bounds the search for working time (a calendar whose only hours fall on holidays).
const maxDays = 366 * 10

// intervals returns the working intervals of a local date as instants, in order. Building them
// with time.Date (never by adding 24 hours) keeps them right across daylight-saving changes: a
// 09:00-17:00 day is 8 hours, and a 00:00-24:00 day is 23 or 25 hours when the clocks change.
func (c *Calendar) intervals(y int, m time.Month, d int) [][2]time.Time {
	loc := c.loc()
	day := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if c.Holidays[day.Format(time.DateOnly)] {
		return nil
	}
	var out [][2]time.Time
	for _, iv := range c.Hours {
		if iv.Day != day.Weekday() {
			continue
		}
		start := time.Date(y, m, d, iv.Start/60, iv.Start%60, 0, 0, loc)
		end := time.Date(y, m, d, iv.End/60, iv.End%60, 0, 0, loc)
		if end.After(start) {
			out = append(out, [2]time.Time{start, end})
		}
	}
	return out
}

// Working returns the working time between a and b (zero unless b is after a).
func (c *Calendar) Working(a, b time.Time) time.Duration {
	if !b.After(a) {
		return 0
	}
	if c.Always() {
		return b.Sub(a)
	}
	var total time.Duration
	la := a.In(c.loc())
	// From the day before a: a day's last interval may end after midnight in a short (DST) day.
	for i := -1; i <= maxDays; i++ {
		y, m, d := la.Date()
		day := time.Date(y, m, d+i, 0, 0, 0, 0, c.loc())
		if day.After(b) {
			break
		}
		for _, iv := range c.intervals(day.Date()) {
			from, to := maxTime(iv[0], a), minTime(iv[1], b)
			if to.After(from) {
				total += to.Sub(from)
			}
		}
	}
	return total
}

// Add returns the earliest instant by which d of working time has passed since from. When from
// is outside working hours the clock starts at the next working moment (so Add(t, 0) is the next
// working moment at or after t). The zero time means the calendar has no working time within ten
// years.
func (c *Calendar) Add(from time.Time, d time.Duration) time.Time {
	if c.Always() {
		return from.Add(d)
	}
	if d < 0 {
		return from
	}
	left := d
	lf := from.In(c.loc())
	for i := -1; i <= maxDays; i++ {
		y, m, dd := lf.Date()
		day := time.Date(y, m, dd+i, 0, 0, 0, 0, c.loc())
		for _, iv := range c.intervals(day.Date()) {
			start := maxTime(iv[0], from)
			if !iv[1].After(start) {
				continue
			}
			if avail := iv[1].Sub(start); avail >= left {
				return start.Add(left)
			} else {
				left -= avail
			}
		}
	}
	return time.Time{}
}

// Sub is the instant d of working time before from: Add, backwards. It is from itself for d <= 0.
func (c *Calendar) Sub(from time.Time, d time.Duration) time.Time {
	if c.Always() {
		return from.Add(-max(d, 0))
	}
	if d <= 0 {
		return from
	}
	left := d
	lf := from.In(c.loc())
	for i := 1; i >= -maxDays; i-- {
		y, m, dd := lf.Date()
		day := time.Date(y, m, dd+i, 0, 0, 0, 0, c.loc())
		ivs := c.intervals(day.Date())
		for k := len(ivs) - 1; k >= 0; k-- {
			end := minTime(ivs[k][1], from)
			if !end.After(ivs[k][0]) {
				continue
			}
			if avail := end.Sub(ivs[k][0]); avail >= left {
				return end.Add(-left)
			} else {
				left -= avail
			}
		}
	}
	return time.Time{}
}

// Contains reports whether t is working time.
func (c *Calendar) Contains(t time.Time) bool {
	if c.Always() {
		return true
	}
	lt := t.In(c.loc())
	for i := -1; i <= 0; i++ {
		y, m, d := lt.Date()
		day := time.Date(y, m, d+i, 0, 0, 0, 0, c.loc())
		for _, iv := range c.intervals(day.Date()) {
			if !t.Before(iv[0]) && t.Before(iv[1]) {
				return true
			}
		}
	}
	return false
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Parsing and validation (calendars are stored as text: "09:00", "2026-12-25")

// ParseClock reads "09:00" (or "24:00" for the end of the day) into minutes after midnight.
func ParseClock(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || len(m) != 2 || hh < 0 || mm < 0 || mm > 59 || hh > 24 || (hh == 24 && mm != 0) {
		return 0, fmt.Errorf("%q is not a time of day (use HH:MM, e.g. 09:00)", s)
	}
	return hh*60 + mm, nil
}

// FormatClock writes minutes after midnight as "09:00".
func FormatClock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

// New builds and validates a calendar: a time zone name, working intervals (none for around the
// clock) and holidays ("2006-01-02").
func New(zone string, hours []Interval, holidays []string) (*Calendar, error) {
	if strings.EqualFold(strings.TrimSpace(zone), "local") {
		return nil, fmt.Errorf("unknown time zone %q (use a name such as Europe/London)", zone)
	}
	loc, err := time.LoadLocation(strings.TrimSpace(zone))
	if err != nil || strings.TrimSpace(zone) == "" {
		return nil, fmt.Errorf("unknown time zone %q (use a name such as Europe/London)", zone)
	}
	sorted := slices.Clone(hours)
	slices.SortFunc(sorted, func(a, b Interval) int {
		if a.Day != b.Day {
			return int(a.Day) - int(b.Day)
		}
		return a.Start - b.Start
	})
	for i, iv := range sorted {
		if iv.Day < time.Sunday || iv.Day > time.Saturday {
			return nil, fmt.Errorf("unknown weekday %d", iv.Day)
		}
		if iv.Start < 0 || iv.End > 24*60 || iv.End <= iv.Start {
			return nil, fmt.Errorf("%s: working hours must end after they start (%s-%s)", iv.Day, FormatClock(iv.Start), FormatClock(iv.End))
		}
		if i > 0 && sorted[i-1].Day == iv.Day && sorted[i-1].End > iv.Start {
			return nil, fmt.Errorf("%s: working hours overlap", iv.Day)
		}
	}
	days := map[string]bool{}
	for _, h := range holidays {
		d, err := time.Parse(time.DateOnly, strings.TrimSpace(h))
		if err != nil {
			return nil, fmt.Errorf("%q is not a date (use YYYY-MM-DD)", h)
		}
		days[d.Format(time.DateOnly)] = true
	}
	return &Calendar{Location: loc, Hours: sorted, Holidays: days}, nil
}
