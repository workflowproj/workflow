package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/geoffo-dev/workflow-management/internal/api"
)

const jqlHelp = `JQL examples:
  project = PLAT AND statusCategory != Done AND assignee = currentUser() ORDER BY priority DESC
  type = Bug AND priority >= High AND created >= -7d
  sprint in openSprints() AND assignee is EMPTY
  text ~ "checkout timeout" AND labels = backend
  parent = PLAT-1   (children of an epic)    fixVersion = "2026.11"
Fields: project, key, type, status, statusCategory ("To Do", "In Progress", Done), assignee, reporter
(email or currentUser()), priority (Highest..Lowest, comparable), labels, sprint (name, openSprints(),
closedSprints(), futureSprints()), fixVersion, parent, summary/description/comment (~ contains), text ~
(full text), created/updated/resolved/due/start (dates: "2026-11-01", -7d, startOfWeek()), storyPoints.
Operators: = != in "not in" ~ !~ < <= > >= "is EMPTY" "is not EMPTY"; combine with AND, OR, NOT, ().`

func (t *tools) register(s *mcp.Server) {
	read := func(title string) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true}
	}
	write := func(title string, idempotent bool) *mcp.ToolAnnotations {
		no := false
		return &mcp.ToolAnnotations{Title: title, DestructiveHint: &no, IdempotentHint: idempotent}
	}
	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Who you are acting as: the access token's owner, organisation role and token scopes.", Annotations: read("Who am I")}, t.whoami)
	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "Projects you can browse, with their keys.", Annotations: read("List projects")}, t.listProjects)
	mcp.AddTool(s, &mcp.Tool{Name: "get_project", Description: "A project's issue types (keys to use when creating issues), boards, active and upcoming sprints, unreleased releases and labels.", Annotations: read("Get project")}, t.getProject)
	mcp.AddTool(s, &mcp.Tool{Name: "search_issues", Description: "Find issues with JQL. Returns one line per issue (key, type, status, assignee, priority, points, summary) and the total.\n\n" + jqlHelp, Annotations: read("Search issues")}, t.searchIssues)
	mcp.AddTool(s, &mcp.Tool{Name: "get_issue", Description: "Everything about one issue: fields, description, parent, sub-tasks, links, pull requests, recent comments, timing metrics (lead/cycle time, time in each status) and the workflow transitions available now.", Annotations: read("Get issue")}, t.getIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "get_board", Description: "A project's board: each column with its issues (the active sprint on a Scrum board).", Annotations: read("Get board")}, t.getBoard)
	mcp.AddTool(s, &mcp.Tool{Name: "get_sprint_report", Description: "Progress of a sprint (the active one by default): commitment, completed work, scope added or removed, and the day-by-day burndown.", Annotations: read("Sprint report")}, t.getSprintReport)
	mcp.AddTool(s, &mcp.Tool{Name: "get_metrics", Description: "Delivery metrics over projects and/or JQL: summary (open, created, resolved, overdue, cycle time), breakdown (counts and points by a field), cycle_time, created_resolved, cumulative_flow, or velocity (one project's recent sprints).", Annotations: read("Get metrics")}, t.getMetrics)
	mcp.AddTool(s, &mcp.Tool{Name: "find_users", Description: "Find people by name or email (to assign issues or write JQL).", Annotations: read("Find users")}, t.findUsers)

	if t.readOnly {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "create_issue", Description: "Create an issue. Use get_project for the project's issue type keys. Description is Markdown.", Annotations: write("Create issue", false)}, t.createIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "update_issue", Description: "Change an issue's fields; only the fields you pass change. Use `clear` to empty fields. Status changes use transition_issue instead.", Annotations: write("Update issue", true)}, t.updateIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "transition_issue", Description: "Move an issue through its workflow, by transition id, name or target status (get_issue lists the available ones). Some transitions ask for screen fields such as a resolution.", Annotations: write("Transition issue", false)}, t.transitionIssue)
	mcp.AddTool(s, &mcp.Tool{Name: "add_comment", Description: "Comment on an issue (Markdown).", Annotations: write("Add comment", false)}, t.addComment)
	mcp.AddTool(s, &mcp.Tool{Name: "link_issues", Description: "Link two issues: key <type> targetKey, e.g. PLAT-3 blocks PLAT-7.", Annotations: write("Link issues", true)}, t.linkIssues)
}

// writeTools are the tools registered after the read tools above: they change issues, need the
// write scope, and are left out for connected apps allowed only to read.
var writeTools = []string{"create_issue", "update_issue", "transition_issue", "add_comment", "link_issues"}

type none struct{}

// ---------------------------------------------------------------------------
// Read tools

func (t *tools) whoami(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	me, err := as[api.GetMe200JSONResponse](t.api.GetMe(ctx, api.GetMeRequestObject{}))
	if err != nil {
		return nil, nil, err
	}
	scopes := "session"
	if t.actor.TokenID != nil {
		scopes = strings.Join(t.actor.Scopes, ", ")
	}
	out := fmt.Sprintf("%s <%s>\nOrganisation role: %s\nToken scopes: %s", me.DisplayName, me.Email, me.OrgRole, scopes)
	if t.connected {
		out += "\nConnected app: " + t.actor.Via
	}
	return text(out), nil, nil
}

