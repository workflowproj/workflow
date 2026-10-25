package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/mail"
)

// RequestUpdate is an email to someone following a service request in the customer portal (its
// reporter, people it is shared with, approvers). It never carries internal notes: callers
// build it from what the portal shows.
type RequestUpdate struct {
	// Kind is "requests" or "approvals" (the person's setting for it applies).
	Kind                   string
	Key, Summary           string
	Status, StatusCategory string
	// Portal names the help desk; URL opens the request in the portal.
	Portal, URL string
	Headline    string
	Intro       []string
	// Actor did What; Excerpt quotes a reply.
	Actor, What, Excerpt string
	Button               string
	Reason               string
}

// PortalManageURL is where portal users change their email notifications.
func (s *Service) PortalManageURL() string {
	return s.Cfg.PublicURL + "/portal/profile#email-notifications"
}

// SendRequestUpdate queues an email about a service request in tx, right away (these are not
// batched or put in digests), unless the person switched that kind of email off, paused email or
// cannot receive email. It reports whether an email was queued.
func (s *Service) SendRequestUpdate(ctx context.Context, tx pgx.Tx, q *dbq.Queries, userID, issueID uuid.UUID, u RequestUpdate) (bool, error) {
	r, err := s.recipient(ctx, q, userID)
	if err != nil || !r.ok || r.settings.Delivery(u.Kind) == Off {
		return false, err
	}
	ev := Event{Actor: u.Actor, What: u.What, When: s.Now().In(r.loc).Format("Mon 2 Jan 15:04")}
	if u.Excerpt != "" {
		ev.Excerpt = Excerpt(u.Excerpt, excerptLength)
	}
	block := IssueBlock{Key: u.Key, Summary: u.Summary, URL: u.URL, Project: u.Portal, Status: u.Status, StatusCategory: u.StatusCategory}
	if u.What != "" {
		block.Events = []Event{ev}
	}
	e := Email{
		Brand:     s.Brand(),
		Subject:   fmt.Sprintf("[%s] %s", u.Key, u.Summary),
		Preheader: u.Headline,
		Headline:  u.Headline,
		Intro:     u.Intro,
		Issues:    []IssueBlock{block},
		Button:    &Link{Label: u.Button, URL: u.URL},
		Reason:    u.Reason,
		ManageURL: s.PortalManageURL(),
	}
	if u.Button == "" {
		e.Button.Label = "View " + u.Key
	}
	unsub := s.UnsubscribeURL(userID, "kinds:"+u.Kind)
	e.Unsub = &Link{Label: stopLabel([]string{u.Kind}), URL: unsub}
	msg, err := s.message(r.user.Email, e, unsub, s.threadID(issueID))
	if err != nil {
		return false, err
	}
	return true, mail.Queue(ctx, s.Jobs, tx, msg)
}
