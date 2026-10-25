package servicedesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/notify"
	"github.com/workflowproj/workflow/internal/tracker"
)

// OnEvent runs inside the transaction of every change to an issue of a service project
// (tracker.OnEvent): it recomputes the request's SLA clocks, so queues, JQL and the issue view
// see them at once, and queues the follow-up emails to run after the change commits.
func (s *Service) OnEvent(ctx context.Context, tx pgx.Tx, actor *authz.Actor, p dbq.Project, issue *dbq.Issue, event string, data map[string]any) error {
	if issue == nil || event == "issue.deleted" || event == "sla.breached" {
		return nil
	}
	if err := s.recompute(ctx, tx, dbq.New(tx), issue.ID); err != nil {
		return err
	}
	switch event {
	case "issue.created", "issue.transitioned", "comment.created":
	default:
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.Jobs.InsertTx(ctx, tx, jobs.ServiceDeskEvent{
		Event: event, ProjectID: p.ID, IssueID: issue.ID, ActorID: actor.UserID(), ActorType: string(actor.Type), Data: raw,
	}, nil)
	return err
}

// HandleEvent emails the people following a request in the portal about what matters to them
// (jobs.ServiceDeskEvent): the request was received, an agent replied, the request needs their
// answer or was resolved, it was shared with them, or their approval is needed. Agents follow
// requests through the usual notifications instead, so people who can browse the project are
// only emailed about approvals. Internal notes are never emailed.
func (s *Service) HandleEvent(ctx context.Context, ev jobs.ServiceDeskEvent) error {
	data := map[string]any{}
	if len(ev.Data) > 0 {
		_ = json.Unmarshal(ev.Data, &data)
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		i, err := q.GetIssue(ctx, ev.IssueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		p, err := q.GetProject(ctx, i.ProjectID)
		if err != nil {
			return err
		}
		d, err := q.GetServiceDesk(ctx, p.ID)
		if err != nil {
			return nil
		}
		sr, err := q.GetServiceRequest(ctx, i.ID)
		if err != nil || sr.RequestTypeID == nil {
			return nil // the portal does not show it, so nobody follows it there
		}
		st, err := q.GetStatusesByIDs(ctx, []uuid.UUID{i.StatusID})
		if err != nil || len(st) == 0 {
			return err
		}
		key := tracker.IssueKey(p, i)
		m := mailer{s: s, tx: tx, q: q, p: p, issue: i, base: notify.RequestUpdate{
			Kind: "requests", Key: key, Summary: i.Summary, Status: st[0].Name, StatusCategory: st[0].Category,
			Portal: d.PortalName, URL: s.RequestURL(key),
		}}
		actor := ""
		if ev.ActorID != nil {
			if u, err := q.GetUser(ctx, *ev.ActorID); err == nil {
				actor = u.DisplayName
			}
		}
		switch ev.Event {
		case "issue.created":
			if i.ReporterID == nil {
				return nil
			}
			u := m.base
			u.Headline = "We received your request " + key
			u.Intro = []string{fmt.Sprintf("Thanks for contacting %s. Your request is %s; we'll email you when there is news, and you can follow it in the help centre.", d.PortalName, key)}
			u.Reason = "You're receiving this because you raised " + key + "."
			return m.send(ctx, []uuid.UUID{*i.ReporterID}, nil, u)
		case "comment.created":
			c, who, ok := publicComment(ctx, q, i, data)
			if !ok {
				return nil // internal notes never leave the agent view
			}
			u := m.base
			u.Headline = who + " replied on " + key
			u.Actor, u.What, u.Excerpt = who, "Replied", c.Body
			u.Button = "Reply"
			u.Reason = "You're receiving this because you raised " + key + " or it is shared with you."
			return m.send(ctx, m.followers(ctx), c.AuthorID, u)
		case "issue.transitioned":
			to := fmt.Sprint(data["to"])
			u := m.base
			// A reply sent to the customer from the transition's screen goes in this email.
			c, who, replied := publicComment(ctx, q, i, data)
			if replied {
				u.Excerpt = c.Body
			}
			switch {
			case strings.EqualFold(to, statusWaitingForCustomer):
				u.Headline = key + " needs your reply"
				u.Intro = []string{"We need more information from you to go on. Reply in the help centre."}
				u.Button = "Reply"
			case fmt.Sprint(data["toCategory"]) == "done" && fmt.Sprint(data["fromCategory"]) != "done":
				u.Headline = key + " is " + strings.ToLower(to)
				if i.Resolution != nil && *i.Resolution != "Done" {
					u.Intro = []string{"Resolution: " + *i.Resolution + "."}
				} else {
					u.Intro = []string{"We think this is sorted. If it isn't, reply in the help centre."}
				}
			case replied:
				u.Headline = who + " replied on " + key
				u.Button = "Reply"
			default:
				return nil // other moves are agents' business
			}
			if actor != "" {
				u.Actor, u.What = actor, "Changed the status to "+to
			}
			u.Reason = "You're receiving this because you raised " + key + " or it is shared with you."
			return m.send(ctx, m.followers(ctx), ev.ActorID, u)
		case "participant.added":
			id, err := uuid.Parse(fmt.Sprint(data["userId"]))
			if err != nil {
				return nil
			}
			u := m.base
			u.Headline = key + " was shared with you"
			u.Intro = []string{fmt.Sprintf("%s shared this request with you. You can follow it and reply in the help centre.", strings.TrimSpace(actor+" "))}
			if actor == "" {
				u.Intro = []string{"This request was shared with you. You can follow it and reply in the help centre."}
			}
			u.Reason = "You're receiving this because " + key + " was shared with you."
			return m.send(ctx, []uuid.UUID{id}, nil, u)
		case "approval.requested":
			approvalID, err := uuid.Parse(fmt.Sprint(data["approvalId"]))
			if err != nil {
				return nil
			}
			rows, err := q.ListApprovers(ctx, []uuid.UUID{approvalID})
			if err != nil {
				return err
			}
			reporter := "Someone"
			if i.ReporterID != nil {
				if r, err := q.GetUser(ctx, *i.ReporterID); err == nil {
					reporter = r.DisplayName
				}
			}
			u := m.base
			u.Kind = "approvals"
			u.Headline = "Your approval is needed on " + key
			u.Intro = []string{reporter + " raised a request that needs your approval before work starts. Approve or decline it in the help centre."}
			u.Button = "Review " + key
			u.Reason = "You're receiving this because you are an approver of " + key + "."
			for _, r := range rows {
				if r.Decision != nil {
					continue
				}
				if _, err := s.Notify.SendRequestUpdate(ctx, tx, q, r.User.ID, i.ID, u); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// mailer sends one request's emails inside the job's transaction.
type mailer struct {
	s     *Service
	tx    pgx.Tx
	q     *dbq.Queries
	p     dbq.Project
	issue dbq.Issue
	base  notify.RequestUpdate
}

// followers are the request's reporter and the people it is shared with.
// publicComment reads the comment an event names (data["commentId"]) when customers may see it:
// a reply on this request, not an internal note. who is its author's name.
func publicComment(ctx context.Context, q *dbq.Queries, i dbq.Issue, data map[string]any) (c dbq.Comment, who string, ok bool) {
	id, err := uuid.Parse(fmt.Sprint(data["commentId"]))
	if err != nil {
		return c, "", false
	}
	c, err = q.GetComment(ctx, id)
	if err != nil || c.Internal || c.IssueID != i.ID {
		return c, "", false
	}
	who = "Support"
	if c.AuthorID != nil {
		if a, err := q.GetUser(ctx, *c.AuthorID); err == nil {
			who = a.DisplayName
		}
	}
	return c, who, true
}

func (m mailer) followers(ctx context.Context) []uuid.UUID {
	var ids []uuid.UUID
	if m.issue.ReporterID != nil {
		ids = append(ids, *m.issue.ReporterID)
	}
	if parts, err := m.q.ListParticipants(ctx, m.issue.ID); err == nil {
		for _, u := range parts {
			ids = append(ids, u.ID)
		}
	}
	return ids
}

// send emails people who follow the request in the portal, except whoever caused the update and
// the project's agents (they get the usual notifications).
func (m mailer) send(ctx context.Context, ids []uuid.UUID, except *uuid.UUID, u notify.RequestUpdate) error {
	var done []uuid.UUID
	for _, id := range ids {
		if slices.Contains(done, id) || (except != nil && id == *except) {
			continue
		}
		done = append(done, id)
		user, err := m.q.GetUser(ctx, id)
		if err != nil {
			continue
		}
		role, err := tracker.EffectiveOrgRoleWith(ctx, m.q, user)
		if err != nil {
			return err
		}
		access, err := (&authz.Resolver{Q: m.q}).Access(ctx, authz.UserActor(user, role), m.p)
		if err != nil {
			return err
		}
		if access.CanBrowse() {
			continue
		}
		if _, err := m.s.Notify.SendRequestUpdate(ctx, m.tx, m.q, id, m.issue.ID, u); err != nil {
			return err
		}
	}
	return nil
}