func (t *tools) listProjects(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	ps, err := as[api.ListProjects200JSONResponse](t.api.ListProjects(ctx, api.ListProjectsRequestObject{}))
	if err != nil {
		return nil, nil, err
	}
	if len(ps) == 0 {
		return text("You cannot browse any projects."), nil, nil
	}
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%s — %s (%s)", p.Key, p.Name, p.Access)
		if d := oneLine(p.Description, 120); d != "" {
			b.WriteString(": " + d)
		}
		b.WriteString("\n")
	}
	return text(b.String()), nil, nil
}

type projectIn struct {
	Key string `json:"key" jsonschema:"project key, e.g. PLAT"`
}

func (t *tools) getProject(ctx context.Context, _ *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	p, err := as[api.GetProject200JSONResponse](t.api.GetProject(ctx, api.GetProjectRequestObject{ProjectKey: in.Key}))
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s (%s project, %s template)\n", p.Key, p.Name, p.Access, p.Template)
	if p.Description != "" {
		fmt.Fprintf(&b, "%s\n", p.Description)
	}
	if p.Lead != nil {
		fmt.Fprintf(&b, "Lead: %s\n", userText(p.Lead))
	}
	fmt.Fprintf(&b, "URL: %s/projects/%s/board\n", t.publicURL, p.Key)
	if types, err := as[api.ListIssueTypes200JSONResponse](t.api.ListIssueTypes(ctx, api.ListIssueTypesRequestObject{ProjectKey: in.Key})); err == nil {
		b.WriteString("\nIssue types (key — name):\n")
		for _, it := range types {
			level := map[int]string{1: "epic level", 0: "standard", -1: "sub-task"}[it.Level]
			fmt.Fprintf(&b, "- %s — %s (%s, workflow %q)\n", it.Key, it.Name, level, it.WorkflowName)
		}
	}
	if boards, err := as[api.ListBoards200JSONResponse](t.api.ListBoards(ctx, api.ListBoardsRequestObject{ProjectKey: in.Key})); err == nil && len(boards) > 0 {
		b.WriteString("\nBoards:\n")
		for _, bd := range boards {
			var cols []string
			for _, c := range bd.Columns {
				cols = append(cols, c.Name)
			}
			fmt.Fprintf(&b, "- %s (%s): %s\n", bd.Name, bd.Type, strings.Join(cols, " → "))
		}
	}
	if sprints, err := as[api.ListSprints200JSONResponse](t.api.ListSprints(ctx, api.ListSprintsRequestObject{ProjectKey: in.Key})); err == nil {
		var open []string
		for _, sp := range sprints {
			if sp.State != api.SprintStateClosed {
				open = append(open, sprintText(sp))
			}
		}
		if len(open) > 0 {
			b.WriteString("\nSprints:\n- " + strings.Join(open, "\n- ") + "\n")
		}
	}
	if releases, err := as[api.ListReleases200JSONResponse](t.api.ListReleases(ctx, api.ListReleasesRequestObject{ProjectKey: in.Key})); err == nil {
		var open []string
		for _, r := range releases {
			if r.State != api.ReleaseStateReleased && r.State != api.ReleaseStateArchived {
				s := fmt.Sprintf("%s (%d/%d done", r.Name, r.DoneCount, r.IssueCount)
				if r.ReleaseDate != nil {
					s += ", due " + r.ReleaseDate.String()
				}
				open = append(open, s+")")
			}
		}
		if len(open) > 0 {
			b.WriteString("\nUnreleased releases: " + strings.Join(open, "; ") + "\n")
		}
	}
	if labels, err := as[api.ListProjectLabels200JSONResponse](t.api.ListProjectLabels(ctx, api.ListProjectLabelsRequestObject{ProjectKey: in.Key})); err == nil && len(labels) > 0 {
		b.WriteString("\nLabels in use: " + strings.Join(labels, ", ") + "\n")
	}
	return text(b.String()), nil, nil
}

type searchIn struct {
	JQL    string `json:"jql" jsonschema:"JQL query; empty matches everything you can see (newest first)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum issues to return (default 25, at most 100)"`
	Offset int    `json:"offset,omitempty" jsonschema:"skip this many results (for paging)"`
}

