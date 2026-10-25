package servicedesk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/tracker"
)

// agentRequest loads an issue of a service project for an agent.
func (s *Service) agentRequest(ctx context.Context, actor *authz.Actor, key string, perm authz.Permission) (dbq.Issue, dbq.Project, authz.Access, error) {
	i, p, access, err := s.Tracker.IssueFor(ctx, actor, key, perm)
	if err != nil {
		return i, p, access, err
	}
	if !tracker.IsServiceProject(p) {
		return i, p, access, apperr.NotFound("service request")
	}
	return i, p, access, nil
}

// Request returns what agents see of a service request besides the issue: its request type,
// SLA clocks, approvals and participants.
func (s *Service) Request(ctx context.Context, actor *authz.Actor, key string) (api.ServiceRequest, error) {
	i, p, _, err := s.agentRequest(ctx, actor, key, authz.ProjectBrowse)
	if err != nil {
		return api.ServiceRequest{}, err
	}
	k := tracker.IssueKey(p, i)
	v := api.ServiceRequest{IssueKey: k, PortalUrl: s.RequestURL(k), Participants: []api.User{}}
	sr, err := s.Q.GetServiceRequest(ctx, i.ID)
	switch {
	case err == nil:
		ch := api.ServiceRequestChannel(sr.Channel)
		v.Channel = &ch
		if sr.RequestTypeID != nil {
			if rt, err := s.Q.GetRequestType(ctx, *sr.RequestTypeID); err == nil {
				ref := requestTypeRef(rt)
				v.RequestType = &ref
				v.CustomerVisible = true
			}
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return v, err
	}
	rows, err := s.Q.IssueSLAsFor(ctx, []uuid.UUID{i.ID})
	if err != nil {
		return v, err
	}
	v.Slas = s.slaViews(ctx, rows)
	if v.Approvals, err = s.approvalsView(ctx, i.ID, actor.UserID()); err != nil {
		return v, err
	}
	parts, err := s.Q.ListParticipants(ctx, i.ID)
	if err != nil {
		return v, err
	}
	for _, u := range parts {
		v.Participants = append(v.Participants, tracker.UserView(u))
	}
	return v, nil
}

// SetRequestType gives an issue a request type (agents raising a request for a customer set the
// reporter and this; the portal only shows issues that have one).
func (s *Service) SetRequestType(ctx context.Context, actor *authz.Actor, key string, requestTypeID uuid.UUID) (api.ServiceRequest, error) {
	i, p, access, err := s.agentRequest(ctx, actor, key, authz.ProjectBrowse)
	if err != nil {
		return api.ServiceRequest{}, err
	}
	if !tracker.CanEdit(access, actor, i) {
		return api.ServiceRequest{}, apperr.Forbidden("you cannot edit %s", tracker.IssueKey(p, i))
	}
	rt, err := s.Q.GetRequestType(ctx, requestTypeID)
	if err != nil || rt.ProjectID != p.ID {
		return api.ServiceRequest{}, apperr.Invalid("requestTypeId", "unknown request type in %s", p.Key)
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var from *string
		channel := "agent"
		if sr, err := q.GetServiceRequest(ctx, i.ID); err == nil {
			channel = sr.Channel
			if sr.RequestTypeID != nil {
				if *sr.RequestTypeID == rt.ID {
					return nil
				}
				if old, err := q.GetRequestType(ctx, *sr.RequestTypeID); err == nil {
					from = &old.Name
				}
			}
		}
		if _, err := q.UpsertServiceRequest(ctx, dbq.UpsertServiceRequestParams{IssueID: i.ID, ProjectID: p.ID, RequestTypeID: &rt.ID, Channel: channel}); err != nil {
			return err
		}
		if err := history(ctx, q, i.ID, actor.User, "requestType", from, &rt.Name); err != nil {
			return err
		}
		// The goal may depend on the request type: emitting recomputes the SLA clocks.
		return s.Tracker.Emit(ctx, tx, actor, p, &i, "issue.updated", map[string]any{"fields": []string{"requestType"}})
	})
	if err != nil {
		return api.ServiceRequest{}, err
	}
	return s.Request(ctx, actor, key)
}

// AddParticipant shares a request with someone who may use the desk's portal: they then follow
// it like its reporter (and are emailed about it).
func (s *Service) AddParticipant(ctx context.Context, actor *authz.Actor, key, email string) (api.ServiceRequest, error) {
	i, p, access, err := s.agentRequest(ctx, actor, key, authz.ProjectBrowse)
	if err != nil {
		return api.ServiceRequest{}, err
	}
	if !tracker.CanEdit(access, actor, i) {
		return api.ServiceRequest{}, apperr.Forbidden("you cannot edit %s", tracker.IssueKey(p, i))
	}
	d, err := s.Q.GetServiceDesk(ctx, p.ID)
	if err != nil {
		return api.ServiceRequest{}, apperr.NotFound("service desk")
	}
	u, err := s.Q.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return api.ServiceRequest{}, apperr.Invalid("email", "nobody with that email can use the %s portal; add them as a customer first", d.PortalName)
	}
	role, err := s.Tracker.EffectiveOrgRole(ctx, u)
	if err != nil {
		return api.ServiceRequest{}, err
	}
	if ok, err := s.canRaise(ctx, u, role, p, d); err != nil {
		return api.ServiceRequest{}, err
	} else if !ok {
		return api.ServiceRequest{}, apperr.Invalid("email", "nobody with that email can use the %s portal; add them as a customer first", d.PortalName)
	}
	if i.ReporterID != nil && *i.ReporterID == u.ID {
		return api.ServiceRequest{}, apperr.Invalid("email", "%s raised this request", u.DisplayName)
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if ok, err := q.IsParticipant(ctx, dbq.IsParticipantParams{IssueID: i.ID, UserID: u.ID}); err != nil || ok {
			return err
		}
		if err := q.AddParticipant(ctx, dbq.AddParticipantParams{IssueID: i.ID, UserID: u.ID, AddedBy: actor.UserID()}); err != nil {
			return err
		}
		if err := history(ctx, q, i.ID, actor.User, "participants", nil, &u.DisplayName); err != nil {
			return err
		}
		data, _ := json.Marshal(map[string]any{"userId": u.ID})
		if _, err := s.Jobs.InsertTx(ctx, tx, jobs.ServiceDeskEvent{Event: "participant.added", ProjectID: p.ID, IssueID: i.ID, ActorID: actor.UserID(), ActorType: string(actor.Type), Data: data}, nil); err != nil {
			return err
		}
		return s.Tracker.Refresh(ctx, tx, actor, p, &i)
	})
	if err != nil {
		return api.ServiceRequest{}, err
	}
	return s.Request(ctx, actor, key)
}

// RemoveParticipant stops sharing a request with someone.
func (s *Service) RemoveParticipant(ctx context.Context, actor *authz.Actor, key string, userID uuid.UUID) error {
	i, p, access, err := s.agentRequest(ctx, actor, key, authz.ProjectBrowse)
	if err != nil {
		return err
	}
	if !tracker.CanEdit(access, actor, i) {
		return apperr.Forbidden("you cannot edit %s", tracker.IssueKey(p, i))
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		n, err := q.RemoveParticipant(ctx, dbq.RemoveParticipantParams{IssueID: i.ID, UserID: userID})
		if err != nil || n == 0 {
			return err
		}
		u, err := q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		if err := history(ctx, q, i.ID, actor.User, "participants", &u.DisplayName, nil); err != nil {
			return err
		}
		return s.Tracker.Refresh(ctx, tx, actor, p, &i)
	})
}
