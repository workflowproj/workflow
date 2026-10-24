package httpapi

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

func boardView(b dbq.Board, projectKey string, cols []dbq.BoardColumn) api.Board {
	v := api.Board{
		Id: b.ID, ProjectKey: projectKey, Name: b.Name, Type: api.BoardType(b.Type), FilterJql: b.FilterJql,
		Swimlanes: api.Swimlanes(b.Swimlanes), DoneRetentionDays: int(b.DoneRetentionDays), Columns: []api.BoardColumn{},
	}
	for _, c := range cols {
		id := c.ID
		col := api.BoardColumn{Id: &id, Name: c.Name, StatusIds: c.StatusIds}
		if c.WipLimit != nil {
			w := int(*c.WipLimit)
			col.WipLimit = &w
		}
		v.Columns = append(v.Columns, col)
	}
	return v
}

func boardInput(b api.BoardInput) tracker.BoardInput {
	in := tracker.BoardInput{Name: b.Name, Type: string(b.Type), FilterJQL: deref(b.FilterJql)}
	if b.Swimlanes != nil {
		in.Swimlanes = string(*b.Swimlanes)
	}
	if b.DoneRetentionDays != nil {
		in.DoneRetentionDays = *b.DoneRetentionDays
	}
	if b.Columns != nil {
		in.Columns = []tracker.BoardColumnInput{}
		for _, c := range *b.Columns {
			in.Columns = append(in.Columns, tracker.BoardColumnInput{Name: c.Name, StatusIDs: c.StatusIds, WipLimit: c.WipLimit})
		}
	}
	return in
}

