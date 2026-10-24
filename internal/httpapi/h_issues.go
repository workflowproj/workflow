package httpapi

import (
	"context"
	"io"
	"mime"
	"strconv"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/search"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

func dateOpt(n interface {
	IsSpecified() bool
	IsNull() bool
}, get func() openapi_types.Date) tracker.Opt[time.Time] {
	if !n.IsSpecified() {
		return tracker.Opt[time.Time]{}
	}
	if n.IsNull() {
		return tracker.Null[time.Time]()
	}
	return tracker.Some(get().Time)
}

func (s *Server) detail(ctx context.Context, a *authz.Actor, i dbq.Issue) (api.IssueDetail, error) {
	return s.Tracker.IssueDetail(ctx, a, i)
}

func (s *Server) CreateIssue(ctx context.Context, req api.CreateIssueRequestObject) (api.CreateIssueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := tracker.IssueInput{
		ProjectKey: b.ProjectKey, TypeKey: b.TypeKey, Summary: b.Summary, Description: deref(b.Description),
		AssigneeID: b.AssigneeId, ParentKey: deref(b.ParentKey), SprintID: b.SprintId, StoryPoints: b.StoryPoints,
		Labels: deref(b.Labels), ReleaseIDs: deref(b.ReleaseIds), Fields: deref(b.Fields),
		OriginalEstimate: b.OriginalEstimate, RemainingEstimate: b.RemainingEstimate,
	}
	if b.Priority != nil {
		in.Priority = string(*b.Priority)
	}
	if b.StartDate != nil {
		in.StartDate = &b.StartDate.Time
	}
	if b.DueDate != nil {
		in.DueDate = &b.DueDate.Time
	}
	i, _, err := s.Tracker.CreateIssue(ctx, a, in)
	if err != nil {
		return nil, err
	}
	d, err := s.detail(ctx, a, i)
	return api.CreateIssue201JSONResponse(d), err
}

func (s *Server) GetIssue(ctx context.Context, req api.GetIssueRequestObject) (api.GetIssueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	i, _, _, err := s.Tracker.IssueFor(ctx, a, req.IssueKey, "")
	if err != nil {
		return nil, err
	}
	d, err := s.detail(ctx, a, i)
	return api.GetIssue200JSONResponse(d), err
}

func (s *Server) UpdateIssue(ctx context.Context, req api.UpdateIssueRequestObject) (api.UpdateIssueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	patch := tracker.IssuePatch{
		Version: b.Version, TypeKey: b.TypeKey, Summary: b.Summary, Description: b.Description,
		AssigneeID: opt(b.AssigneeId), ReporterID: opt(b.ReporterId), ParentKey: opt(b.ParentKey), SprintID: opt(b.SprintId),
		StoryPoints: opt(b.StoryPoints), Labels: b.Labels, ReleaseIDs: b.ReleaseIds, Fields: deref(b.Fields),
		StartDate: dateOpt(b.StartDate, b.StartDate.MustGet), DueDate: dateOpt(b.DueDate, b.DueDate.MustGet),
		OriginalEstimate: opt(b.OriginalEstimate), RemainingEstimate: opt(b.RemainingEstimate),
	}
	if b.Priority != nil {
		p := string(*b.Priority)
		patch.Priority = &p
	}
	i, _, err := s.Tracker.UpdateIssue(ctx, a, req.IssueKey, patch)
	if err != nil {
		return nil, err
	}
	d, err := s.detail(ctx, a, i)
	return api.UpdateIssue200JSONResponse(d), err
}

func (s *Server) DeleteIssue(ctx context.Context, req api.DeleteIssueRequestObject) (api.DeleteIssueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteIssue(ctx, a, req.IssueKey); err != nil {
		return nil, err
	}
	return api.DeleteIssue204Response{}, nil
}

func screenView(fields []tracker.ScreenFieldInfo) []api.ScreenFieldInfo {
	out := []api.ScreenFieldInfo{}
	for _, f := range fields {
		v := api.ScreenFieldInfo{Field: f.Field, Name: f.Name, Type: f.Type, Required: f.Required}
		if len(f.Options) > 0 {
			o := f.Options
			v.Options = &o
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) ListTransitions(ctx context.Context, req api.ListTransitionsRequestObject) (api.ListTransitionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := s.Tracker.AvailableTransitions(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	out := api.ListTransitions200JSONResponse{}
	for _, t := range ts {
		out = append(out, api.AvailableTransition{
			Id: t.Transition.ID, Name: t.Transition.Name, Screen: screenView(t.Screen),
			To: api.Status{Id: t.To.ID, Name: t.To.Name, Category: api.StatusCategory(t.To.Category)},
		})
	}
	return out, nil
}

func (s *Server) TransitionIssue(ctx context.Context, req api.TransitionIssueRequestObject) (api.TransitionIssueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	i, _, err := s.Tracker.Transition(ctx, a, req.IssueKey, tracker.TransitionInput{
		TransitionID: b.TransitionId, Fields: deref(b.Fields), Comment: deref(b.Comment), Version: b.Version,
	})
	if err != nil {
		return nil, err
	}
	d, err := s.detail(ctx, a, i)
	return api.TransitionIssue200JSONResponse(d), err
}

func (s *Server) MoveIssue(ctx context.Context, req api.MoveIssueRequestObject) (api.MoveIssueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	i, _, err := s.Tracker.Move(ctx, a, req.IssueKey, tracker.MoveInput{
		StatusID: b.StatusId, TransitionID: b.TransitionId, SprintID: opt(b.SprintId), AfterIssueID: b.AfterIssueId, BeforeIssueID: b.BeforeIssueId,
	})
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.IssueView(ctx, i)
	return api.MoveIssue200JSONResponse(v), err
}

// ---------------------------------------------------------------------------
// Comments, history, links, watchers

func (s *Server) commentView(ctx context.Context, a *authz.Actor, access authz.Access, c dbq.Comment, users map[uuid.UUID]dbq.User) api.Comment {
	own := c.AuthorID != nil && a.UserID() != nil && *c.AuthorID == *a.UserID()
	v := api.Comment{Id: c.ID, Body: c.Body, Source: c.Source, CreatedAt: c.CreatedAt, EditedAt: c.EditedAt, CanEdit: own || access.Has(authz.CommentModerate)}
	if c.AuthorID != nil {
		u, ok := users[*c.AuthorID]
		if !ok {
			if fetched, err := s.Q.GetUser(ctx, *c.AuthorID); err == nil {
				u, ok = fetched, true
				users[u.ID] = u
			}
		}
		if ok {
			uv := tracker.UserView(u)
			v.Author = &uv
		}
	}
	return v
}

func (s *Server) ListComments(ctx context.Context, req api.ListCommentsRequestObject) (api.ListCommentsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cs, access, err := s.Tracker.ListComments(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	users := map[uuid.UUID]dbq.User{}
	out := api.ListComments200JSONResponse{}
	for _, c := range cs {
		out = append(out, s.commentView(ctx, a, access, c, users))
	}
	return out, nil
}

func (s *Server) AddComment(ctx context.Context, req api.AddCommentRequestObject) (api.AddCommentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.Tracker.AddComment(ctx, a, req.IssueKey, req.Body.Body)
	if err != nil {
		return nil, err
	}
	return api.AddComment201JSONResponse(s.commentView(ctx, a, authz.Access{}, c, map[uuid.UUID]dbq.User{})), nil
}

func (s *Server) UpdateComment(ctx context.Context, req api.UpdateCommentRequestObject) (api.UpdateCommentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.Tracker.UpdateComment(ctx, a, req.CommentId, req.Body.Body)
	if err != nil {
		return nil, err
	}
	return api.UpdateComment200JSONResponse(s.commentView(ctx, a, authz.Access{}, c, map[uuid.UUID]dbq.User{})), nil
}

func (s *Server) DeleteComment(ctx context.Context, req api.DeleteCommentRequestObject) (api.DeleteCommentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteComment(ctx, a, req.CommentId); err != nil {
		return nil, err
	}
	return api.DeleteComment204Response{}, nil
}

func (s *Server) ListIssueHistory(ctx context.Context, req api.ListIssueHistoryRequestObject) (api.ListIssueHistoryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Tracker.History(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	users := map[uuid.UUID]*api.User{}
	out := api.ListIssueHistory200JSONResponse{}
	for _, h := range rows {
		v := api.HistoryEntry{Id: h.ID, ActorType: h.ActorType, Field: h.Field, From: h.FromValue, To: h.ToValue, CreatedAt: h.CreatedAt}
		if h.ActorID != nil {
			if _, ok := users[*h.ActorID]; !ok {
				if u, err := s.Q.GetUser(ctx, *h.ActorID); err == nil {
					uv := tracker.UserView(u)
					users[*h.ActorID] = &uv
				}
			}
			v.Actor = users[*h.ActorID]
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) CreateIssueLink(ctx context.Context, req api.CreateIssueLinkRequestObject) (api.CreateIssueLinkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	inward := req.Body.Direction != nil && string(*req.Body.Direction) == string(api.LinkDirectionInward)
	l, target, err := s.Tracker.CreateLink(ctx, a, req.IssueKey, string(req.Body.Type), req.Body.TargetKey, inward)
	if err != nil {
		return nil, err
	}
	tv, err := s.Tracker.IssueView(ctx, target)
	if err != nil {
		return nil, err
	}
	dir := api.LinkDirectionOutward
	if inward {
		dir = api.LinkDirectionInward
	}
	return api.CreateIssueLink201JSONResponse{
		Id: l.ID, Type: api.LinkType(l.LinkType), Direction: dir, Label: tracker.LinkLabel(l.LinkType, !inward),
		Issue: api.IssueRef{Id: tv.Id, Key: tv.Key, Summary: tv.Summary, Type: tv.Type, Status: tv.Status},
	}, nil
}

func (s *Server) DeleteIssueLink(ctx context.Context, req api.DeleteIssueLinkRequestObject) (api.DeleteIssueLinkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteLink(ctx, a, req.LinkId); err != nil {
		return nil, err
	}
	return api.DeleteIssueLink204Response{}, nil
}

func (s *Server) ListWatchers(ctx context.Context, req api.ListWatchersRequestObject) (api.ListWatchersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	us, err := s.Tracker.Watchers(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	out := api.ListWatchers200JSONResponse{}
	for _, u := range us {
		out = append(out, tracker.UserView(u))
	}
	return out, nil
}

func (s *Server) AddWatcher(ctx context.Context, req api.AddWatcherRequestObject) (api.AddWatcherResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.AddWatcher(ctx, a, req.IssueKey, req.Body.UserId); err != nil {
		return nil, err
	}
	return api.AddWatcher204Response{}, nil
}

func (s *Server) RemoveWatcher(ctx context.Context, req api.RemoveWatcherRequestObject) (api.RemoveWatcherResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.RemoveWatcher(ctx, a, req.IssueKey, req.UserId); err != nil {
		return nil, err
	}
	return api.RemoveWatcher204Response{}, nil
}

// ---------------------------------------------------------------------------
// Attachments

func (s *Server) UploadAttachment(ctx context.Context, req api.UploadAttachmentRequestObject) (api.UploadAttachmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	for {
		part, err := req.Body.NextPart()
		if err == io.EOF {
			return nil, apperr.Invalid("file", "no file part in the upload")
		}
		if err != nil {
			return nil, apperr.BadRequest("invalid multipart body: %v", err)
		}
		if part.FormName() != "file" {
			continue
		}
		size := int64(-1)
		if v := part.Header.Get("Content-Length"); v != "" {
			size, _ = strconv.ParseInt(v, 10, 64)
		}
		ct := part.Header.Get("Content-Type")
		if mt, _, err := mime.ParseMediaType(ct); err == nil {
			ct = mt
		}
		att, err := s.Tracker.Attach(ctx, a, req.IssueKey, part.FileName(), ct, size, part)
		if err != nil {
			return nil, err
		}
		v := api.Attachment{Id: att.ID, Filename: att.Filename, ContentType: att.ContentType, Size: att.SizeBytes, CreatedAt: att.CreatedAt, Url: "/api/v1/attachments/" + att.ID.String()}
		if a.User != nil {
			uv := tracker.UserView(*a.User)
			v.Uploader = &uv
		}
		return api.UploadAttachment201JSONResponse(v), nil
	}
}

func (s *Server) DownloadAttachment(ctx context.Context, req api.DownloadAttachmentRequestObject) (api.DownloadAttachmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	att, rc, u, err := s.Tracker.OpenAttachment(ctx, a, req.AttachmentId)
	if err != nil {
		return nil, err
	}
	if u != nil {
		return api.DownloadAttachment302Response{Headers: api.DownloadAttachment302ResponseHeaders{Location: ptr(u.String())}}, nil
	}
	if w, _ := httpFrom(ctx); w != nil {
		w.Header().Set("Content-Type", att.ContentType)
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	return api.DownloadAttachment200ApplicationoctetStreamResponse{
		Body: rc, ContentLength: att.SizeBytes,
		Headers: api.DownloadAttachment200ResponseHeaders{ContentDisposition: ptr(mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))},
	}, nil
}

func (s *Server) DeleteAttachment(ctx context.Context, req api.DeleteAttachmentRequestObject) (api.DeleteAttachmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteAttachment(ctx, a, req.AttachmentId); err != nil {
		return nil, err
	}
	return api.DeleteAttachment204Response{}, nil
}

// ---------------------------------------------------------------------------
// Search & filters

func (s *Server) SearchIssues(ctx context.Context, req api.SearchIssuesRequestObject) (api.SearchIssuesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	limit := limitOr(req.Params.Limit, 50)
	offset := 0
	if req.Params.Offset != nil {
		offset = max(*req.Params.Offset, 0)
	}
	issues, total, err := s.Tracker.Search(ctx, a, deref(req.Params.Jql), limit, offset)
	if err != nil {
		return nil, err
	}
	views, err := s.Tracker.IssueViews(ctx, issues)
	if err != nil {
		return nil, err
	}
	return api.SearchIssues200JSONResponse{Issues: views, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Server) GetSearchMetadata(ctx context.Context, _ api.GetSearchMetadataRequestObject) (api.GetSearchMetadataResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	out := api.GetSearchMetadata200JSONResponse{Fields: []api.SearchField{}, Functions: search.Functions}
	for _, f := range search.Fields {
		sf := api.SearchField{Name: f.Name, Type: f.Type, Operators: f.Operators}
		switch f.Name {
		case "priority":
			sf.Values = &[]string{"Highest", "High", "Medium", "Low", "Lowest"}
		case "statusCategory":
			sf.Values = &[]string{"To Do", "In Progress", "Done"}
		case "status":
			sts, _ := s.Q.ListStatuses(ctx)
			vals := []string{}
			for _, st := range sts {
				vals = append(vals, st.Name)
			}
			sf.Values = &vals
		}
		out.Fields = append(out.Fields, sf)
	}
	defs, _ := s.Q.ListFieldDefinitions(ctx)
	for _, d := range defs {
		ops := []string{"=", "!=", "in", "not in", "is", "is not"}
		switch d.Type {
		case "number", "date":
			ops = []string{"=", "!=", ">", ">=", "<", "<=", "is", "is not"}
		case "text", "url":
			ops = []string{"~", "!~", "=", "!=", "is", "is not"}
		}
		out.Fields = append(out.Fields, api.SearchField{Name: d.Key, Type: d.Type, Operators: ops})
	}
	return out, nil
}

func (s *Server) filterView(ctx context.Context, a *authz.Actor, f dbq.SavedFilter) api.SavedFilter {
	v := api.SavedFilter{Id: f.ID, Name: f.Name, Description: f.Description, Jql: f.Jql, Shared: f.Shared, Mine: a.User != nil && f.OwnerID == a.User.ID}
	if u, err := s.Q.GetUser(ctx, f.OwnerID); err == nil {
		v.Owner = tracker.UserView(u)
	}
	return v
}

func (s *Server) ListFilters(ctx context.Context, _ api.ListFiltersRequestObject) (api.ListFiltersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	fs, err := s.Q.ListFilters(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListFilters200JSONResponse{}
	for _, f := range fs {
		out = append(out, s.filterView(ctx, a, f))
	}
	return out, nil
}

func (s *Server) CreateFilter(ctx context.Context, req api.CreateFilterRequestObject) (api.CreateFilterResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	f, err := s.Tracker.CreateFilter(ctx, a, b.Name, deref(b.Description), b.Jql, deref(b.Shared))
	if err != nil {
		return nil, err
	}
	return api.CreateFilter201JSONResponse(s.filterView(ctx, a, f)), nil
}

func (s *Server) UpdateFilter(ctx context.Context, req api.UpdateFilterRequestObject) (api.UpdateFilterResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	f, err := s.Tracker.UpdateFilter(ctx, a, req.FilterId, b.Name, deref(b.Description), b.Jql, deref(b.Shared))
	if err != nil {
		return nil, err
	}
	return api.UpdateFilter200JSONResponse(s.filterView(ctx, a, f)), nil
}

func (s *Server) DeleteFilter(ctx context.Context, req api.DeleteFilterRequestObject) (api.DeleteFilterResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteFilter(ctx, a, req.FilterId); err != nil {
		return nil, err
	}
	return api.DeleteFilter204Response{}, nil
}
