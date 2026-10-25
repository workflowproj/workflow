package servicedesk

import (
	"context"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
)

// personView shows someone in the portal: a name and picture, never an email address.
func personView(u dbq.User) api.PortalPerson {
	return api.PortalPerson{Id: u.ID, DisplayName: u.DisplayName, AvatarUrl: u.AvatarUrl}
}

func attachmentView(a dbq.Attachment) api.PortalAttachment {
	return api.PortalAttachment{
		Id: a.ID, Filename: a.Filename, ContentType: a.ContentType, Size: a.SizeBytes, CreatedAt: a.CreatedAt,
		Url: "/api/v1/portal/attachments/" + a.ID.String(),
	}
}

// portalLookups batch-loads what portal request cards show.
type portalLookups struct {
	projects map[uuid.UUID]dbq.Project
	desks    map[uuid.UUID]dbq.ServiceDesk
	statuses map[uuid.UUID]dbq.Status
	users    map[uuid.UUID]dbq.User
	requests map[uuid.UUID]dbq.ServiceRequest
	types    map[uuid.UUID]dbq.RequestType
}

func (s *Service) loadPortal(ctx context.Context, issues []dbq.Issue) (*portalLookups, error) {
	l := &portalLookups{
		projects: map[uuid.UUID]dbq.Project{}, desks: map[uuid.UUID]dbq.ServiceDesk{}, statuses: map[uuid.UUID]dbq.Status{},
		users: map[uuid.UUID]dbq.User{}, requests: map[uuid.UUID]dbq.ServiceRequest{}, types: map[uuid.UUID]dbq.RequestType{},
	}
	var projectIDs, statusIDs, userIDs, issueIDs []uuid.UUID
	for _, i := range issues {
		projectIDs = append(projectIDs, i.ProjectID)
		statusIDs = append(statusIDs, i.StatusID)
		issueIDs = append(issueIDs, i.ID)
		if i.ReporterID != nil {
			userIDs = append(userIDs, *i.ReporterID)
		}
	}
	projects, err := s.Q.ListProjectsByIDs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		l.projects[p.ID] = p
		if d, err := s.Q.GetServiceDesk(ctx, p.ID); err == nil {
			l.desks[p.ID] = d
		}
	}
	sts, err := s.Q.GetStatusesByIDs(ctx, statusIDs)
	if err != nil {
		return nil, err
	}
	for _, st := range sts {
		l.statuses[st.ID] = st
	}
	users, err := s.Q.GetUsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		l.users[u.ID] = u
	}
	srs, err := s.Q.ServiceRequestsFor(ctx, issueIDs)
	if err != nil {
		return nil, err
	}
	for _, sr := range srs {
		l.requests[sr.IssueID] = sr
		if sr.RequestTypeID != nil {
			if _, ok := l.types[*sr.RequestTypeID]; !ok {
				if rt, err := s.Q.GetRequestType(ctx, *sr.RequestTypeID); err == nil {
					l.types[rt.ID] = rt
				}
			}
		}
	}
	return l, nil
}

func (l *portalLookups) request(i dbq.Issue, role string) api.PortalRequest {
	p := l.projects[i.ProjectID]
	v := api.PortalRequest{
		Key: tracker.IssueKey(p, i), Summary: i.Summary, Status: tracker.StatusView(l.statuses[i.StatusID]),
		Desk: api.PortalDeskRef{ProjectKey: p.Key, Name: l.desks[p.ID].PortalName}, Resolution: i.Resolution,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, Role: api.PortalRequestRole(role),
		Reporter: api.PortalPerson{DisplayName: "Unknown"},
	}
	if v.Desk.Name == "" {
		v.Desk.Name = p.Name
	}
	if i.ReporterID != nil {
		if u, ok := l.users[*i.ReporterID]; ok {
			v.Reporter = personView(u)
		}
	}
	if sr, ok := l.requests[i.ID]; ok && sr.RequestTypeID != nil {
		if rt, ok := l.types[*sr.RequestTypeID]; ok {
			v.RequestType = requestTypeRef(rt)
		}
	}
	return v
}

func requestTypeRef(rt dbq.RequestType) api.RequestTypeRef {
	return api.RequestTypeRef{Id: rt.ID, Name: rt.Name, Icon: rt.Icon}
}
