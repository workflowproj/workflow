// Package timetrack parses and formats work durations the way people write them in a tracker:
// "1w 2d 3h 30m", where a day and a week are working days and weeks (WFM_TIME_HOURS_PER_DAY,
// WFM_TIME_DAYS_PER_WEEK). Durations are stored as seconds. The web UI has the same rules in
// web/src/lib/duration.ts; keep the two in step.
package timetrack

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Units says how long a working day and week are.
type Units struct {
	HoursPerDay float64
	DaysPerWeek float64
}

// Default is an eight-hour day and a five-day week.
var Default = Units{HoursPerDay: 8, DaysPerWeek: 5}

// Validate checks the units are usable: a day of more than 0 and at most 24 hours, a week of
// more than 0 and at most 7 days.
func (u Units) Validate() error {
	if !(u.HoursPerDay > 0 && u.HoursPerDay <= 24) {
		return fmt.Errorf("hours per day must be more than 0 and at most 24, got %v", u.HoursPerDay)
	}
	if !(u.DaysPerWeek > 0 && u.DaysPerWeek <= 7) {
		return fmt.Errorf("days per week must be more than 0 and at most 7, got %v", u.DaysPerWeek)
	}
	return nil
}

// minutes per working day and week (rounded, so 7.5-hour days work).
func (u Units) dayMinutes() int64  { return max(int64(math.Round(u.HoursPerDay*60)), 1) }
func (u Units) weekMinutes() int64 { return max(int64(math.Round(u.HoursPerDay*u.DaysPerWeek*60)), 1) }

// DaySeconds is the length of a working day.
func (u Units) DaySeconds() int64 { return u.dayMinutes() * 60 }

// WeekSeconds is the length of a working week.
func (u Units) WeekSeconds() int64 { return u.weekMinutes() * 60 }

var (
	bareNumber = regexp.MustCompile(`^\d+(?:[.,]\d+)?$`)
	// Longer unit names first: Go's regexp prefers the leftmost alternative, like Perl.
	part = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s*(weeks|week|wks|wk|w|days|day|d|hours|hour|hrs|hr|h|minutes|minute|mins|min|m)`)
)

// ErrEmpty is returned for a blank duration.
var ErrEmpty = errors.New("enter a duration such as 2h 30m")

// Parse reads a duration such as "1w 2d 3h 30m", "1.5h", "90m" or "2 days" into seconds. A bare
// number is minutes. Units may come in any order and repeat (they add up).
func Parse(s string, u Units) (int64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, ErrEmpty
	}
	if bareNumber.MatchString(s) {
		n, _ := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
		return int64(math.Round(n * 60)), nil
	}
	var total float64
	rest := s
	for rest != "" {
		m := part.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf("%q is not a duration (use w, d, h and m, e.g. 1d 4h or 90m)", s)
		}
		n, _ := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
		switch m[2][0] {
		case 'w':
			total += n * float64(u.WeekSeconds())
		case 'd':
			total += n * float64(u.DaySeconds())
		case 'h':
			total += n * 3600
		default:
			total += n * 60
		}
		rest = strings.TrimLeft(rest[len(m[0]):], " ,+")
	}
	if total > math.MaxInt32*60 {
		return 0, fmt.Errorf("%q is too long", s)
	}
	return int64(math.Round(total)), nil
}

// Format writes seconds as "1w 2d 3h 30m" (rounded to the minute, zero parts left out);
// nothing at all is "0m".
func Format(seconds int64, u Units) string {
	mins := int64(math.Round(float64(seconds) / 60))
	if mins <= 0 {
		return "0m"
	}
	week, day := u.weekMinutes(), u.dayMinutes()
	var parts []string
	if w := mins / week; w > 0 {
		parts = append(parts, strconv.FormatInt(w, 10)+"w")
		mins -= w * week
	}
	if d := mins / day; d > 0 {
		parts = append(parts, strconv.FormatInt(d, 10)+"d")
		mins -= d * day
	}
	if h := mins / 60; h > 0 {
		parts = append(parts, strconv.FormatInt(h, 10)+"h")
		mins -= h * 60
	}
	if mins > 0 {
		parts = append(parts, strconv.FormatInt(mins, 10)+"m")
	}
	return strings.Join(parts, " ")
}

// FormatPtr formats an optional duration; nil stays nil.
func FormatPtr(seconds *int64, u Units) *string {
	if seconds == nil {
		return nil
	}
	s := Format(*seconds, u)
	return &s
}
