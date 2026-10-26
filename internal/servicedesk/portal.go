package servicedesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
)

// ---------------------------------------------------------------------------
// Who may use the portal

// portalUser is the signed-in person using the portal.
func portalUser(actor *authz.Actor) (dbq.User, error) {
	if actor == nil || actor.User == nil {
		return dbq.User{}, apperr.Unauthorized()
	}
	if actor.User.Kind != "human" {
		return dbq.User{}, apperr.Forbidden("the portal is for people, not service accounts")
	}
	return *actor.User, nil
}

// canRaise reports whether a person may raise requests in a desk: they are on its customer list,
// the desk is open to the organisation and they are a member, or they are one of its agents.
func (s *Service) canRaise(ctx context.Context, u dbq.User, role string, p dbq.Project, d dbq.ServiceDesk) (bool, error) {
	if !u.Active || u.DeletedAt != nil || u.Kind != "human" {
		return false, nil
	}
	ok, err := s.Q.IsServiceCustomer(ctx, dbq.IsServiceCustomerParams{ProjectID: p.ID, UserID: u.ID})
	if err != nil || ok {
		return ok, err
	}
	if u.OrgRole == authz.OrgCustomer {
		return false, nil
	}
	if d.PortalAccess == "organisation" && (role == "member" || role == "admin") {
		return true, nil
	}
	access, err := s.Tracker.Authz.Access(ctx, authz.UserActor(u, role), p)
	return access.CanBrowse(), err
}

// portalDesk loads a desk the caller may raise requests in ("not found" otherwise).
func (s *Service) portalDesk(ctx context.Context, actor *authz.Actor, ref string) (dbq.User, dbq.Project, dbq.ServiceDesk, error) {
	u, err := portalUser(actor)
	if err != nil {
		return u, dbq.Project{}, dbq.ServiceDesk{}, err
	}
	p, err := s.Tracker.ResolveProject(ctx, s.Q, ref)
	if err != nil || !tracker.IsServiceProject(p) || p.ArchivedAt != nil {
		return u, p, dbq.ServiceDesk{}, apperr.NotFound("service desk")
	}
	d, err := s.Q.GetServiceDesk(ctx, p.ID)
	if err != nil {
		return u, p, d, apperr.NotFound("service desk")
	}
	ok, err := s.canRaise(ctx, u, actor.OrgRole, p, d)
	if err != nil {
		return u, p, d, err
	}
	if !ok {
		return u, p, d, apperr.NotFound("service desk")
	}
	return u, p, d, nil
}

// portalRequest is a request as one person may see it in the portal.
type portalRequest struct {
	user    dbq.User
	issue   dbq.Issue
	project dbq.Project
	desk    dbq.ServiceDesk
	request dbq.ServiceRequest
	role    string // reporter, participant or approver
}