func (t *tools) searchIssues(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	limit = min(limit, 100)
	offset := max(in.Offset, 0)
	res, err := as[api.SearchIssues200JSONResponse](t.api.SearchIssues(ctx, api.SearchIssuesRequestObject{Params: api.SearchIssuesParams{Jql: &in.JQL, Limit: &limit, Offset: &offset}}))
	if err != nil {
		return nil, nil, err
	}
	if res.Total == 0 {
		return text("No issues match."), nil, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d issue%s match; showing %d–%d.\n", res.Total, plural(res.Total), offset+1, offset+len(res.Issues))
	for _, i := range res.Issues {
		b.WriteString(issueLine(i) + "\n")
	}
	if offset+len(res.Issues) < res.Total {
		fmt.Fprintf(&b, "More: offset=%d\n", offset+len(res.Issues))
	}
	return text(b.String()), nil, nil
}

type issueIn struct {
	Key      string `json:"key" jsonschema:"issue key, e.g. PLAT-12"`
	Comments *int   `json:"comments,omitempty" jsonschema:"how many of the latest comments to include (default 10, 0 for none)"`
}

func (t *tools) getIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	i, err := as[api.GetIssue200JSONResponse](t.api.GetIssue(ctx, api.GetIssueRequestObject{IssueKey: in.Key}))
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", i.Key, i.Summary)
	fmt.Fprintf(&b, "Type: %s · Status: %s (%s) · Priority: %s\n", i.Type.Name, i.Status.Name, categoryText(i.Status.Category), i.Priority)
	fmt.Fprintf(&b, "Assignee: %s · Reporter: %s\n", userText(i.Assignee), userText(i.Reporter))
	var extra []string
	if i.Sprint != nil {
		extra = append(extra, "Sprint: "+sprintText(*i.Sprint))
	}
	if i.StoryPoints != nil {
		extra = append(extra, "Points: "+num(*i.StoryPoints))
	}
	if i.StartDate != nil {
		extra = append(extra, "Start: "+i.StartDate.String())
	}
	if i.DueDate != nil {
		extra = append(extra, "Due: "+i.DueDate.String())
	}
	if len(i.Labels) > 0 {
		extra = append(extra, "Labels: "+strings.Join(i.Labels, ", "))
	}
	if len(i.Releases) > 0 {
		var names []string
		for _, r := range i.Releases {
			names = append(names, r.Name)
		}
		extra = append(extra, "Fix versions: "+strings.Join(names, ", "))
	}
	if i.Resolution != nil {
		extra = append(extra, "Resolution: "+*i.Resolution)
	}
	if len(extra) > 0 {
		b.WriteString(strings.Join(extra, " · ") + "\n")
	}
	if len(i.Fields) > 0 {
		keys := make([]string, 0, len(i.Fields))
		for k := range i.Fields {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		var parts []string
		for _, k := range keys {
			v, _ := json.Marshal(i.Fields[k])
			parts = append(parts, k+"="+string(v))
		}
		b.WriteString("Custom fields: " + strings.Join(parts, ", ") + "\n")
	}
	if i.Parent != nil {
		fmt.Fprintf(&b, "Parent: %s %s (%s)\n", i.Parent.Key, i.Parent.Summary, i.Parent.Type.Name)
	}
	fmt.Fprintf(&b, "Project: %s · Created %s · Updated %s · Version %d\n", i.Project.Key, day(i.CreatedAt), day(i.UpdatedAt), i.Version)
	fmt.Fprintf(&b, "URL: %s/browse/%s\n", t.publicURL, i.Key)

	if m, err := as[api.GetIssueMetrics200JSONResponse](t.api.GetIssueMetrics(ctx, api.GetIssueMetricsRequestObject{IssueKey: in.Key})); err == nil {
		var parts []string
		if m.LeadTimeHours != nil {
			parts = append(parts, "lead time "+duration(*m.LeadTimeHours))
		}
		if m.CycleTimeHours != nil {
			label := "cycle time "
			if m.ResolvedAt == nil {
				label = "in progress for "
			}
			parts = append(parts, label+duration(*m.CycleTimeHours))
		}
		parts = append(parts, fmt.Sprintf("in %s for %s", i.Status.Name, duration(m.StatusAgeHours)))
		var tis []string
		for _, st := range m.TimeInStatus {
			tis = append(tis, fmt.Sprintf("%s %s", st.Status.Name, duration(st.Hours)))
		}
		fmt.Fprintf(&b, "Timing: %s; time in status: %s", strings.Join(parts, ", "), strings.Join(tis, ", "))
		if m.Reopened > 0 {
			fmt.Fprintf(&b, "; reopened %d×", m.Reopened)
		}
		if m.Sprints > 1 {
			fmt.Fprintf(&b, "; in %d sprints", m.Sprints)
		}
		b.WriteString("\n")
	}
	if ts, err := as[api.ListTransitions200JSONResponse](t.api.ListTransitions(ctx, api.ListTransitionsRequestObject{IssueKey: in.Key})); err == nil {
		if len(ts) == 0 {
			b.WriteString("Transitions available: none\n")
		} else {
			b.WriteString("Transitions available:\n")
			for _, tr := range ts {
				fmt.Fprintf(&b, "- %s (%q) → %s", tr.Id, tr.Name, tr.To.Name)
				if len(tr.Screen) > 0 {
					var fs []string
					for _, f := range tr.Screen {
						s := f.Field
						if f.Required {
							s += " (required)"
						}
						if f.Options != nil && len(*f.Options) > 0 {
							s += " one of " + strings.Join(*f.Options, "/")
						}
						fs = append(fs, s)
					}
					b.WriteString("; screen fields: " + strings.Join(fs, ", "))
				}
				b.WriteString("\n")
			}
		}
	}
	if len(i.Children) > 0 {
		b.WriteString("\nChildren:\n")
		for _, c := range i.Children {
			b.WriteString(issueLine(c) + "\n")
		}
	}
	if len(i.Links) > 0 {
		b.WriteString("\nLinks:\n")
		for _, l := range i.Links {
			fmt.Fprintf(&b, "- %s %s %s (%s)\n", l.Label, l.Issue.Key, l.Issue.Summary, l.Issue.Status.Name)
		}
	}
	if len(i.Development) > 0 {
		b.WriteString("\nDevelopment:\n")
		for _, d := range i.Development {
			fmt.Fprintf(&b, "- %s %s: %s (%s) %s\n", d.Kind, d.Repository.FullPath, d.Title, d.State, d.Url)
		}
	}
	if desc := strings.TrimSpace(i.Description); desc != "" {
		b.WriteString("\nDescription:\n" + desc + "\n")
	}
	n := 10
	if in.Comments != nil {
		n = max(*in.Comments, 0)
	}
	if n > 0 {
		if cs, err := as[api.ListComments200JSONResponse](t.api.ListComments(ctx, api.ListCommentsRequestObject{IssueKey: in.Key})); err == nil && len(cs) > 0 {
			from := max(len(cs)-n, 0)
			fmt.Fprintf(&b, "\nComments (%d of %d, oldest first):\n", len(cs)-from, len(cs))
			for _, c := range cs[from:] {
				fmt.Fprintf(&b, "- %s, %s: %s\n", userText(c.Author), c.CreatedAt.UTC().Format("2006-01-02 15:04"), strings.TrimSpace(c.Body))
			}
		}
	}
	return text(b.String()), nil, nil
}

type boardIn struct {
	ProjectKey string `json:"projectKey" jsonschema:"project key"`
	Board      string `json:"board,omitempty" jsonschema:"board name or id (default: the project's first board)"`
}

func (t *tools) getBoard(ctx context.Context, _ *mcp.CallToolRequest, in boardIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	boards, err := as[api.ListBoards200JSONResponse](t.api.ListBoards(ctx, api.ListBoardsRequestObject{ProjectKey: in.ProjectKey}))
	if err != nil {
		return nil, nil, err
	}
	if len(boards) == 0 {
		return text(in.ProjectKey + " has no board."), nil, nil
	}
	board := boards[0]
	if in.Board != "" {
		k := slices.IndexFunc(boards, func(b api.Board) bool { return strings.EqualFold(b.Name, in.Board) || b.Id.String() == in.Board })
		if k < 0 {
			return nil, nil, fmt.Errorf("no board %q in %s", in.Board, in.ProjectKey)
		}
		board = boards[k]
	}
	data, err := as[api.GetBoardData200JSONResponse](t.api.GetBoardData(ctx, api.GetBoardDataRequestObject{BoardId: board.Id}))
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s board)\n", board.Name, board.Type)
	if board.Type == api.BoardTypeScrum {
		if data.Sprint == nil {
			b.WriteString("No active sprint.\n")
			return text(b.String()), nil, nil
		}
		fmt.Fprintf(&b, "Active sprint: %s\n", sprintText(*data.Sprint))
	}
	for _, c := range data.Columns {
		fmt.Fprintf(&b, "\n## %s (%d", c.Name, len(c.Issues))
		if c.WipLimit != nil {
			fmt.Fprintf(&b, "/%d WIP", *c.WipLimit)
			if c.OverLimit {
				b.WriteString(", over limit")
			}
		}
		b.WriteString(")\n")
		for _, i := range c.Issues {
			b.WriteString(issueLine(i) + "\n")
		}
	}
	return text(b.String()), nil, nil
}

