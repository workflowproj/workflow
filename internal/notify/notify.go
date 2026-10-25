// Package notify emails people about their in-app notifications: per-person preferences
// (immediately, in a daily digest or not at all, per kind of notification), immediate emails
// that turn a burst of changes to one issue into one email, the daily digest, the email
// templates and one-click unsubscribe (RFC 8058).
//
// tracker.Service.Notify creates in-app notifications; it asks DeliveryFor how each is emailed,
// stores the answer with the notification and, for immediate ones, calls Schedule. Service
// sends the emails from background jobs and serves the settings and unsubscribe endpoints.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // time zones for digests, also in minimal container images

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
)

// How a kind of notification is emailed.
const (
	Immediate = "immediate"
	Digest    = "digest"
	Off       = "off"
)

// Kind is a kind of in-app notification people choose an email delivery for.
type Kind struct {
	Key         string
	Label       string
	Description string
	// Noun names these emails in "Stop emails about ..." links.
	Noun    string
	Default string
	// Portal kinds are about service requests followed in the customer portal; they are the only
	// ones customers see, and they are emailed immediately or not at all (never in a digest).
	Portal bool
}

// Kinds in display order. Notifications of a kind not listed here are emailed in the digest.
var Kinds = []Kind{
	{Key: "assigned", Label: "Assigned to me", Description: "Someone assigns an issue to you.", Noun: "assignments", Default: Immediate},
	{Key: "commented", Label: "Comments", Description: "New comments on issues you report, are assigned to or watch.", Noun: "comments", Default: Digest},
	{Key: "transitioned", Label: "Status changes", Description: "Issues you report, are assigned to or watch move to another status.", Noun: "status changes", Default: Digest},
	{Key: "automation", Label: "Automation", Description: "Automation rules that notify you about an issue.", Noun: "automation updates", Default: Digest},
	{Key: "sla", Label: "SLA breaches", Description: "A service request assigned to you misses an SLA goal.", Noun: "SLA breaches", Default: Immediate},
	{Key: "requests", Label: "My requests", Description: "Replies and status changes on service requests you raised or that are shared with you.", Noun: "your requests", Default: Immediate, Portal: true},
	{Key: "approvals", Label: "Approvals", Description: "Someone asks for your approval on a service request.", Noun: "approval requests", Default: Immediate, Portal: true},
}

// Deliveries lists the choices a kind offers.
func (k Kind) Deliveries() []string {
	if k.Portal {
		return []string{Immediate, Off}
	}
	return []string{Immediate, Digest, Off}
}

// KindByKey finds a kind.
func KindByKey(key string) (Kind, bool) {
	i := slices.IndexFunc(Kinds, func(k Kind) bool { return k.Key == key })
	if i < 0 {
		return Kind{}, false
	}
	return Kinds[i], true
}

// ValidDelivery reports whether d is immediate, digest or off.
func ValidDelivery(d string) bool { return d == Immediate || d == Digest || d == Off }

// Defaults for people who have not changed their settings.
const (
	DefaultDigestHour = 9
	DefaultTimeZone   = "UTC"
)

// Settings are one person's email notification preferences.
type Settings struct {
	// Deliveries holds the choices people made, by kind; other kinds use their default.
	Deliveries   map[string]string
	DigestHour   int
	TimeZone     string
	Paused       bool
	LastDigestAt *time.Time
}

// Defaults returns the settings of someone who never changed them.
func Defaults() Settings {
	return Settings{Deliveries: map[string]string{}, DigestHour: DefaultDigestHour, TimeZone: DefaultTimeZone}
}

// FromRow converts stored settings.
func FromRow(r dbq.EmailNotificationSetting) Settings {
	return Settings{Deliveries: parseDeliveries(r.Deliveries), DigestHour: int(r.DigestHour), TimeZone: r.TimeZone, Paused: r.Paused, LastDigestAt: r.LastDigestAt}
}

func parseDeliveries(raw []byte) map[string]string {
	m := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// Delivery returns how notifications of a kind are emailed (ignoring Paused).
func (s Settings) Delivery(kind string) string {
	k, known := KindByKey(kind)
	if d, ok := s.Deliveries[kind]; ok && ValidDelivery(d) {
		if known && k.Portal && d == Digest {
			return Immediate
		}
		return d
	}
	if known {
		return k.Default
	}
	return Digest
}

// Location is the settings' time zone (UTC if it no longer loads).
func (s Settings) Location() *time.Location {
	if loc, err := LoadZone(s.TimeZone); err == nil {
		return loc
	}
	return time.UTC
}

// LoadZone loads an IANA time zone name such as Europe/London ("Local" is refused: it means
// whatever the server's zone happens to be).
func LoadZone(name string) (*time.Location, error) {
	if strings.EqualFold(name, "local") {
		return nil, errBadZone
	}
	return time.LoadLocation(name)
}

var errBadZone = errors.New("unknown time zone")

// digestSlot is the most recent digest time at or before t.
func (s Settings) digestSlot(t time.Time) time.Time {
	loc := s.Location()
	lt := t.In(loc)
	slot := time.Date(lt.Year(), lt.Month(), lt.Day(), s.DigestHour, 0, 0, 0, loc)
	if slot.After(t) {
		slot = time.Date(lt.Year(), lt.Month(), lt.Day()-1, s.DigestHour, 0, 0, 0, loc)
	}
	return slot
}

// NextDigest is the first digest time after t.
func (s Settings) NextDigest(t time.Time) time.Time {
	last := s.digestSlot(t).In(s.Location())
	return time.Date(last.Year(), last.Month(), last.Day()+1, s.DigestHour, 0, 0, 0, s.Location())
}

// DigestDue reports whether a digest should go out at now for notifications waiting since
// oldest: a digest time has passed since the oldest one arrived.
func (s Settings) DigestDue(oldest, now time.Time) bool {
	return !oldest.After(s.digestSlot(now))
}

// DeliveryFor decides, as a notification is created, how it is emailed: Immediate, Digest or ""
// (not at all). Notifications without an issue, deactivated people, service accounts and paused
// settings get no email.
func DeliveryFor(r dbq.EmailRecipientRow, kind string, hasIssue bool) string {
	if !hasIssue || !r.Active || r.DeletedAt != nil || r.Kind != "human" || r.Email == "" {
		return ""
	}
	if r.Paused != nil && *r.Paused {
		return ""
	}
	s := Settings{Deliveries: parseDeliveries(r.Deliveries)}
	switch d := s.Delivery(kind); d {
	case Immediate, Digest:
		return d
	}
	return ""
}

// Schedule queues the email for an immediate notification. The job runs one window from now and
// is unique per person, issue and window, so everything that happens to the issue meanwhile
// goes out in one email; the job sends whatever is pending when it runs.
func Schedule(ctx context.Context, enq jobs.Enqueuer, window time.Duration, now time.Time, userID, issueID uuid.UUID) error {
	_, err := enq.Insert(ctx, jobs.EmailNotify{UserID: userID, IssueID: issueID}, &river.InsertOpts{
		ScheduledAt: now.Add(window),
		MaxAttempts: 5,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: window, ByState: []rivertype.JobState{
			// Not completed: once a job has sent, the next change needs a new one.
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable, rivertype.JobStateRunning, rivertype.JobStateScheduled,
		}},
	})
	return err
}