// requestFor loads a request the caller may see in the portal: they reported it, it is shared
// with them, or they are (or were) asked to approve it. Issues without a request type, issues of
// other projects and everyone else's requests are all "not found", so the answer reveals nothing.
func (s *Service) requestFor(ctx context.Context, actor *authz.Actor, key string) (portalRequest, error) {
	var r portalRequest
	u, err := portalUser(actor)
	if err != nil {
		return r, err
	}
	r.user = u
	notFound := apperr.NotFound("request")
	if !u.Active || u.DeletedAt != nil {
		return r, notFound
	}
	if r.issue, err = s.Tracker.ResolveIssue(ctx, s.Q, key); err != nil {
		return r, notFound
	}
	if r.project, err = s.Q.GetProject(ctx, r.issue.ProjectID); err != nil || !tracker.IsServiceProject(r.project) {
		return r, notFound
	}
	if r.desk, err = s.Q.GetServiceDesk(ctx, r.project.ID); err != nil {
		return r, notFound
	}
	if r.request, err = s.Q.GetServiceRequest(ctx, r.issue.ID); err != nil || r.request.RequestTypeID == nil {
		return r, notFound
	}
	switch {
	case r.issue.ReporterID != nil && *r.issue.ReporterID == u.ID:
		r.role = "reporter"
	default:
		ok, err := s.Q.IsParticipant(ctx, dbq.IsParticipantParams{IssueID: r.issue.ID, UserID: u.ID})
		if err != nil {
			return r, err
		}
		if ok {
			r.role = "participant"
			break
		}
		if ok, err = s.Q.IsApprover(ctx, dbq.IsApproverParams{IssueID: r.issue.ID, UserID: u.ID}); err != nil {
			return r, err
		}
		if !ok {
			return r, notFound
		}
		r.role = "approver"
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// The help centre and its forms

// ListDesks is the help centre: the desks the caller may raise requests in.
func (s *Service) ListDesks(ctx context.Context, actor *authz.Actor) ([]api.PortalDesk, error) {
	u, err := portalUser(actor)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListServiceDesks(ctx)
	if err != nil {
		return nil, err
	}
	out := []api.PortalDesk{}
	for _, r := range rows {
		ok, err := s.canRaise(ctx, u, actor.OrgRole, r.Project, r.ServiceDesk)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		n, err := s.Q.CountRequestTypes(ctx, r.Project.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, api.PortalDesk{ProjectKey: r.Project.Key, Name: r.ServiceDesk.PortalName, Description: r.ServiceDesk.PortalDescription, RequestTypeCount: int(n)})
	}
	return out, nil
}

// PortalDesk returns a desk's request types with their forms.
func (s *Service) PortalDesk(ctx context.Context, actor *authz.Actor, ref string) (api.PortalDeskDetail, error) {
	_, p, d, err := s.portalDesk(ctx, actor, ref)
	if err != nil {
		return api.PortalDeskDetail{}, err
	}
	rts, err := s.Q.ListRequestTypes(ctx, p.ID)
	if err != nil {
		return api.PortalDeskDetail{}, err
	}
	defs, err := s.Q.ListFieldDefinitions(ctx)
	if err != nil {
		return api.PortalDeskDetail{}, err
	}
	out := api.PortalDeskDetail{ProjectKey: p.Key, Name: d.PortalName, Description: d.PortalDescription, RequestTypes: []api.PortalRequestType{}}
	for _, rt := range rts {
		it, err := s.Q.GetIssueType(ctx, rt.IssueTypeID)
		if err != nil {
			return out, err
		}
		fields, _ := resolveForm(defs, p, it, decodeForm(rt.Form), false)
		v := api.PortalRequestType{Id: rt.ID, Name: rt.Name, Description: rt.Description, Icon: rt.Icon, Group: rt.PortalGroup, Fields: []api.PortalFormField{}, NeedsApproval: decodeApproval(rt.Approval) != nil}
		for _, f := range fields {
			pf := api.PortalFormField{Key: f.Field, Label: f.Label, Type: f.Type, Required: f.Required}
			if f.Help != "" {
				pf.Help = ptr(f.Help)
			}
			if f.Options != nil {
				pf.Options = ptr(f.Options)
			}
			v.Fields = append(v.Fields, pf)
		}
		out.RequestTypes = append(out.RequestTypes, v)
	}
	notes, err := s.Q.CountPublishedReleaseNotes(ctx, p.ID)
	if err != nil {
		return out, err
	}
	out.WhatsNew = notes > 0
	return out, nil
}

// formValues turns submitted form values into the issue they create.
func formValues(fields []resolvedField, rt dbq.RequestType, values map[string]any) (tracker.IssueInput, error) {
	in := tracker.IssueInput{Fields: map[string]any{}}
	allowed := map[string]bool{}
	for _, f := range fields {
		allowed[f.Field] = true
	}
	for k := range values {
		if !allowed[k] {
			return in, apperr.Invalid("values."+k, "%q is not on this form", k)
		}
	}
	for _, f := range fields {
		v, present := values[f.Field]
		if present && isEmpty(v) {
			present = false
		}
		if !present {
			if f.Required {
				return in, apperr.Invalid("values."+f.Field, "%s is required", f.Label)
			}
			continue
		}
		bad := func(format string, args ...any) error {
			return apperr.Invalid("values."+f.Field, "%s: %s", f.Label, fmt.Sprintf(format, args...))
		}
		switch f.Field {
		case "summary":
			str, ok := v.(string)
			if str = strings.TrimSpace(str); !ok || len(str) > 255 {
				return in, bad("at most 255 characters")
			}
			in.Summary = str
		case "description":
			str, ok := v.(string)
			if !ok || len(str) > 50000 {
				return in, bad("at most 50,000 characters")
			}
			in.Description = strings.TrimSpace(str)
		case "priority":
			str, _ := v.(string)
			if !slices.Contains(priorityOptions, str) {
				return in, bad("must be one of %s", strings.Join(priorityOptions, ", "))
			}
			in.Priority = str
		case "dueDate":
			str, _ := v.(string)
			d, err := time.Parse(time.DateOnly, str)
			if err != nil {
				return in, bad("expected a date (YYYY-MM-DD)")
			}
			in.DueDate = &d
		default:
			in.Fields[f.Field] = v // the tracker checks custom field values
		}
	}
	if in.Summary == "" {
		in.Summary = rt.Name
	}
	return in, nil
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	}
	return false
}

// CreateRequest raises a request from a request type's form. With an approval configured, the
// request then waits for it in the "Pending approval" status.
func (s *Service) CreateRequest(ctx context.Context, actor *authz.Actor, ref string, in api.PortalRequestCreate) (api.PortalRequestDetail, error) {
	u, p, _, err := s.portalDesk(ctx, actor, ref)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	rt, err := s.Q.GetRequestType(ctx, in.RequestTypeId)
	if err != nil || rt.ProjectID != p.ID {
		return api.PortalRequestDetail{}, apperr.Invalid("requestTypeId", "unknown request type")
	}
	it, err := s.Q.GetIssueType(ctx, rt.IssueTypeID)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	defs, err := s.Q.ListFieldDefinitions(ctx)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	fields, _ := resolveForm(defs, p, it, decodeForm(rt.Form), false)
	issueIn, err := formValues(fields, rt, in.Values)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	issueIn.ProjectKey, issueIn.TypeKey = p.Key, it.Key
	approval := decodeApproval(rt.Approval)
	waiting := false
	issueIn.OnCreate = func(ctx context.Context, tx pgx.Tx, q *dbq.Queries, issue dbq.Issue) error {
		if _, err := q.UpsertServiceRequest(ctx, dbq.UpsertServiceRequestParams{IssueID: issue.ID, ProjectID: p.ID, RequestTypeID: &rt.ID, Channel: "portal"}); err != nil {
			return err
		}
		if approval != nil {
			var err error
			waiting, err = s.startApproval(ctx, tx, q, p, issue, *approval)
			return err
		}
		return nil
	}
	issue, _, err := s.Tracker.CreateIssue(ctx, authz.PortalActor(u, p.ID), issueIn)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	if waiting {
		if moved, err := s.Tracker.TransitionToStatus(ctx, deskActor(u, "portal"), issue.ID, statusPendingApproval, ""); err != nil || !moved {
			s.Log.Warn("request needs an approval but could not move to "+statusPendingApproval, "issue", issue.ID, "error", err)
		}
	}
	return s.PortalRequest(ctx, actor, tracker.IssueKey(p, issue))
}

// ---------------------------------------------------------------------------
// My requests

// ListRequests lists the caller's requests: raised by them or shared with them.
func (s *Service) ListRequests(ctx context.Context, actor *authz.Actor, status, project string, limit int) ([]api.PortalRequest, error) {
	u, err := portalUser(actor)
	if err != nil {
		return nil, err
	}
	params := dbq.PortalRequestsParams{UserID: &u.ID, Lim: int32(limit)}
	switch status {
	case "open":
		params.Done = ptr(false)
	case "closed":
		params.Done = ptr(true)
	}
	if project != "" {
		p, err := s.Tracker.ResolveProject(ctx, s.Q, project)
		if err != nil {
			return []api.PortalRequest{}, nil
		}
		params.ProjectID = &p.ID
	}
	rows, err := s.Q.PortalRequests(ctx, params)
	if err != nil {
		return nil, err
	}
	issues := make([]dbq.Issue, len(rows))
	for k, r := range rows {
		issues[k] = r.Issue
	}
	return s.portalList(ctx, u, issues, "")
}

// ListApprovals lists the requests waiting for the caller's decision.
func (s *Service) ListApprovals(ctx context.Context, actor *authz.Actor, limit int) ([]api.PortalRequest, error) {
	u, err := portalUser(actor)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.PendingApprovalRequests(ctx, dbq.PendingApprovalRequestsParams{UserID: u.ID, Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	issues := make([]dbq.Issue, len(rows))
	for k, r := range rows {
		issues[k] = r.Issue
	}
	return s.portalList(ctx, u, issues, "approver")
}

// portalList renders requests for the list pages in a fixed number of queries.
func (s *Service) portalList(ctx context.Context, u dbq.User, issues []dbq.Issue, role string) ([]api.PortalRequest, error) {
	out := []api.PortalRequest{}
	if len(issues) == 0 {
		return out, nil
	}
	l, err := s.loadPortal(ctx, issues)
	if err != nil {
		return nil, err
	}
	for _, i := range issues {
		r := role
		if r == "" {
			r = "participant"
			if i.ReporterID != nil && *i.ReporterID == u.ID {
				r = "reporter"
			}
		}
		out = append(out, l.request(i, r))
	}
	return out, nil
}

// PortalRequest returns a request as the caller may see it in the portal: its form values, the public
// conversation, the files customers added, approvals and participants.
func (s *Service) PortalRequest(ctx context.Context, actor *authz.Actor, key string) (api.PortalRequestDetail, error) {
	r, err := s.requestFor(ctx, actor, key)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	return s.requestDetail(ctx, r)
}

func (s *Service) requestDetail(ctx context.Context, r portalRequest) (api.PortalRequestDetail, error) {
	l, err := s.loadPortal(ctx, []dbq.Issue{r.issue})
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	base := l.request(r.issue, r.role)
	d := api.PortalRequestDetail{
		Key: base.Key, Summary: base.Summary, Status: base.Status, RequestType: base.RequestType, Desk: base.Desk,
		Reporter: base.Reporter, Resolution: base.Resolution, CreatedAt: base.CreatedAt, UpdatedAt: base.UpdatedAt,
		Role: api.PortalRequestDetailRole(r.role), Fields: []api.PortalFieldValue{}, Comments: []api.PortalComment{},
		Attachments: []api.PortalAttachment{}, Participants: []api.PortalPerson{}, CanComment: r.role != "approver",
	}
	// The values of the request type's form fields, as they are now.
	if rt, err := s.Q.GetRequestType(ctx, *r.request.RequestTypeID); err == nil {
		it, _ := s.Q.GetIssueType(ctx, r.issue.TypeID)
		defs, err := s.Q.ListFieldDefinitions(ctx)
		if err != nil {
			return d, err
		}
		fields, _ := resolveForm(defs, r.project, it, decodeForm(rt.Form), false)
		d.Fields = fieldValues(fields, r.issue)
	}
	comments, err := s.Q.ListPublicComments(ctx, r.issue.ID)
	if err != nil {
		return d, err
	}
	var authorIDs []uuid.UUID
	for _, c := range comments {
		if c.AuthorID != nil {
			authorIDs = append(authorIDs, *c.AuthorID)
		}
	}
	authors, err := s.Q.GetUsersByIDs(ctx, authorIDs)
	if err != nil {
		return d, err
	}
	for _, c := range comments {
		if c.Internal {
			continue // never; the query already leaves them out
		}
		pc := api.PortalComment{Id: c.ID, Body: c.Body, CreatedAt: c.CreatedAt, FromSupport: c.Source != "portal", Author: api.PortalPerson{DisplayName: "Support"}}
		if c.AuthorID != nil {
			if k := slices.IndexFunc(authors, func(u dbq.User) bool { return u.ID == *c.AuthorID }); k >= 0 {
				pc.Author = personView(authors[k])
			}
		}
		d.Comments = append(d.Comments, pc)
	}
	atts, err := s.Q.ListPublicAttachments(ctx, r.issue.ID)
	if err != nil {
		return d, err
	}
	for _, a := range atts {
		d.Attachments = append(d.Attachments, attachmentView(a))
	}
	if d.Approvals, err = s.approvalsView(ctx, r.issue.ID, &r.user.ID); err != nil {
		return d, err
	}
	parts, err := s.Q.ListParticipants(ctx, r.issue.ID)
	if err != nil {
		return d, err
	}
	for _, u := range parts {
		d.Participants = append(d.Participants, personView(u))
	}
	return d, nil
}

// fieldValues shows a request's form fields with their current values.
func fieldValues(fields []resolvedField, i dbq.Issue) []api.PortalFieldValue {
	out := []api.PortalFieldValue{}
	custom := map[string]any{}
	_ = json.Unmarshal(i.Fields, &custom)
	for _, f := range fields {
		var value string
		switch f.Field {
		case "summary":
			value = i.Summary
		case "description":
			value = i.Description
		case "priority":
			value = strings.ToUpper(i.Priority[:1]) + i.Priority[1:]
		case "dueDate":
			if i.DueDate != nil {
				value = i.DueDate.Format(time.DateOnly)
			}
		default:
			value = displayValue(custom[f.Field])
		}
		if value == "" {
			continue
		}
		t := f.Type
		out = append(out, api.PortalFieldValue{Label: f.Label, Value: value, Type: &t})
	}
	return out
}

func displayValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "Yes"
		}
		return "No"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for k, e := range x {
			parts[k] = fmt.Sprint(e)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// ---------------------------------------------------------------------------
// Replies, files and approvals from the portal

// AddComment replies on a request from the portal (always public). A reply from the customer
// while the request waits for them hands it back to support.
func (s *Service) AddComment(ctx context.Context, actor *authz.Actor, key, body string) (api.PortalComment, error) {
	r, err := s.requestFor(ctx, actor, key)
	if err != nil {
		return api.PortalComment{}, err
	}
	if r.role == "approver" {
		return api.PortalComment{}, apperr.Forbidden("approvers decide on a request; they do not join its conversation")
	}
	c, err := s.Tracker.AddCommentWith(ctx, authz.PortalActor(r.user, r.project.ID), r.issue.ID.String(), tracker.CommentInput{Body: body, Visibility: tracker.VisibilityPublic})
	if err != nil {
		return api.PortalComment{}, err
	}
	if st, err := s.Q.GetStatusesByIDs(ctx, []uuid.UUID{r.issue.StatusID}); err == nil && len(st) == 1 && strings.EqualFold(st[0].Name, statusWaitingForCustomer) {
		if _, err := s.Tracker.TransitionToStatus(ctx, deskActor(r.user, "portal"), r.issue.ID, statusWaitingForSupport, ""); err != nil {
			s.Log.Warn("customer replied but the request did not go back to support", "issue", r.issue.ID, "error", err)
		}
	}
	return api.PortalComment{Id: c.ID, Author: personView(r.user), Body: c.Body, CreatedAt: c.CreatedAt}, nil
}

// Attach adds a file to a request from the portal (portal files are the ones customers see).
func (s *Service) Attach(ctx context.Context, actor *authz.Actor, key, filename, contentType string, size int64, body io.Reader) (api.PortalAttachment, error) {
	r, err := s.requestFor(ctx, actor, key)
	if err != nil {
		return api.PortalAttachment{}, err
	}
	if r.role == "approver" {
		return api.PortalAttachment{}, apperr.Forbidden("approvers cannot add files to a request")
	}
	a, err := s.Tracker.Attach(ctx, authz.PortalActor(r.user, r.project.ID), r.issue.ID.String(), filename, contentType, size, body)
	if err != nil {
		return api.PortalAttachment{}, err
	}
	return attachmentView(a), nil
}

// OpenAttachment returns a file customers may download: one added in the portal to a request
// the caller may see.
func (s *Service) OpenAttachment(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Attachment, io.ReadCloser, *url.URL, error) {
	a, err := s.Q.GetAttachment(ctx, id)
	if err != nil || !a.Public {
		return a, nil, nil, apperr.NotFound("attachment")
	}
	if _, err := s.requestFor(ctx, actor, a.IssueID.String()); err != nil {
		return a, nil, nil, apperr.NotFound("attachment")
	}
	rc, u, err := s.Tracker.Storage.Open(ctx, a.ObjectKey, a.Filename)
	return a, rc, u, err
}

// Decide records the caller's decision on a request waiting for their approval.
func (s *Service) Decide(ctx context.Context, actor *authz.Actor, key string, d api.ApprovalDecision) (api.PortalRequestDetail, error) {
	r, err := s.requestFor(ctx, actor, key)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	if !d.Decision.Valid() {
		return api.PortalRequestDetail{}, apperr.Invalid("decision", "decision is approve or decline")
	}
	approvalID, err := s.pendingApprovalFor(ctx, r.issue.ID, r.user.ID)
	if err != nil {
		return api.PortalRequestDetail{}, err
	}
	if err := s.decide(ctx, r.user, r.issue.ID, approvalID, d.Decision == api.ApprovalDecisionDecisionApprove, deref(d.Comment)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.PortalRequestDetail{}, apperr.NotFound("approval")
		}
		return api.PortalRequestDetail{}, err
	}
	return s.PortalRequest(ctx, actor, key)
}