type sprintIn struct {
	ProjectKey string `json:"projectKey" jsonschema:"project key"`
	Sprint     string `json:"sprint,omitempty" jsonschema:"sprint name or id (default: the active sprint, else the last completed one)"`
}

func (t *tools) getSprintReport(ctx context.Context, _ *mcp.CallToolRequest, in sprintIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	sprints, err := as[api.ListSprints200JSONResponse](t.api.ListSprints(ctx, api.ListSprintsRequestObject{ProjectKey: in.ProjectKey}))
	if err != nil {
		return nil, nil, err
	}
	var sp *api.Sprint
	for k := range sprints {
		s := &sprints[k]
		switch {
		case in.Sprint != "" && (strings.EqualFold(s.Name, in.Sprint) || s.Id.String() == in.Sprint):
			sp = s
		case in.Sprint == "" && s.State == api.SprintStateActive:
			sp = s
		case in.Sprint == "" && s.State == api.SprintStateClosed && (sp == nil || sp.State == api.SprintStateClosed && completed(s).After(completed(sp))):
			sp = s
		}
	}
	if sp == nil {
		return nil, nil, fmt.Errorf("no matching sprint in %s", in.ProjectKey)
	}
	rep, err := as[api.GetSprintReport200JSONResponse](t.api.GetSprintReport(ctx, api.GetSprintReportRequestObject{SprintId: sp.Id}))
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", sprintText(rep.Sprint))
	if rep.Sprint.Goal != "" {
		fmt.Fprintf(&b, "Goal: %s\n", rep.Sprint.Goal)
	}
	fmt.Fprintf(&b, "Committed at start: %s points, %d issues\n", num(rep.CommittedPoints), rep.CommittedIssues)
	fmt.Fprintf(&b, "Completed: %s points, %d issues\n", num(rep.CompletedPoints), rep.CompletedIssues)
	fmt.Fprintf(&b, "Added after start: %s points, %d issues; removed: %s points, %d issues\n", num(rep.AddedPoints), rep.AddedIssues, num(rep.RemovedPoints), rep.RemovedIssues)
	if len(rep.Burndown) > 0 {
		b.WriteString("\nDay        scope  done  remaining  ideal  (points; issues remaining)\n")
		for _, p := range rep.Burndown {
			fmt.Fprintf(&b, "%s %6s %5s %10s %6s  (%d left)\n", p.Date.String(), num(p.ScopePoints), num(p.CompletedPoints), num(p.RemainingPoints), num(p.IdealPoints), p.RemainingIssues)
		}
	}
	return text(b.String()), nil, nil
}

type metricsIn struct {
	Kind       string   `json:"kind" jsonschema:"summary, breakdown, cycle_time, created_resolved, cumulative_flow or velocity"`
	Projects   []string `json:"projects,omitempty" jsonschema:"project keys (default: every project you can browse)"`
	JQL        string   `json:"jql,omitempty" jsonschema:"extra JQL the issues must match"`
	Days       int      `json:"days,omitempty" jsonschema:"window in days ending today (default 30, at most 365)"`
	By         string   `json:"by,omitempty" jsonschema:"breakdown field: status, statusCategory, priority, type, assignee, label, epic, sprint, release or project"`
	Interval   string   `json:"interval,omitempty" jsonschema:"created_resolved buckets: day or week"`
	ProjectKey string   `json:"projectKey,omitempty" jsonschema:"velocity: the project"`
	Count      int      `json:"count,omitempty" jsonschema:"velocity: how many completed sprints (default 7)"`
}

