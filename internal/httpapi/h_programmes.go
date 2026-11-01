package httpapi

import (
	"context"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
)

func programmeTeamsView(t tracker.ProgrammeTeams) api.ProgrammeTeams {
	return api.ProgrammeTeams{Teams: projectRefs(t.Teams), HiddenCount: t.Hidden}
}

func projectRefs(ps []dbq.Project) []api.ProjectRef {
	out := make([]api.ProjectRef, 0, len(ps))
	for _, p := range ps {
		out = append(out, api.ProjectRef{Id: p.ID, Key: p.Key, Name: p.Name})
	}
	return out
}

func (s *Server) GetProgrammeOverview(ctx context.Context, req api.GetProgrammeOverviewRequestObject) (api.GetProgrammeOverviewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	doneDays := 90
	if req.Params.DoneDays != nil {
		doneDays = *req.Params.DoneDays
	}
	o, err := s.Tracker.ProgrammeOverview(ctx, a, req.ProjectKey, doneDays)
	if err != nil {
		return nil, err
	}
	return api.GetProgrammeOverview200JSONResponse(o), nil
}

func (s *Server) ListProgrammeTeams(ctx context.Context, req api.ListProgrammeTeamsRequestObject) (api.ListProgrammeTeamsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.Tracker.ProgrammeTeams(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.ListProgrammeTeams200JSONResponse(programmeTeamsView(t)), nil
}

func (s *Server) AddProgrammeTeam(ctx context.Context, req api.AddProgrammeTeamRequestObject) (api.AddProgrammeTeamResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apperr.Invalid("projectKey", "projectKey is required")
	}
	t, err := s.Tracker.AddProgrammeTeam(ctx, a, req.ProjectKey, req.Body.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.AddProgrammeTeam201JSONResponse(programmeTeamsView(t)), nil
}

func (s *Server) RemoveProgrammeTeam(ctx context.Context, req api.RemoveProgrammeTeamRequestObject) (api.RemoveProgrammeTeamResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.Tracker.RemoveProgrammeTeam(ctx, a, req.ProjectKey, req.TeamKey)
	if err != nil {
		return nil, err
	}
	return api.RemoveProgrammeTeam200JSONResponse{UnlinkedEpics: n}, nil
}

func (s *Server) ListProjectProgrammes(ctx context.Context, req api.ListProjectProgrammesRequestObject) (api.ListProjectProgrammesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := s.Tracker.ProgrammesOf(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.ListProjectProgrammes200JSONResponse(projectRefs(ps)), nil
}
