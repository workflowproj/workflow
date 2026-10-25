package servicedesk

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
)

// deskActor makes the workflow moves the service desk itself causes (approval transitions are
// reserved to it), recorded as the person who caused them "via approval" or "via portal".
func deskActor(u dbq.User, via string) *authz.Actor {
	return &authz.Actor{User: &u, Type: authz.ActorSystem, OrgRole: "admin", Via: via}
}

// history writes a service desk entry to a request's history (approvals, request type,
// participants), attributed to a person.
func history(ctx context.Context, q *dbq.Queries, issueID uuid.UUID, by *dbq.User, field string, from, to *string) error {
	p := dbq.InsertHistoryParams{IssueID: issueID, ActorType: "system", Field: field, FromValue: from, ToValue: to}
	if by != nil {
		p.ActorID, p.ActorType = &by.ID, "user"
	}
	return q.InsertHistory(ctx, p)
}

// startApproval asks a request type's approvers to approve a new request, in its creating
// transaction. Approvers are the named people and the group's members as of now, except the
// person who raised it (nobody approves their own request); with nobody to ask (an emptied
// group, or only the reporter) the request goes ahead without an approval.
func (s *Service) startApproval(ctx context.Context, tx pgx.Tx, q *dbq.Queries, p dbq.Project, issue dbq.Issue, cfg ApprovalConfig) (bool, error) {
	ids := slices.Clone(cfg.UserIDs)
	if cfg.GroupID != nil {
		members, err := q.GroupMembers(ctx, *cfg.GroupID)
		if err != nil {
			return false, err
		}
		for _, m := range members {
			ids = append(ids, m.ID)
		}
	}
	users, err := q.GetUsersByIDs(ctx, ids)
	if err != nil {
		return false, err
	}
	var approvers []dbq.User
	for _, u := range users {
		if u.Active && u.DeletedAt == nil && u.Kind == "human" && (issue.ReporterID == nil || u.ID != *issue.ReporterID) {
			approvers = append(approvers, u)
		}
	}
	if len(approvers) == 0 {
		s.Log.Warn("request needs an approval but nobody can approve it", "issue", issue.ID)
		return false, nil
	}
	a, err := q.CreateApproval(ctx, dbq.CreateApprovalParams{IssueID: issue.ID, Mode: cfg.Mode, ApprovedStatus: cfg.ApprovedStatus, DeclinedStatus: cfg.DeclinedStatus})
	if err != nil {
		return false, err
	}
	var names []string
	for _, u := range approvers {
		if err := q.AddApprover(ctx, dbq.AddApproverParams{ApprovalID: a.ID, UserID: u.ID}); err != nil {
			return false, err
		}
		names = append(names, u.DisplayName)
	}
	what := "Approval requested from " + strings.Join(names, ", ")
	if cfg.Mode == "all" && len(names) > 1 {
		what += " (all must approve)"
	}
	if err := history(ctx, q, issue.ID, nil, "approval", nil, &what); err != nil {
		return false, err
	}
	data, _ := json.Marshal(map[string]any{"approvalId": a.ID})
	_, err = s.Jobs.InsertTx(ctx, tx, jobs.ServiceDeskEvent{Event: "approval.requested", ProjectID: p.ID, IssueID: issue.ID, ActorType: "system", Data: data}, nil)
	return true, err
}