func (t *tools) getMetrics(ctx context.Context, _ *mcp.CallToolRequest, in metricsIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	var projects *string
	if len(in.Projects) > 0 {
		p := strings.Join(in.Projects, ",")
		projects = &p
	}
	jql := &in.JQL
	days := in.Days
	if days == 0 {
		days = 30
	}
	var b strings.Builder
	switch in.Kind {
	case "summary":
		m, err := as[api.GetMetricsSummary200JSONResponse](t.api.GetMetricsSummary(ctx, api.GetMetricsSummaryRequestObject{Params: api.GetMetricsSummaryParams{Projects: projects, Jql: jql, Days: &days}}))
		if err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(&b, "Open: %d (%d to do, %d in progress; %s points), %d overdue, %d unassigned\n", m.Open, m.Todo, m.InProgress, num(m.OpenPoints), m.Overdue, m.Unassigned)
		fmt.Fprintf(&b, "Last %d days: %d created, %d resolved (%s points)", m.Days, m.Created, m.Resolved, num(m.ResolvedPoints))
		if m.MedianCycleHours != nil {
			fmt.Fprintf(&b, ", median cycle time %s", duration(*m.MedianCycleHours))
		}
		b.WriteString("\n")
	case "breakdown":
		if in.By == "" {
			return nil, nil, fmt.Errorf("breakdown needs `by`")
		}
		r, err := as[api.GetBreakdown200JSONResponse](t.api.GetBreakdown(ctx, api.GetBreakdownRequestObject{Params: api.GetBreakdownParams{Projects: projects, Jql: jql, By: api.BreakdownBy(in.By)}}))
		if err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(&b, "%d issues (%s points) by %s:\n", r.Total, num(r.TotalPoints), in.By)
		for _, g := range r.Groups {
			fmt.Fprintf(&b, "- %s: %d issues, %s points\n", g.Label, g.Count, num(g.Points))
		}
	case "cycle_time":
		r, err := as[api.GetCycleTimeMetrics200JSONResponse](t.api.GetCycleTimeMetrics(ctx, api.GetCycleTimeMetricsRequestObject{Params: api.GetCycleTimeMetricsParams{Projects: projects, Jql: jql, Days: &days}}))
		if err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(&b, "%d issues resolved in the last %d days. Cycle time: median %s, average %s, 85th percentile %s\n", len(r.Issues), r.Days, duration(r.MedianHours), duration(r.AverageHours), duration(r.P85Hours))
		slices.SortFunc(r.Issues, func(a, b api.CycleTimeIssue) int { return int(b.CycleHours - a.CycleHours) })
		for _, i := range r.Issues[:min(len(r.Issues), 10)] {
			fmt.Fprintf(&b, "- %s %s: %s\n", i.Key, i.Summary, duration(i.CycleHours))
		}
	case "created_resolved":
		interval := api.GetCreatedResolvedParamsInterval(cmpOr(in.Interval, "week"))
		r, err := as[api.GetCreatedResolved200JSONResponse](t.api.GetCreatedResolved(ctx, api.GetCreatedResolvedRequestObject{Params: api.GetCreatedResolvedParams{Projects: projects, Jql: jql, Days: &days, Interval: &interval}}))
		if err != nil {
			return nil, nil, err
		}
		b.WriteString("Period start  created  resolved  open at end\n")
		for _, k := range r.Buckets {
			fmt.Fprintf(&b, "%s  %7d  %8d  %11d\n", k.Start.String(), k.Created, k.Resolved, k.Open)
		}
	case "cumulative_flow":
		r, err := as[api.GetCumulativeFlow200JSONResponse](t.api.GetCumulativeFlow(ctx, api.GetCumulativeFlowRequestObject{Params: api.GetCumulativeFlowParams{Projects: projects, Jql: jql, Days: &days}}))
		if err != nil {
			return nil, nil, err
		}
		b.WriteString("Date       ")
		for _, s := range r.Series {
			fmt.Fprintf(&b, " %12s", s.Label)
		}
		b.WriteString("\n")
		step := max(len(r.Dates)/15, 1) // at most ~15 rows
		for k := 0; k < len(r.Dates); k += step {
			if k+step >= len(r.Dates) {
				k = len(r.Dates) - 1
			}
			b.WriteString(r.Dates[k].String())
			for _, s := range r.Series {
				fmt.Fprintf(&b, " %12d", s.Counts[k])
			}
			b.WriteString("\n")
		}
	case "velocity":
		if in.ProjectKey == "" {
			return nil, nil, fmt.Errorf("velocity needs projectKey")
		}
		count := in.Count
		if count == 0 {
			count = 7
		}
		r, err := as[api.GetVelocity200JSONResponse](t.api.GetVelocity(ctx, api.GetVelocityRequestObject{ProjectKey: in.ProjectKey, Params: api.GetVelocityParams{Count: &count}}))
		if err != nil {
			return nil, nil, err
		}
		if len(r.Sprints) == 0 {
			return text("No completed sprints yet."), nil, nil
		}
		fmt.Fprintf(&b, "Average completed: %s points, %s issues per sprint\n", num(r.AveragePoints), num(r.AverageIssues))
		for _, s := range r.Sprints {
			fmt.Fprintf(&b, "- %s: committed %s, completed %s points (%d/%d issues)\n", s.Sprint.Name, num(s.CommittedPoints), num(s.CompletedPoints), s.CompletedIssues, s.CommittedIssues)
		}
	default:
		return nil, nil, fmt.Errorf("unknown kind %q: use summary, breakdown, cycle_time, created_resolved, cumulative_flow or velocity", in.Kind)
	}
	return text(b.String()), nil, nil
}