func (s *Server) ListBoards(ctx context.Context, req api.ListBoardsRequestObject) (api.ListBoardsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	bs, p, err := s.Tracker.ListBoards(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	out := api.ListBoards200JSONResponse{}
	for _, b := range bs {
		cols, err := s.Q.BoardColumns(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, boardView(b, p.Key, cols))
	}
	return out, nil
}

func (s *Server) CreateBoard(ctx context.Context, req api.CreateBoardRequestObject) (api.CreateBoardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b, p, err := s.Tracker.CreateBoard(ctx, a, req.ProjectKey, boardInput(*req.Body))
	if err != nil {
		return nil, err
	}
	cols, err := s.Q.BoardColumns(ctx, b.ID)
	return api.CreateBoard201JSONResponse(boardView(b, p.Key, cols)), err
}

func (s *Server) GetBoard(ctx context.Context, req api.GetBoardRequestObject) (api.GetBoardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b, p, cols, err := s.Tracker.GetBoard(ctx, a, req.BoardId)
	if err != nil {
		return nil, err
	}
	return api.GetBoard200JSONResponse(boardView(b, p.Key, cols)), nil
}

func (s *Server) UpdateBoard(ctx context.Context, req api.UpdateBoardRequestObject) (api.UpdateBoardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b, err := s.Tracker.UpdateBoard(ctx, a, req.BoardId, boardInput(*req.Body))
	if err != nil {
		return nil, err
	}
	_, p, cols, err := s.Tracker.GetBoard(ctx, a, b.ID)
	if err != nil {
		return nil, err
	}
	return api.UpdateBoard200JSONResponse(boardView(b, p.Key, cols)), nil
}

func (s *Server) DeleteBoard(ctx context.Context, req api.DeleteBoardRequestObject) (api.DeleteBoardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteBoard(ctx, a, req.BoardId); err != nil {
		return nil, err
	}
	return api.DeleteBoard204Response{}, nil
}

func (s *Server) GetBoardData(ctx context.Context, req api.GetBoardDataRequestObject) (api.GetBoardDataResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.Tracker.GetBoardData(ctx, a, req.BoardId, deref(req.Params.Jql))
	if err != nil {
		return nil, err
	}
	out := api.GetBoardData200JSONResponse{Board: boardView(d.Board, d.Project.Key, d.AllCols), Columns: []api.BoardColumnData{}, UnmappedStatusIds: d.Unmapped}
	if out.UnmappedStatusIds == nil {
		out.UnmappedStatusIds = []openapiUUID{}
	}
	if d.Sprint != nil {
		sv := tracker.SprintView(*d.Sprint)
		out.Sprint = &sv
	}
	var all []dbq.Issue
	for _, c := range d.Columns {
		all = append(all, c.Issues...)
	}
	views, err := s.Tracker.IssueViews(ctx, all)
	if err != nil {
		return nil, err
	}
	n := 0
	for _, c := range d.Columns {
		col := api.BoardColumnData{Id: c.Column.ID, Name: c.Column.Name, StatusIds: c.Column.StatusIds, OverLimit: c.OverLimit, Issues: views[n : n+len(c.Issues)]}
		n += len(c.Issues)
		if c.Column.WipLimit != nil {
			w := int(*c.Column.WipLimit)
			col.WipLimit = &w
		}
		out.Columns = append(out.Columns, col)
	}
	return out, nil
}

func (s *Server) GetBacklog(ctx context.Context, req api.GetBacklogRequestObject) (api.GetBacklogResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	bl, err := s.Tracker.GetBacklog(ctx, a, req.BoardId, deref(req.Params.Jql))
	if err != nil {
		return nil, err
	}
	out := api.GetBacklog200JSONResponse{Sprints: []api.BacklogSprint{}}
	for _, sp := range bl.Sprints {
		views, err := s.Tracker.IssueViews(ctx, sp.Issues)
		if err != nil {
			return nil, err
		}
		out.Sprints = append(out.Sprints, api.BacklogSprint{Sprint: tracker.SprintView(sp.Sprint), Issues: views})
	}
	if out.Backlog, err = s.Tracker.IssueViews(ctx, bl.Backlog); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Sprints

func sprintInput(b api.SprintInput) tracker.SprintInput {
	return tracker.SprintInput{Name: b.Name, Goal: b.Goal, StartDate: dateOpt(b.StartDate, b.StartDate.MustGet), EndDate: dateOpt(b.EndDate, b.EndDate.MustGet)}
}

func (s *Server) ListSprints(ctx context.Context, req api.ListSprintsRequestObject) (api.ListSprintsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	state := ""
	if req.Params.State != nil {
		state = string(*req.Params.State)
	}
	sps, err := s.Tracker.ListSprints(ctx, a, req.ProjectKey, state)
	if err != nil {
		return nil, err
	}
	out := api.ListSprints200JSONResponse{}
	for _, sp := range sps {
		out = append(out, tracker.SprintView(sp))
	}
	return out, nil
}

func (s *Server) CreateSprint(ctx context.Context, req api.CreateSprintRequestObject) (api.CreateSprintResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sp, err := s.Tracker.CreateSprint(ctx, a, req.ProjectKey, sprintInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateSprint201JSONResponse(tracker.SprintView(sp)), nil
}

func (s *Server) UpdateSprint(ctx context.Context, req api.UpdateSprintRequestObject) (api.UpdateSprintResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sp, err := s.Tracker.UpdateSprint(ctx, a, req.SprintId, sprintInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateSprint200JSONResponse(tracker.SprintView(sp)), nil
}

func (s *Server) DeleteSprint(ctx context.Context, req api.DeleteSprintRequestObject) (api.DeleteSprintResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteSprint(ctx, a, req.SprintId); err != nil {
		return nil, err
	}
	return api.DeleteSprint204Response{}, nil
}

func (s *Server) StartSprint(ctx context.Context, req api.StartSprintRequestObject) (api.StartSprintResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sp, err := s.Tracker.StartSprint(ctx, a, req.SprintId, req.Body.StartDate.Time, req.Body.EndDate.Time, req.Body.Goal)
	if err != nil {
		return nil, err
	}
	return api.StartSprint200JSONResponse(tracker.SprintView(sp)), nil
}

func (s *Server) CompleteSprint(ctx context.Context, req api.CompleteSprintRequestObject) (api.CompleteSprintResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sp, err := s.Tracker.CompleteSprint(ctx, a, req.SprintId, req.Body.MoveOpenIssuesTo)
	if err != nil {
		return nil, err
	}
	return api.CompleteSprint200JSONResponse(tracker.SprintView(sp)), nil
}

func (s *Server) GetSprintReport(ctx context.Context, req api.GetSprintReportRequestObject) (api.GetSprintReportResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rep, err := s.Tracker.SprintReport(ctx, a, req.SprintId)
	if err != nil {
		return nil, err
	}
	out := api.GetSprintReport200JSONResponse{
		Sprint: tracker.SprintView(rep.Sprint), CommittedPoints: rep.CommittedPoints, CompletedPoints: rep.CompletedPoints,
		CommittedIssues: rep.CommittedIssues, CompletedIssues: rep.CompletedIssues, Burndown: []api.BurndownPoint{},
	}
	for _, b := range rep.Burndown {
		out.Burndown = append(out.Burndown, api.BurndownPoint{Date: openapiDate(b.Date), RemainingPoints: b.Remaining, IdealPoints: b.Ideal})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Releases

func releaseInput(b api.ReleaseInput) tracker.ReleaseInput {
	in := tracker.ReleaseInput{Name: b.Name, Description: b.Description, StartDate: dateOpt(b.StartDate, b.StartDate.MustGet), ReleaseDate: dateOpt(b.ReleaseDate, b.ReleaseDate.MustGet)}
	if b.State != nil {
		st := string(*b.State)
		in.State = &st
	}
	return in
}

func (s *Server) ListReleases(ctx context.Context, req api.ListReleasesRequestObject) (api.ListReleasesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Tracker.ListReleases(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	out := api.ListReleases200JSONResponse{}
	for _, r := range rows {
		rel := dbq.Release{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Description: r.Description, StartDate: r.StartDate, ReleaseDate: r.ReleaseDate,
			State: r.State, ReleasedAt: r.ReleasedAt, Position: r.Position, CreatedAt: r.CreatedAt}
		out = append(out, tracker.ReleaseView(rel, int(r.IssueCount), int(r.DoneCount)))
	}
	return out, nil
}

func (s *Server) CreateRelease(ctx context.Context, req api.CreateReleaseRequestObject) (api.CreateReleaseResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Tracker.CreateRelease(ctx, a, req.ProjectKey, releaseInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateRelease201JSONResponse(tracker.ReleaseView(r, 0, 0)), nil
}

func (s *Server) UpdateRelease(ctx context.Context, req api.UpdateReleaseRequestObject) (api.UpdateReleaseResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Tracker.UpdateRelease(ctx, a, req.ReleaseId, releaseInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateRelease200JSONResponse(tracker.ReleaseView(r, 0, 0)), nil
}

func (s *Server) DeleteRelease(ctx context.Context, req api.DeleteReleaseRequestObject) (api.DeleteReleaseResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteRelease(ctx, a, req.ReleaseId); err != nil {
		return nil, err
	}
	return api.DeleteRelease204Response{}, nil
}

func (s *Server) ShipRelease(ctx context.Context, req api.ShipReleaseRequestObject) (api.ShipReleaseResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Tracker.ShipRelease(ctx, a, req.ReleaseId, req.Body.MoveOpenIssuesTo)
	if err != nil {
		return nil, err
	}
	return api.ShipRelease200JSONResponse(tracker.ReleaseView(r, 0, 0)), nil
}

func (s *Server) GetTimeline(ctx context.Context, req api.GetTimelineRequestObject) (api.GetTimelineResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	if req.Params.Projects != nil && *req.Params.Projects != "" {
		keys = strings.Split(*req.Params.Projects, ",")
	}
	doneDays := 90
	if req.Params.DoneDays != nil {
		doneDays = *req.Params.DoneDays
	}
	tl, err := s.Tracker.Timeline(ctx, a, keys, deref(req.Params.Jql), doneDays)
	if err != nil {
		return nil, err
	}
	views, err := s.Tracker.IssueViews(ctx, tl.Issues)
	if err != nil {
		return nil, err
	}
	keyByID := make(map[uuid.UUID]string, len(views))
	for _, v := range views {
		keyByID[v.Id] = v.Key
	}
	out := api.GetTimeline200JSONResponse{
		Projects: []api.ProjectRef{}, Issues: views, Links: []api.TimelineLink{}, Sprints: []api.TimelineSprint{},
		Releases: []api.TimelineRelease{}, Truncated: tl.Truncated,
	}
	for _, p := range tl.Projects {
		out.Projects = append(out.Projects, api.ProjectRef{Id: p.ID, Key: p.Key, Name: p.Name})
	}
	for _, l := range tl.Links {
		out.Links = append(out.Links, api.TimelineLink{Id: l.ID, SourceKey: keyByID[l.SourceID], TargetKey: keyByID[l.TargetID], Type: api.TimelineLinkTypeBlocks})
	}
	for _, sp := range tl.Sprints {
		out.Sprints = append(out.Sprints, api.TimelineSprint{ProjectKey: sp.ProjectKey, Sprint: tracker.SprintView(sp.Sprint)})
	}
	for _, pr := range tl.Releases {
		r := pr.Release
		rel := dbq.Release{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Description: r.Description, StartDate: r.StartDate, ReleaseDate: r.ReleaseDate,
			State: r.State, ReleasedAt: r.ReleasedAt, Position: r.Position, CreatedAt: r.CreatedAt}
		out.Releases = append(out.Releases, api.TimelineRelease{ProjectKey: pr.ProjectKey, Release: tracker.ReleaseView(rel, int(r.IssueCount), int(r.DoneCount))})
	}
	return out, nil
}
