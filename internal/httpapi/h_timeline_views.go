package httpapi

import (
	"context"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/tracker"
)

// Saved timelines: named groups of projects for the Timeline page (see tracker/timeline_views.go).

func (s *Server) timelineViewView(ctx context.Context, a *authz.Actor, v tracker.TimelineView) api.TimelineView {
	out := api.TimelineView{
		Id: v.View.ID, Name: v.View.Name, Description: v.View.Description, Shared: v.View.Shared,
		Mine:     a.User != nil && v.View.OwnerID == a.User.ID,
		Projects: make([]api.ProjectRef, 0, len(v.Projects)), HiddenProjects: v.HiddenProjects,
	}
	for _, p := range v.Projects {
		out.Projects = append(out.Projects, api.ProjectRef{Id: p.ID, Key: p.Key, Name: p.Name})
	}
	if u, err := s.Q.GetUser(ctx, v.View.OwnerID); err == nil {
		out.Owner = tracker.UserView(u)
	}
	return out
}

func timelineViewInput(b *api.TimelineViewInput) tracker.TimelineViewInput {
	return tracker.TimelineViewInput{Name: b.Name, Description: deref(b.Description), Shared: deref(b.Shared), Projects: b.Projects}
}

func (s *Server) ListTimelineViews(ctx context.Context, _ api.ListTimelineViewsRequestObject) (api.ListTimelineViewsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := s.Tracker.ListTimelineViews(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make(api.ListTimelineViews200JSONResponse, 0, len(vs))
	for _, v := range vs {
		out = append(out, s.timelineViewView(ctx, a, v))
	}
	return out, nil
}

func (s *Server) GetTimelineView(ctx context.Context, req api.GetTimelineViewRequestObject) (api.GetTimelineViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.GetTimelineView(ctx, a, req.ViewId)
	if err != nil {
		return nil, err
	}
	return api.GetTimelineView200JSONResponse(s.timelineViewView(ctx, a, v)), nil
}

func (s *Server) CreateTimelineView(ctx context.Context, req api.CreateTimelineViewRequestObject) (api.CreateTimelineViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.CreateTimelineView(ctx, a, timelineViewInput(req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateTimelineView201JSONResponse(s.timelineViewView(ctx, a, v)), nil
}

func (s *Server) UpdateTimelineView(ctx context.Context, req api.UpdateTimelineViewRequestObject) (api.UpdateTimelineViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.UpdateTimelineView(ctx, a, req.ViewId, timelineViewInput(req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateTimelineView200JSONResponse(s.timelineViewView(ctx, a, v)), nil
}

func (s *Server) DeleteTimelineView(ctx context.Context, req api.DeleteTimelineViewRequestObject) (api.DeleteTimelineViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteTimelineView(ctx, a, req.ViewId); err != nil {
		return nil, err
	}
	return api.DeleteTimelineView204Response{}, nil
}