type usersIn struct {
	Query   string `json:"query" jsonschema:"part of a name or email"`
	Project string `json:"project,omitempty" jsonschema:"only people who can be assigned in this project"`
}

func (t *tools) findUsers(ctx context.Context, _ *mcp.CallToolRequest, in usersIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	users, err := t.users(ctx, in.Query, in.Project)
	if err != nil {
		return nil, nil, err
	}
	if len(users) == 0 {
		return text("Nobody matches."), nil, nil
	}
	var b strings.Builder
	for _, u := range users {
		fmt.Fprintf(&b, "%s <%s>\n", u.DisplayName, u.Email)
	}
	return text(b.String()), nil, nil
}

func (t *tools) users(ctx context.Context, q, project string) ([]api.User, error) {
	limit := 20
	params := api.ListUsersParams{Q: &q, Limit: &limit}
	if project != "" {
		params.Project = &project
	}
	return as[api.ListUsers200JSONResponse](t.api.ListUsers(ctx, api.ListUsersRequestObject{Params: params}))
}

// ---------------------------------------------------------------------------
// Write tools

type createIn struct {
	ProjectKey  string   `json:"projectKey" jsonschema:"project key"`
	Type        string   `json:"type" jsonschema:"issue type key from get_project, e.g. story, task, bug, epic, subtask"`
	Summary     string   `json:"summary" jsonschema:"one-line title"`
	Description string   `json:"description,omitempty" jsonschema:"Markdown"`
	Priority    string   `json:"priority,omitempty" jsonschema:"highest, high, medium, low or lowest"`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"email, name, or me"`
	ParentKey   string   `json:"parentKey,omitempty" jsonschema:"epic (for a story/task/bug) or parent issue (for a sub-task)"`
	Labels      []string `json:"labels,omitempty"`
	StoryPoints *float64 `json:"storyPoints,omitempty"`
	Sprint      string   `json:"sprint,omitempty" jsonschema:"active, or a sprint name or id"`
	StartDate   string   `json:"startDate,omitempty" jsonschema:"YYYY-MM-DD"`
	DueDate     string   `json:"dueDate,omitempty" jsonschema:"YYYY-MM-DD"`
}

func (t *tools) createIssue(ctx context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	body := api.IssueCreate{ProjectKey: in.ProjectKey, TypeKey: in.Type, Summary: in.Summary, StoryPoints: in.StoryPoints}
	if in.Description != "" {
		body.Description = &in.Description
	}
	if in.Priority != "" {
		p := api.Priority(strings.ToLower(in.Priority))
		body.Priority = &p
	}
	if in.ParentKey != "" {
		body.ParentKey = &in.ParentKey
	}
	if len(in.Labels) > 0 {
		body.Labels = &in.Labels
	}
	if in.Assignee != "" {
		id, err := t.userID(ctx, in.Assignee, in.ProjectKey)
		if err != nil {
			return nil, nil, err
		}
		body.AssigneeId = &id
	}
	if in.Sprint != "" {
		id, err := t.sprintID(ctx, in.ProjectKey, in.Sprint)
		if err != nil {
			return nil, nil, err
		}
		body.SprintId = id
	}
	if body.StartDate, err = date(in.StartDate, "startDate"); err != nil {
		return nil, nil, err
	}
	if body.DueDate, err = date(in.DueDate, "dueDate"); err != nil {
		return nil, nil, err
	}
	i, err := as[api.CreateIssue201JSONResponse](t.api.CreateIssue(ctx, api.CreateIssueRequestObject{Body: &body}))
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("Created %s: %s (%s, %s)\n%s/browse/%s", i.Key, i.Summary, i.Type.Name, i.Status.Name, t.publicURL, i.Key)), nil, nil
}

type updateIn struct {
	Key         string   `json:"key" jsonschema:"issue key"`
	Summary     string   `json:"summary,omitempty"`
	Description *string  `json:"description,omitempty" jsonschema:"Markdown; replaces the whole description"`
	Priority    string   `json:"priority,omitempty" jsonschema:"highest, high, medium, low or lowest"`
	Type        string   `json:"type,omitempty" jsonschema:"change the issue type (key)"`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"email, name, or me"`
	ParentKey   string   `json:"parentKey,omitempty"`
	Labels      []string `json:"labels,omitempty" jsonschema:"replaces all labels"`
	StoryPoints *float64 `json:"storyPoints,omitempty"`
	Sprint      string   `json:"sprint,omitempty" jsonschema:"active, backlog, or a sprint name or id"`
	StartDate   string   `json:"startDate,omitempty" jsonschema:"YYYY-MM-DD"`
	DueDate     string   `json:"dueDate,omitempty" jsonschema:"YYYY-MM-DD"`
	Clear       []string `json:"clear,omitempty" jsonschema:"fields to empty: assignee, parent, sprint, storyPoints, startDate, dueDate, labels"`
}

