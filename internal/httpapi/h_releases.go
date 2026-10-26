package httpapi

import (
	"context"

	"github.com/workflowproj/workflow/internal/api"
)

// ---------------------------------------------------------------------------
// The release page: detail, readiness, burnup, notes and tag settings

func (s *Server) GetRelease(ctx context.Context, req api.GetReleaseRequestObject) (api.GetReleaseResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.Tracker.ReleaseDetail(ctx, a, req.ReleaseId)
	if err != nil {
		return nil, err
	}
	return api.GetRelease200JSONResponse(d), nil
}

func (s *Server) GetReleaseReadiness(ctx context.Context, req api.GetReleaseReadinessRequestObject) (api.GetReleaseReadinessResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Tracker.ReleaseReadiness(ctx, a, req.ReleaseId)
	if err != nil {
		return nil, err
	}
	return api.GetReleaseReadiness200JSONResponse(r), nil
}

func (s *Server) GetReleaseBurnup(ctx context.Context, req api.GetReleaseBurnupRequestObject) (api.GetReleaseBurnupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b, err := s.Tracker.ReleaseBurnup(ctx, a, req.ReleaseId)
	if err != nil {
		return nil, err
	}
	return api.GetReleaseBurnup200JSONResponse(b), nil
}

func (s *Server) GenerateReleaseNotes(ctx context.Context, req api.GenerateReleaseNotesRequestObject) (api.GenerateReleaseNotesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.Tracker.GenerateReleaseNotes(ctx, a, req.ReleaseId)
	if err != nil {
		return nil, err
	}
	return api.GenerateReleaseNotes200JSONResponse(n), nil
}

func (s *Server) GetReleaseSettings(ctx context.Context, req api.GetReleaseSettingsRequestObject) (api.GetReleaseSettingsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	st, err := s.Tracker.ReleaseSettings(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.GetReleaseSettings200JSONResponse(st), nil
}

func (s *Server) UpdateReleaseSettings(ctx context.Context, req api.UpdateReleaseSettingsRequestObject) (api.UpdateReleaseSettingsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	st, err := s.Tracker.UpdateReleaseSettings(ctx, a, req.ProjectKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.UpdateReleaseSettings200JSONResponse(st), nil
}

// ListPortalReleaseNotes is the help centre's What's new page.
func (s *Server) ListPortalReleaseNotes(ctx context.Context, req api.ListPortalReleaseNotesRequestObject) (api.ListPortalReleaseNotesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	notes, err := s.ServiceDesk.WhatsNew(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.ListPortalReleaseNotes200JSONResponse(notes), nil
}
