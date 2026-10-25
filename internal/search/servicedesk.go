package search

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/timetrack"
)

// Service desk fields: SLA clocks, request types, approvals and participants.
//
//	"Time to resolution" = breached()          "Time to first response" < remaining("1h")
//	"Request type" = "Get IT help"             approval = pendingBy(currentUser())

// SLACalendar is a calendar SLA clocks run in. Deadline returns the instant by which d of working
// time has passed since from; remaining() compares running clocks' breach times with it.
type SLACalendar struct {
	ID       uuid.UUID
	Deadline func(from time.Time, d time.Duration) time.Time
}

// slaUnits read remaining() durations in calendar time: a day is 24 hours, a week 7 days.
var slaUnits = timetrack.Units{HoursPerDay: 24, DaysPerWeek: 7}

func slaExists(metric, cond string) string {
	return "EXISTS (SELECT 1 FROM issue_slas s WHERE s.issue_id = i.id AND s.metric = '" + metric + "' AND " + cond + ")"
}

// slaField compiles "Time to first response" and "Time to resolution": the clock's state with
// breached(), running(), paused() and completed(), and the time left with < remaining("2h")
// (also <=, > and >=). IS EMPTY matches requests without that SLA.
func (b *builder) slaField(c Clause, metric string) (string, error) {
	switch c.Op {
	case "<", "<=", ">", ">=":
		return b.slaRemaining(c, metric)
	}
	return b.setField(c, func(v Value) (string, error) {
		if v.Kind != VFunc {
			return "", errAt(v.Pos, "compare %s with breached(), running(), paused(), completed() or remaining(\"2h\")", c.Field)
		}
		switch strings.ToLower(v.Text) {
		case "breached":
			return slaExists(metric, fmt.Sprintf("(s.breached OR (s.state = 'running' AND s.breach_at < %s::timestamptz))", b.arg(b.env.Now))), nil
		case "running":
			return slaExists(metric, "s.state = 'running'"), nil
		case "paused":
			return slaExists(metric, "s.state = 'paused'"), nil
		case "completed", "stopped":
			return slaExists(metric, "s.state = 'stopped'"), nil
		}
		return "", errAt(v.Pos, "unknown function %s() for %s (use breached, running, paused, completed or remaining)", v.Text, c.Field)
	}, "NOT EXISTS (SELECT 1 FROM issue_slas s WHERE s.issue_id = i.id AND s.metric = '"+metric+"')")
}

// slaRemaining compares the working time left on running and paused clocks. A running clock's
// time left is below d exactly when it breaches before d of working time from now in its own
// calendar, so each calendar's deadline is computed here and compared with breach_at.
func (b *builder) slaRemaining(c Clause, metric string) (string, error) {
	v := c.Values[0]
	text := v.Text
	if v.Kind == VFunc {
		if !strings.EqualFold(v.Text, "remaining") || len(v.Args) != 1 {
			return "", errAt(v.Pos, "use remaining(\"2h\") with %s %s", c.Field, c.Op)
		}
		text = v.Args[0].Text
	} else if v.Kind != VString && v.Kind != VNumber {
		return "", errAt(v.Pos, "expected remaining(\"2h\")")
	}
	secs, err := timetrack.Parse(text, slaUnits)
	if err != nil {
		return "", errAt(v.Pos, "%q is not a duration (use m, h, d and w, e.g. remaining(\"30m\"))", text)
	}
	d := time.Duration(secs) * time.Second
	now := b.env.Now
	deadlines := []string{fmt.Sprintf("(s.calendar_id IS NULL AND s.breach_at %s %s::timestamptz)", c.Op, b.arg(now.Add(d)))}
	if b.env.SLACalendars != nil {
		cals, err := b.env.SLACalendars()
		if err != nil {
			return "", err
		}
		for _, cal := range cals {
			at := cal.Deadline(now, d)
			if at.IsZero() {
				continue
			}
			deadlines = append(deadlines, fmt.Sprintf("(s.calendar_id = %s::uuid AND s.breach_at %s %s::timestamptz)", b.arg(cal.ID), c.Op, b.arg(at)))
		}
	}
	paused := fmt.Sprintf("(s.state = 'paused' AND s.target_seconds - s.elapsed_seconds %s %s::bigint)", c.Op, b.arg(secs))
	running := "(s.state = 'running' AND (" + strings.Join(deadlines, " OR ") + "))"
	return slaExists(metric, "("+paused+" OR "+running+")"), nil
}

// slaOrder sorts by when a clock breaches (paused clocks by their time left from now); clocks
// that stopped, and requests without the SLA, come last.
func slaOrder(metric string) string {
	return "(SELECT CASE s.state WHEN 'running' THEN s.breach_at WHEN 'paused' THEN now() + make_interval(secs => s.target_seconds - s.elapsed_seconds) END " +
		"FROM issue_slas s WHERE s.issue_id = i.id AND s.metric = '" + metric + "')"
}

func (b *builder) requestTypeField(c Clause) (string, error) {
	return b.setField(c, func(v Value) (string, error) {
		s, err := b.str(v)
		if err != nil {
			return "", err
		}
		p := b.arg(s)
		return fmt.Sprintf("EXISTS (SELECT 1 FROM service_requests sr JOIN request_types rt ON rt.id = sr.request_type_id WHERE sr.issue_id = i.id AND (lower(rt.name) = lower(%[1]s::text) OR rt.id::text = %[1]s::text))", p), nil
	}, "NOT EXISTS (SELECT 1 FROM service_requests sr WHERE sr.issue_id = i.id AND sr.request_type_id IS NOT NULL)")
}

// approvalField matches requests by their approvals: pending(), approved(), declined(), or
// pendingBy(user) for the ones waiting for someone's decision.
func (b *builder) approvalField(c Clause) (string, error) {
	return b.setField(c, func(v Value) (string, error) {
		if v.Kind != VFunc {
			return "", errAt(v.Pos, "compare approval with pending(), approved(), declined() or pendingBy(currentUser())")
		}
		switch fn := strings.ToLower(v.Text); fn {
		case "pending", "approved", "declined":
			return fmt.Sprintf("EXISTS (SELECT 1 FROM request_approvals ra WHERE ra.issue_id = i.id AND ra.status = %s::text)", b.arg(fn)), nil
		case "pendingby":
			if len(v.Args) != 1 {
				return "", errAt(v.Pos, "pendingBy() takes a person, e.g. pendingBy(currentUser())")
			}
			u, err := b.userSet(v.Args[0])
			if err != nil {
				return "", err
			}
			return "EXISTS (SELECT 1 FROM request_approvals ra JOIN approval_approvers aa ON aa.approval_id = ra.id WHERE ra.issue_id = i.id AND ra.status = 'pending' AND aa.decision IS NULL AND aa.user_id IN (" + u + "))", nil
		}
		return "", errAt(v.Pos, "unknown function %s() for approval (use pending, approved, declined or pendingBy)", v.Text)
	}, "NOT EXISTS (SELECT 1 FROM request_approvals ra WHERE ra.issue_id = i.id)")
}

func (b *builder) participantField(c Clause) (string, error) {
	return b.setField(c, func(v Value) (string, error) {
		u, err := b.userSet(v)
		if err != nil {
			return "", err
		}
		return "EXISTS (SELECT 1 FROM request_participants rp WHERE rp.issue_id = i.id AND rp.user_id IN (" + u + "))", nil
	}, "NOT EXISTS (SELECT 1 FROM request_participants rp WHERE rp.issue_id = i.id)")
}