func (t *tools) updateIssue(ctx context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	cur, err := as[api.GetIssue200JSONResponse](t.api.GetIssue(ctx, api.GetIssueRequestObject{IssueKey: in.Key}))
	if err != nil {
		return nil, nil, err
	}
	var p api.IssuePatch
	if in.Summary != "" {
		p.Summary = &in.Summary
	}
	p.Description = in.Description
	if in.Priority != "" {
		pr := api.Priority(strings.ToLower(in.Priority))
		p.Priority = &pr
	}
	if in.Type != "" {
		p.TypeKey = &in.Type
	}
	if in.Assignee != "" {
		id, err := t.userID(ctx, in.Assignee, cur.ProjectKey)
		if err != nil {
			return nil, nil, err
		}
		p.AssigneeId = nullable.NewNullableWithValue(id)
	}
	if in.ParentKey != "" {
		p.ParentKey = nullable.NewNullableWithValue(in.ParentKey)
	}
	if in.Labels != nil {
		p.Labels = &in.Labels
	}
	if in.StoryPoints != nil {
		p.StoryPoints = nullable.NewNullableWithValue(*in.StoryPoints)
	}
	if strings.EqualFold(in.Sprint, "backlog") {
		p.SprintId = nullable.NewNullNullable[openapi_types.UUID]()
	} else if in.Sprint != "" {
		id, err := t.sprintID(ctx, cur.ProjectKey, in.Sprint)
		if err != nil {
			return nil, nil, err
		}
		p.SprintId = nullable.NewNullableWithValue(*id)
	}
	for _, f := range []struct {
		in    string
		field string
		set   *nullable.Nullable[openapi_types.Date]
	}{{in.StartDate, "startDate", &p.StartDate}, {in.DueDate, "dueDate", &p.DueDate}} {
		d, err := date(f.in, f.field)
		if err != nil {
			return nil, nil, err
		}
		if d != nil {
			*f.set = nullable.NewNullableWithValue(*d)
		}
	}
	for _, f := range in.Clear {
		switch f {
		case "assignee":
			p.AssigneeId = nullable.NewNullNullable[openapi_types.UUID]()
		case "parent", "parentKey":
			p.ParentKey = nullable.NewNullNullable[string]()
		case "sprint":
			p.SprintId = nullable.NewNullNullable[openapi_types.UUID]()
		case "storyPoints":
			p.StoryPoints = nullable.NewNullNullable[float64]()
		case "startDate":
			p.StartDate = nullable.NewNullNullable[openapi_types.Date]()
		case "dueDate":
			p.DueDate = nullable.NewNullNullable[openapi_types.Date]()
		case "labels":
			p.Labels = &[]string{}
		default:
			return nil, nil, fmt.Errorf("cannot clear %q", f)
		}
	}
	i, err := as[api.UpdateIssue200JSONResponse](t.api.UpdateIssue(ctx, api.UpdateIssueRequestObject{IssueKey: in.Key, Body: &p}))
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("Updated %s (version %d).\n%s", i.Key, i.Version, issueLine(api.Issue{
		Key: i.Key, Type: i.Type, Summary: i.Summary, Status: i.Status, Priority: i.Priority, Assignee: i.Assignee, StoryPoints: i.StoryPoints, DueDate: i.DueDate,
	}))), nil, nil
}

type transitionIn struct {
	Key        string         `json:"key" jsonschema:"issue key"`
	Transition string         `json:"transition" jsonschema:"transition id, transition name, or target status name"`
	Comment    string         `json:"comment,omitempty" jsonschema:"optional comment to add with the transition"`
	Fields     map[string]any `json:"fields,omitempty" jsonschema:"screen field values the transition asks for, e.g. {\"resolution\": \"Done\"}"`
}

func (t *tools) transitionIssue(ctx context.Context, _ *mcp.CallToolRequest, in transitionIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	ts, err := as[api.ListTransitions200JSONResponse](t.api.ListTransitions(ctx, api.ListTransitionsRequestObject{IssueKey: in.Key}))
	if err != nil {
		return nil, nil, err
	}
	want := strings.TrimSpace(in.Transition)
	k := slices.IndexFunc(ts, func(tr api.AvailableTransition) bool { return tr.Id == want })
	if k < 0 {
		k = slices.IndexFunc(ts, func(tr api.AvailableTransition) bool { return strings.EqualFold(tr.Name, want) })
	}
	if k < 0 {
		k = slices.IndexFunc(ts, func(tr api.AvailableTransition) bool { return strings.EqualFold(tr.To.Name, want) })
	}
	if k < 0 {
		var names []string
		for _, tr := range ts {
			names = append(names, fmt.Sprintf("%s (%q → %s)", tr.Id, tr.Name, tr.To.Name))
		}
		if len(names) == 0 {
			return nil, nil, fmt.Errorf("%s has no transitions available to you from its current status", in.Key)
		}
		return nil, nil, fmt.Errorf("no transition %q from the current status; available: %s", want, strings.Join(names, ", "))
	}
	body := api.TransitionRequest{TransitionId: ts[k].Id}
	if len(in.Fields) > 0 {
		body.Fields = &in.Fields
	}
	if in.Comment != "" {
		body.Comment = &in.Comment
	}
	i, err := as[api.TransitionIssue200JSONResponse](t.api.TransitionIssue(ctx, api.TransitionIssueRequestObject{IssueKey: in.Key, Body: &body}))
	if err != nil {
		return nil, nil, err
	}
	msg := fmt.Sprintf("%s is now %s.", i.Key, i.Status.Name)
	if i.Assignee != nil {
		msg += " Assignee: " + userText(i.Assignee) + "."
	}
	return text(msg), nil, nil
}

