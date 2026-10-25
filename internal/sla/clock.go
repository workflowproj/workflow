package sla

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// Metric is what an SLA measures.
type Metric string

const (
	// FirstResponse runs from the request's creation until an agent first replies to the
	// customer (or the request is resolved without a reply). It runs once.
	FirstResponse Metric = "first_response"
	// Resolution runs from creation until the request is resolved (enters a done status).
	// Reopening it starts a new cycle.
	Resolution Metric = "resolution"
)

// Metrics in display order.
var Metrics = []Metric{FirstResponse, Resolution}

// Valid reports whether m is a known metric.
func (m Metric) Valid() bool { return slices.Contains(Metrics, m) }

// Name is the metric's label.
func (m Metric) Name() string {
	if m == FirstResponse {
		return "Time to first response"
	}
	return "Time to resolution"
}

// StatusChange is a request entering a status.
type StatusChange struct {
	At       time.Time
	StatusID uuid.UUID
	// Done: the status is in the done category (the request is resolved there).
	Done bool
}

// Timeline is what happened to a request.
type Timeline struct {
	Created time.Time
	// Initial is the status the request was created in (At is ignored).
	Initial StatusChange
	// Changes are the status changes after creation, in order.
	Changes []StatusChange
	// FirstReply is when an agent first replied to the customer, if they have.
	FirstReply *time.Time
}

// Goal is the target a request's clock is measured against, and the calendar it runs in.
type Goal struct {
	Target   time.Duration
	Calendar *Calendar
	// Pause lists statuses in which the clock stops (waiting for the customer, for an approval).
	Pause map[uuid.UUID]bool
}

// State is a clock at a moment.
type State string

const (
	Running State = "running"
	Paused  State = "paused"
	Stopped State = "stopped"
)

// Clock is one SLA of one request, as of a moment.
type Clock struct {
	State State
	// Elapsed is the working time used in the current cycle.
	Elapsed time.Duration
	Target  time.Duration
	// CycleStart is when the current cycle began (creation, or the last reopening).
	CycleStart time.Time
	// StoppedAt is when a stopped clock stopped.
	StoppedAt *time.Time
	// BreachAt is when a running clock reaches its target; nil unless running. For a clock that
	// went over its target, paused and resumed, it is the moment it would have reached the target
	// had it not paused, so that the time over it (Remaining) leaves out the pause.
	BreachAt *time.Time
	// BreachedAt is when the current cycle went over its target, if it has.
	BreachedAt *time.Time
}

// Breached reports whether the clock is over its target at now.
func (c Clock) Breached(now time.Time) bool {
	switch c.State {
	case Running:
		return c.BreachAt != nil && now.After(*c.BreachAt)
	default:
		return c.Elapsed > c.Target
	}
}

// Remaining is the working time left at now (negative once breached), measured in cal.
func (c Clock) Remaining(now time.Time, cal *Calendar) time.Duration {
	if c.State != Running || c.BreachAt == nil {
		return c.Target - c.Elapsed
	}
	if now.Before(*c.BreachAt) {
		return cal.Working(now, *c.BreachAt)
	}
	return -cal.Working(*c.BreachAt, now)
}

// ElapsedAt is the working time used by now (a running clock keeps counting after it was
// computed).
func (c Clock) ElapsedAt(now time.Time, cal *Calendar) time.Duration {
	if c.State != Running || c.BreachAt == nil {
		return c.Elapsed
	}
	return c.Target - c.Remaining(now, cal)
}

// event is a moment on the timeline that can start, pause, resume or stop a clock.
type event struct {
	at     time.Time
	status *StatusChange
	reply  bool
}

// Compute runs a request's clock for a metric from its creation up to now.
func Compute(m Metric, t Timeline, g Goal, now time.Time) Clock {
	cal := g.Calendar
	if cal == nil {
		cal = AroundTheClock
	}
	var evs []event
	for i := range t.Changes {
		evs = append(evs, event{at: t.Changes[i].At, status: &t.Changes[i]})
	}
	if m == FirstResponse && t.FirstReply != nil {
		evs = append(evs, event{at: *t.FirstReply, reply: true})
	}
	slices.SortStableFunc(evs, func(a, b event) int { return a.at.Compare(b.at) })

	c := Clock{Target: g.Target, CycleStart: t.Created}
	status := t.Initial
	var segStart time.Time // start of the running stretch
	set := func(at time.Time) {
		switch {
		case c.State == Stopped:
		case g.Pause[status.StatusID]:
			c.State = Paused
		default:
			c.State, segStart = Running, at
		}
	}
	// accrue adds the working time of the running stretch up to `to`, noting when it crosses the
	// target.
	accrue := func(to time.Time) {
		if c.State != Running || !to.After(segStart) {
			return
		}
		work := cal.Working(segStart, to)
		if c.BreachedAt == nil && c.Elapsed+work > c.Target {
			at := cal.Add(segStart, c.Target-c.Elapsed)
			c.BreachedAt = &at
		}
		c.Elapsed += work
		segStart = to
	}
	stop := func(at time.Time) {
		c.State = Stopped
		c.StoppedAt = &at
	}
	if status.Done {
		stop(t.Created)
	} else {
		set(t.Created)
	}
	for _, e := range evs {
		at := e.at
		if at.After(now) {
			break
		}
		if at.Before(t.Created) {
			at = t.Created
		}
		accrue(at)
		if e.reply {
			if c.State != Stopped {
				stop(at)
			}
			continue
		}
		status = *e.status
		switch {
		case c.State == Stopped && !status.Done && m == Resolution:
			// Reopened: a new cycle against the same goal.
			c = Clock{Target: g.Target, CycleStart: at}
			set(at)
		case c.State != Stopped && status.Done:
			stop(at)
		default:
			set(at)
		}
	}
	if c.State == Running {
		before := c.Elapsed
		start := segStart
		accrue(now)
		if c.BreachedAt != nil && !c.BreachedAt.After(start) {
			// Already over the target when this stretch began: count the overrun back from it.
			at := cal.Sub(start, before-c.Target)
			c.BreachAt = &at
		} else {
			at := cal.Add(start, c.Target-before)
			c.BreachAt = &at
		}
		if c.BreachedAt != nil && !now.After(*c.BreachedAt) {
			c.BreachedAt = nil // reaching the target exactly is not a breach
		}
	}
	return c
}
