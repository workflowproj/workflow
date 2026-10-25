package httpapi

import (
	"context"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

// metricScope reads the shared projects/jql query parameters.
func metricScope(projects *string, jql *string) tracker.MetricScope {
	sc := tracker.MetricScope{JQL: deref(jql)}
	if projects != nil && *projects != "" {
		sc.Projects = strings.Split(*projects, ",")
	}
	return sc
}

func metricDays(days *int) int {
	if days == nil {
		return 30
	}
	return *days
}

func cycleTimeView(rep tracker.CycleTimeReport) api.CycleTimeReport {
	out := api.CycleTimeReport{Days: rep.Days, AverageHours: rep.Average, MedianHours: rep.Median, P85Hours: rep.P85, Issues: []api.CycleTimeIssue{}}
	if rep.Truncated {
		out.Truncated = &rep.Truncated
	}
	for _, i := range rep.Issues {
		pk := i.ProjectKey
		out.Issues = append(out.Issues, api.CycleTimeIssue{Key: i.Key, ProjectKey: &pk, Summary: i.Summary, CycleHours: i.Hours, ResolvedAt: i.ResolvedAt})
	}
	return out
}

func (s *Server) GetMetricsSummary(ctx context.Context, req api.GetMetricsSummaryRequestObject) (api.GetMetricsSummaryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.Tracker.MetricsSummary(ctx, a, metricScope(req.Params.Projects, req.Params.Jql), metricDays(req.Params.Days))
	if err != nil {
		return nil, err
	}
	return api.GetMetricsSummary200JSONResponse{
		Days: m.Days, Open: m.Open, Todo: m.Todo, InProgress: m.InProgress, Created: m.Created, Resolved: m.Resolved,
		Overdue: m.Overdue, Unassigned: m.Unassigned, OpenPoints: m.OpenPoints, ResolvedPoints: m.ResolvedPoints, MedianCycleHours: m.MedianCycleHours,
	}, nil
}

func (s *Server) GetCumulativeFlow(ctx context.Context, req api.GetCumulativeFlowRequestObject) (api.GetCumulativeFlowResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	days := metricDays(req.Params.Days)
	dates, series, truncated, err := s.Tracker.CumulativeFlow(ctx, a, metricScope(req.Params.Projects, req.Params.Jql), days, string(deref(req.Params.By)))
	if err != nil {
		return nil, err
	}
	out := api.GetCumulativeFlow200JSONResponse{Days: days, Dates: []openapi_types.Date{}, Series: []api.FlowSeries{}}
	if truncated {
		out.Truncated = &truncated
	}
	for _, d := range dates {
		out.Dates = append(out.Dates, openapiDate(d))
	}
	for _, fs := range series {
		out.Series = append(out.Series, api.FlowSeries{Key: fs.Key, Label: fs.Label, Category: api.StatusCategory(fs.Category), Counts: fs.Counts})
	}
	return out, nil
}

func (s *Server) GetCreatedResolved(ctx context.Context, req api.GetCreatedResolvedRequestObject) (api.GetCreatedResolvedResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	days := metricDays(req.Params.Days)
	interval := string(deref(req.Params.Interval))
	if interval == "" {
		interval = "day"
	}
	buckets, err := s.Tracker.CreatedResolved(ctx, a, metricScope(req.Params.Projects, req.Params.Jql), days, interval)
	if err != nil {
		return nil, err
	}
	out := api.GetCreatedResolved200JSONResponse{Days: days, Interval: api.CreatedResolvedInterval(interval), Buckets: []api.CreatedResolvedBucket{}}
	for _, b := range buckets {
		out.Buckets = append(out.Buckets, api.CreatedResolvedBucket{Start: openapiDate(b.Start), Created: b.Created, Resolved: b.Resolved, Open: b.Open})
	}
	return out, nil
}

func (s *Server) GetBreakdown(ctx context.Context, req api.GetBreakdownRequestObject) (api.GetBreakdownResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	groups, total, points, err := s.Tracker.Breakdown(ctx, a, metricScope(req.Params.Projects, req.Params.Jql), string(req.Params.By))
	if err != nil {
		return nil, err
	}
	out := api.GetBreakdown200JSONResponse{By: req.Params.By, Total: total, TotalPoints: points, Groups: []api.BreakdownGroup{}}
	for _, g := range groups {
		v := api.BreakdownGroup{Key: g.Key, Label: g.Label, Count: g.Count, Points: g.Points}
		if g.Category != "" {
			c := api.StatusCategory(g.Category)
			v.Category = &c
		}
		if g.Color != "" {
			c := g.Color
			v.Color = &c
		}
		if g.User != nil {
			u := tracker.UserView(*g.User)
			v.User = &u
		}
		out.Groups = append(out.Groups, v)
	}
	return out, nil
}

func (s *Server) GetCycleTimeMetrics(ctx context.Context, req api.GetCycleTimeMetricsRequestObject) (api.GetCycleTimeMetricsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rep, err := s.Tracker.CycleTimeFor(ctx, a, metricScope(req.Params.Projects, req.Params.Jql), metricDays(req.Params.Days))
	if err != nil {
		return nil, err
	}
	return api.GetCycleTimeMetrics200JSONResponse(cycleTimeView(rep)), nil
}

func (s *Server) GetVelocity(ctx context.Context, req api.GetVelocityRequestObject) (api.GetVelocityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	count := 7
	if req.Params.Count != nil {
		count = *req.Params.Count
	}
	sprints, err := s.Tracker.Velocity(ctx, a, req.ProjectKey, count)
	if err != nil {
		return nil, err
	}
	out := api.GetVelocity200JSONResponse{Sprints: []api.VelocitySprint{}}
	for _, v := range sprints {
		out.Sprints = append(out.Sprints, api.VelocitySprint{Sprint: tracker.SprintView(v.Sprint), CommittedPoints: v.CommittedPoints, CompletedPoints: v.CompletedPoints,
			CommittedIssues: v.CommittedIssues, CompletedIssues: v.CompletedIssues})
		out.AveragePoints += v.CompletedPoints
		out.AverageIssues += float64(v.CompletedIssues)
	}
	if n := float64(len(sprints)); n > 0 {
		out.AveragePoints = round1(out.AveragePoints / n)
		out.AverageIssues = round1(out.AverageIssues / n)
	}
	return out, nil
}

func (s *Server) GetIssueMetrics(ctx context.Context, req api.GetIssueMetricsRequestObject) (api.GetIssueMetricsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.Tracker.IssueMetrics(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	out := api.GetIssueMetrics200JSONResponse{
		CreatedAt: m.CreatedAt, StartedAt: m.StartedAt, ResolvedAt: m.ResolvedAt, LeadTimeHours: m.LeadTimeHours, CycleTimeHours: m.CycleTimeHours,
		AgeHours: m.AgeHours, StatusSince: m.StatusSince, StatusAgeHours: m.StatusAgeHours, Reopened: m.Reopened, Sprints: m.Sprints, TimeInStatus: []api.StatusTime{},
	}
	for _, st := range m.TimeInStatus {
		out.TimeInStatus = append(out.TimeInStatus, api.StatusTime{Status: tracker.StatusView(st.Status), Hours: st.Hours, Visits: st.Visits})
	}
	return out, nil
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// ---------------------------------------------------------------------------
// Dashboards

func dashboardView(v tracker.DashboardView) api.Dashboard {
	d := v.Dashboard
	return api.Dashboard{
		Id: d.ID, Name: d.Name, Description: d.Description, Owner: tracker.UserView(v.Owner), Shared: d.Shared, CanEdit: v.CanEdit,
		Gadgets: v.Gadgets, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func dashboardInput(b *api.DashboardInput) tracker.DashboardInput {
	in := tracker.DashboardInput{Name: b.Name, Description: deref(b.Description), Shared: deref(b.Shared), Gadgets: b.Gadgets}
	if in.Gadgets == nil {
		in.Gadgets = []api.Gadget{}
	}
	return in
}

func (s *Server) ListDashboards(ctx context.Context, _ api.ListDashboardsRequestObject) (api.ListDashboardsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.Tracker.ListDashboards(ctx, a)
	if err != nil {
		return nil, err
	}
	out := api.ListDashboards200JSONResponse{}
	for _, v := range list {
		out = append(out, dashboardView(v))
	}
	return out, nil
}

func (s *Server) CreateDashboard(ctx context.Context, req api.CreateDashboardRequestObject) (api.CreateDashboardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.CreateDashboard(ctx, a, dashboardInput(req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateDashboard201JSONResponse(dashboardView(v)), nil
}

func (s *Server) GetDashboard(ctx context.Context, req api.GetDashboardRequestObject) (api.GetDashboardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.GetDashboard(ctx, a, req.DashboardId)
	if err != nil {
		return nil, err
	}
	return api.GetDashboard200JSONResponse(dashboardView(v)), nil
}

func (s *Server) UpdateDashboard(ctx context.Context, req api.UpdateDashboardRequestObject) (api.UpdateDashboardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.UpdateDashboard(ctx, a, req.DashboardId, dashboardInput(req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateDashboard200JSONResponse(dashboardView(v)), nil
}

func (s *Server) DeleteDashboard(ctx context.Context, req api.DeleteDashboardRequestObject) (api.DeleteDashboardResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteDashboard(ctx, a, req.DashboardId); err != nil {
		return nil, err
	}
	return api.DeleteDashboard204Response{}, nil
}
