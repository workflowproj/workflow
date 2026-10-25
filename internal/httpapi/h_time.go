package httpapi

import (
	"context"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
)

// ---------------------------------------------------------------------------
// Time tracking: worklogs, the timesheet and the working day/week

func (s *Server) GetTimeTrackingSettings(ctx context.Context, _ api.GetTimeTrackingSettingsRequestObject) (api.GetTimeTrackingSettingsResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	u := s.Tracker.Units()
	return api.GetTimeTrackingSettings200JSONResponse{HoursPerDay: u.HoursPerDay, DaysPerWeek: u.DaysPerWeek}, nil
}

func (s *Server) worklogView(ctx context.Context, a *authz.Actor, access authz.Access, key string, w dbq.Worklog, users map[uuid.UUID]dbq.User) api.Worklog {
	v := api.Worklog{
		Id: w.ID, IssueKey: key, StartedAt: w.StartedAt, TimeSpentSeconds: w.DurationSeconds, Comment: w.Comment,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, CanEdit: tracker.CanEditWorklog(access, a, w),
	}
	if w.AuthorID != nil {
		u, ok := users[*w.AuthorID]
		if !ok {
			if fetched, err := s.Q.GetUser(ctx, *w.AuthorID); err == nil {
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

// worklogResult renders a worklog the caller just wrote, with their access to its project.
func (s *Server) worklogResult(ctx context.Context, a *authz.Actor, w dbq.Worklog, i dbq.Issue, p dbq.Project) (api.Worklog, error) {
	access, err := s.Tracker.Authz.Access(ctx, a, p)
	if err != nil {
		return api.Worklog{}, err
	}
	return s.worklogView(ctx, a, access, tracker.IssueKey(p, i), w, map[uuid.UUID]dbq.User{}), nil
}

func (s *Server) ListWorklogs(ctx context.Context, req api.ListWorklogsRequestObject) (api.ListWorklogsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ws, i, p, access, err := s.Tracker.ListWorklogs(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	key := tracker.IssueKey(p, i)
	users := map[uuid.UUID]dbq.User{}
	out := api.ListWorklogs200JSONResponse{}
	for _, w := range ws {
		out = append(out, s.worklogView(ctx, a, access, key, w, users))
	}
	return out, nil
}

func adjustOf(a *api.AdjustEstimate) string {
	if a == nil {
		return ""
	}
	return string(*a)
}

func (s *Server) AddWorklog(ctx context.Context, req api.AddWorklogRequestObject) (api.AddWorklogResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	w, i, p, err := s.Tracker.AddWorklog(ctx, a, req.IssueKey, tracker.WorklogInput{
		StartedAt: b.StartedAt, Seconds: b.TimeSpentSeconds, Comment: deref(b.Comment), Adjust: adjustOf(b.AdjustEstimate), Remaining: b.RemainingEstimateSeconds,
	})
	if err != nil {
		return nil, err
	}
	v, err := s.worklogResult(ctx, a, w, i, p)
	return api.AddWorklog201JSONResponse(v), err
}

func (s *Server) UpdateWorklog(ctx context.Context, req api.UpdateWorklogRequestObject) (api.UpdateWorklogResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	w, i, p, err := s.Tracker.UpdateWorklog(ctx, a, req.WorklogId, tracker.WorklogPatch{
		StartedAt: b.StartedAt, Seconds: b.TimeSpentSeconds, Comment: b.Comment, Adjust: adjustOf(b.AdjustEstimate), Remaining: b.RemainingEstimateSeconds,
	})
	if err != nil {
		return nil, err
	}
	v, err := s.worklogResult(ctx, a, w, i, p)
	return api.UpdateWorklog200JSONResponse(v), err
}

func (s *Server) DeleteWorklog(ctx context.Context, req api.DeleteWorklogRequestObject) (api.DeleteWorklogResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteWorklog(ctx, a, req.WorklogId, adjustOf(req.Params.AdjustEstimate), req.Params.RemainingEstimateSeconds); err != nil {
		return nil, err
	}
	return api.DeleteWorklog204Response{}, nil
}

func (s *Server) GetTimesheet(ctx context.Context, req api.GetTimesheetRequestObject) (api.GetTimesheetResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	q := req.Params
	ts, err := s.Tracker.Timesheet(ctx, a, tracker.TimesheetQuery{
		From: q.From.Time, To: q.To.Time, Projects: metricScope(q.Projects, nil).Projects, UserID: q.User, TimeZone: deref(q.Tz),
	})
	if err != nil {
		return nil, err
	}
	out := api.GetTimesheet200JSONResponse{
		From: openapiDate(ts.From), To: openapiDate(ts.To), TimeZone: ts.TimeZone, DayTotals: ts.DayTotals, Total: ts.Total,
		Dates: []openapi_types.Date{}, People: []api.TimesheetPerson{},
	}
	for _, d := range ts.Dates {
		out.Dates = append(out.Dates, openapiDate(d))
	}
	for _, person := range ts.People {
		pv := api.TimesheetPerson{Days: person.Days, Total: person.Total, Issues: []api.TimesheetIssue{}}
		if person.User != nil {
			uv := tracker.UserView(*person.User)
			pv.User = &uv
		}
		for _, ti := range person.Issues {
			pv.Issues = append(pv.Issues, api.TimesheetIssue{Issue: ts.IssueRef(ti.Issue), Days: ti.Days, Total: ti.Total})
		}
		out.People = append(out.People, pv)
	}
	return out, nil
}