// decide records an approver's decision on one of a request's approvals. When it settles the
// approval (any one approval, everyone's approvals, or one decline), the request moves to the
// configured status.
func (s *Service) decide(ctx context.Context, u dbq.User, issueID, approvalID uuid.UUID, approve bool, comment string) error {
	comment = strings.TrimSpace(comment)
	if len(comment) > 5000 {
		return apperr.Invalid("comment", "comments are at most 5000 characters")
	}
	decision := "approved"
	if !approve {
		decision = "declined"
	}
	var settled *dbq.RequestApproval
	err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		a, err := q.GetApprovalForUpdate(ctx, approvalID)
		if err != nil || a.IssueID != issueID {
			return apperr.NotFound("approval")
		}
		if a.Status != "pending" {
			return apperr.Conflict("approval_decided", "this approval was already %s", a.Status)
		}
		// A request that has left Pending approval (moved on by an agent or a rule) no longer
		// waits for this approval, so a late decision must not move it again.
		if waiting, err := awaitingApproval(ctx, q, issueID); err != nil {
			return err
		} else if !waiting {
			return apperr.Conflict("not_awaiting_approval", "this request is no longer waiting for approval")
		}
		n, err := q.SetApproverDecision(ctx, dbq.SetApproverDecisionParams{ApprovalID: a.ID, UserID: u.ID, Decision: &decision, Comment: comment})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Forbidden("you are not asked to approve this request, or already decided")
		}
		what := u.DisplayName + " approved"
		if !approve {
			what = u.DisplayName + " declined"
		}
		if comment != "" {
			what += ": " + comment
		}
		if err := history(ctx, q, issueID, &u, "approval", nil, &what); err != nil {
			return err
		}
		approvers, err := q.ListApprovers(ctx, []uuid.UUID{a.ID})
		if err != nil {
			return err
		}
		final := ""
		switch {
		case !approve:
			final = "declined"
		case a.Mode == "any":
			final = "approved"
		case !slices.ContainsFunc(approvers, func(r dbq.ListApproversRow) bool { return r.Decision == nil || *r.Decision != "approved" }):
			final = "approved"
		}
		if final == "" {
			return nil
		}
		if err := q.SetApprovalStatus(ctx, dbq.SetApprovalStatusParams{ID: a.ID, Status: final}); err != nil {
			return err
		}
		outcome := "Request approved"
		if final == "declined" {
			outcome = "Request declined"
		}
		if err := history(ctx, q, issueID, nil, "approval", nil, &outcome); err != nil {
			return err
		}
		a.Status = final
		settled = &a
		return nil
	})
	if err != nil || settled == nil {
		return err
	}
	target := settled.ApprovedStatus
	if settled.Status == "declined" {
		target = settled.DeclinedStatus
	}
	moved, err := s.Tracker.TransitionFromStatus(ctx, deskActor(u, "approval"), issueID, statusPendingApproval, target)
	if err != nil {
		s.Log.Warn("approval decided but the request did not move", "issue", issueID, "to", target, "error", err)
	} else if !moved {
		s.Log.Info("approval decided; the request is no longer waiting for it, so it stays where it is", "issue", issueID, "to", target)
	}
	return nil
}

// awaitingApproval reports whether a request is in the Pending approval status.
func awaitingApproval(ctx context.Context, q *dbq.Queries, issueID uuid.UUID) (bool, error) {
	i, err := q.GetIssue(ctx, issueID)
	if err != nil {
		return false, err
	}
	st, err := q.GetStatusesByIDs(ctx, []uuid.UUID{i.StatusID})
	if err != nil || len(st) != 1 {
		return false, err
	}
	return strings.EqualFold(st[0].Name, statusPendingApproval), nil
}

// approvalsView renders a request's approvals for a viewer (who may be an approver).
func (s *Service) approvalsView(ctx context.Context, issueID uuid.UUID, viewer *uuid.UUID) ([]api.Approval, error) {
	as, err := s.Q.ListApprovals(ctx, issueID)
	if err != nil {
		return nil, err
	}
	out := []api.Approval{}
	if len(as) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(as))
	for k, a := range as {
		ids[k] = a.ID
	}
	rows, err := s.Q.ListApprovers(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range as {
		v := api.Approval{Id: a.ID, Status: api.ApprovalStatus(a.Status), Mode: api.ApprovalMode(a.Mode), CreatedAt: a.CreatedAt, DecidedAt: a.DecidedAt, Approvers: []api.Approver{}}
		for _, r := range rows {
			if r.ApprovalID != a.ID {
				continue
			}
			av := api.Approver{Person: personView(r.User), DecidedAt: r.DecidedAt}
			if r.Decision != nil {
				d := api.ApproverDecision(*r.Decision)
				av.Decision = &d
			}
			if r.Comment != "" {
				av.Comment = ptr(r.Comment)
			}
			v.Approvers = append(v.Approvers, av)
			if viewer != nil && r.User.ID == *viewer && r.Decision == nil && a.Status == "pending" {
				v.CanDecide = true
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// pendingApprovalFor finds the approval of a request waiting for this person's decision.
func (s *Service) pendingApprovalFor(ctx context.Context, issueID, userID uuid.UUID) (uuid.UUID, error) {
	views, err := s.approvalsView(ctx, issueID, &userID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, v := range views {
		if v.CanDecide {
			return v.Id, nil
		}
	}
	return uuid.Nil, apperr.Forbidden("nothing on this request is waiting for your approval")
}

// DecideAsAgent records an approval decision made from the agent issue view.
func (s *Service) DecideAsAgent(ctx context.Context, actor *authz.Actor, key string, approvalID uuid.UUID, d api.ApprovalDecision) (api.ServiceRequest, error) {
	i, _, _, err := s.Tracker.IssueFor(ctx, actor, key, authz.ProjectBrowse)
	if err != nil {
		return api.ServiceRequest{}, err
	}
	if actor.User == nil {
		return api.ServiceRequest{}, apperr.Forbidden("only people can approve requests")
	}
	if !d.Decision.Valid() {
		return api.ServiceRequest{}, apperr.Invalid("decision", "decision is approve or decline")
	}
	if err := s.decide(ctx, *actor.User, i.ID, approvalID, d.Decision == api.ApprovalDecisionDecisionApprove, deref(d.Comment)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.ServiceRequest{}, apperr.NotFound("approval")
		}
		return api.ServiceRequest{}, err
	}
	return s.Request(ctx, actor, key)
}