type commentIn struct {
	Key  string `json:"key" jsonschema:"issue key"`
	Body string `json:"body" jsonschema:"Markdown"`
}

func (t *tools) addComment(ctx context.Context, _ *mcp.CallToolRequest, in commentIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	c, err := as[api.AddComment201JSONResponse](t.api.AddComment(ctx, api.AddCommentRequestObject{IssueKey: in.Key, Body: &api.CommentInput{Body: in.Body}}))
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("Commented on %s at %s.", in.Key, c.CreatedAt.UTC().Format("2006-01-02 15:04"))), nil, nil
}

type linkIn struct {
	Key       string `json:"key" jsonschema:"issue key"`
	Type      string `json:"type" jsonschema:"blocks, relates, duplicates or clones (key <type> targetKey)"`
	TargetKey string `json:"targetKey" jsonschema:"the other issue's key"`
}

func (t *tools) linkIssues(ctx context.Context, _ *mcp.CallToolRequest, in linkIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	lt := api.LinkType(strings.ToLower(in.Type))
	if !lt.Valid() {
		return nil, nil, fmt.Errorf("type must be blocks, relates, duplicates or clones")
	}
	if _, err := as[api.CreateIssueLink201JSONResponse](t.api.CreateIssueLink(ctx, api.CreateIssueLinkRequestObject{IssueKey: in.Key, Body: &api.CreateIssueLinkJSONRequestBody{Type: lt, TargetKey: in.TargetKey}})); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("Linked: %s %s %s.", in.Key, lt, in.TargetKey)), nil, nil
}

// ---------------------------------------------------------------------------
// Lookups and formatting

// userID resolves "me", an id, an email or a name to a user who can be assigned in project.
func (t *tools) userID(ctx context.Context, ref, project string) (openapi_types.UUID, error) {
	ref = strings.TrimSpace(ref)
	if strings.EqualFold(ref, "me") {
		if id := t.actor.UserID(); id != nil {
			return *id, nil
		}
	}
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	users, err := t.users(ctx, ref, project)
	if err != nil {
		return uuid.Nil, err
	}
	for _, u := range users {
		if strings.EqualFold(u.Email, ref) || strings.EqualFold(u.DisplayName, ref) {
			return u.Id, nil
		}
	}
	if len(users) == 1 {
		return users[0].Id, nil
	}
	if len(users) == 0 {
		return uuid.Nil, fmt.Errorf("nobody who can be assigned in %s matches %q", project, ref)
	}
	var names []string
	for _, u := range users {
		names = append(names, u.DisplayName+" <"+u.Email+">")
	}
	return uuid.Nil, fmt.Errorf("%q matches several people: %s; use an email", ref, strings.Join(names, ", "))
}

// sprintID resolves "active", an id or a name to one of the project's sprints.
func (t *tools) sprintID(ctx context.Context, project, ref string) (*openapi_types.UUID, error) {
	sprints, err := as[api.ListSprints200JSONResponse](t.api.ListSprints(ctx, api.ListSprintsRequestObject{ProjectKey: project}))
	if err != nil {
		return nil, err
	}
	for _, s := range sprints {
		if (strings.EqualFold(ref, "active") && s.State == api.SprintStateActive) || strings.EqualFold(s.Name, ref) || s.Id.String() == ref {
			id := s.Id
			return &id, nil
		}
	}
	if strings.EqualFold(ref, "active") {
		return nil, fmt.Errorf("%s has no active sprint", project)
	}
	return nil, fmt.Errorf("no sprint %q in %s", ref, project)
}

func issueLine(i api.Issue) string {
	parts := []string{i.Type.Name, i.Status.Name, userText(i.Assignee), string(i.Priority)}
	if i.StoryPoints != nil {
		parts = append(parts, num(*i.StoryPoints)+" pts")
	}
	if i.DueDate != nil {
		parts = append(parts, "due "+i.DueDate.String())
	}
	return fmt.Sprintf("%s [%s] %s", i.Key, strings.Join(parts, " · "), i.Summary)
}

func userText(u *api.User) string {
	if u == nil {
		return "Unassigned"
	}
	return u.DisplayName + " <" + u.Email + ">"
}

func sprintText(s api.Sprint) string {
	out := fmt.Sprintf("%s (%s", s.Name, s.State)
	if s.StartDate != nil && s.EndDate != nil {
		out += fmt.Sprintf(", %s → %s", s.StartDate.String(), s.EndDate.String())
	}
	return out + ")"
}

func categoryText(c api.StatusCategory) string {
	return map[api.StatusCategory]string{"todo": "to do", "in_progress": "in progress", "done": "done"}[c]
}

func completed(s *api.Sprint) time.Time {
	if s.CompletedAt == nil {
		return time.Time{}
	}
	return *s.CompletedAt
}

func date(v, field string) (*openapi_types.Date, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, fmt.Errorf("%s must be YYYY-MM-DD", field)
	}
	return &openapi_types.Date{Time: t}, nil
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

// duration renders hours the way people say them: 45m, 6h, 3.5d.
func duration(h float64) string {
	switch {
	case h < 1:
		return fmt.Sprintf("%.0fm", h*60)
	case h < 48:
		return strconv.FormatFloat(float64(int(h*10+0.5))/10, 'f', -1, 64) + "h"
	default:
		return strconv.FormatFloat(float64(int(h/24*10+0.5))/10, 'f', -1, 64) + "d"
	}
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
