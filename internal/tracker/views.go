package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

func UserView(u dbq.User) api.User {
	return api.User{Id: u.ID, DisplayName: u.DisplayName, Email: u.Email, AvatarUrl: u.AvatarUrl, Kind: api.UserKind(u.Kind)}
}

func StatusView(s dbq.Status) api.Status {
	return api.Status{Id: s.ID, Name: s.Name, Category: api.StatusCategory(s.Category)}
}

func TypeRef(t dbq.IssueType) api.IssueTypeRef {
	return api.IssueTypeRef{Id: t.ID, Key: t.Key, Name: t.Name, Icon: t.Icon, Color: t.Color, Level: int(t.Level), Kind: api.IssueTypeKind(cmpOr(t.Kind, KindWork))}
}

func dateView(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}

func SprintView(sp dbq.Sprint) api.Sprint {
	return api.Sprint{
		Id: sp.ID, Name: sp.Name, Goal: sp.Goal, State: api.SprintState(sp.State), Position: int(sp.Position),
		StartDate: dateView(sp.StartDate), EndDate: dateView(sp.EndDate), CompletedAt: sp.CompletedAt,
	}
}

func ReleaseView(r dbq.Release, issueCount, doneCount int) api.Release {
	return api.Release{
		Id: r.ID, Name: r.Name, Description: r.Description, State: api.ReleaseState(r.State), Position: int(r.Position),
		StartDate: dateView(r.StartDate), ReleaseDate: dateView(r.ReleaseDate), ReleasedAt: r.ReleasedAt,
		IssueCount: issueCount, DoneCount: doneCount,
	}
}

func (s *Service) ProjectView(ctx context.Context, p dbq.Project, access authz.Access) (api.Project, error) {
	v := api.Project{
		Id: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, Template: p.Template,
		Access: api.ProjectAccessPrivate, RisksDecisions: p.RisksDecisions, CreatedAt: p.CreatedAt, MyPermissions: access.List(),
	}
	if v.MyPermissions == nil {
		v.MyPermissions = []string{}
	}
	if p.OpenRoleID != nil {
		v.Access = api.ProjectAccessOpen
		if r, err := s.Q.GetRole(ctx, *p.OpenRoleID); err == nil {
			v.OpenRoleKey = &r.Key
		}
	}
	if p.LeadID != nil {
		if u, err := s.Q.GetUser(ctx, *p.LeadID); err == nil {
			uv := UserView(u)
			v.Lead = &uv
		}
	}
	aliases, err := s.Q.ProjectKeyAliases(ctx, p.ID)
	if err != nil {
		return v, err
	}
	if len(aliases) > 0 {
		v.KeyAliases = &aliases
	}
	return v, nil
}

// lookups batch-loads everything issue cards reference.
type lookups struct {
	projects map[uuid.UUID]dbq.Project
	types    map[uuid.UUID]dbq.IssueType
	statuses map[uuid.UUID]dbq.Status
	users    map[uuid.UUID]dbq.User
	parents  map[uuid.UUID]dbq.Issue
	dev      map[uuid.UUID]dbq.DevSummariesRow
	spent    map[uuid.UUID]int64
}

func (s *Service) loadLookups(ctx context.Context, issues []dbq.Issue) (*lookups, error) {
	l := &lookups{
		projects: map[uuid.UUID]dbq.Project{}, types: map[uuid.UUID]dbq.IssueType{}, statuses: map[uuid.UUID]dbq.Status{},
		users: map[uuid.UUID]dbq.User{}, parents: map[uuid.UUID]dbq.Issue{}, dev: map[uuid.UUID]dbq.DevSummariesRow{},
	}
	var projectIDs, typeIDs, statusIDs, userIDs, parentIDs, issueIDs []uuid.UUID
	for _, i := range issues {
		issueIDs = append(issueIDs, i.ID)
		projectIDs = append(projectIDs, i.ProjectID)
		typeIDs = append(typeIDs, i.TypeID)
		statusIDs = append(statusIDs, i.StatusID)
		userIDs = append(userIDs, compactIDs(i.AssigneeID, i.ReporterID)...)
		parentIDs = append(parentIDs, compactIDs(i.ParentID)...)
	}
	parents, err := s.Q.GetIssuesByIDs(ctx, parentIDs)
	if err != nil {
		return nil, err
	}
	for _, par := range parents {
		l.parents[par.ID] = par
		typeIDs = append(typeIDs, par.TypeID)
		statusIDs = append(statusIDs, par.StatusID)
		projectIDs = append(projectIDs, par.ProjectID)
	}
	projects, err := s.Q.ListProjectsByIDs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		l.projects[p.ID] = p
	}
	types, err := s.Q.GetIssueTypesByIDs(ctx, typeIDs)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		l.types[t.ID] = t
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
	dev, err := s.Q.DevSummaries(ctx, issueIDs)
	if err != nil {
		return nil, err
	}
	for _, d := range dev {
		l.dev[d.IssueID] = d
	}
	if l.spent, err = s.TimeSpent(ctx, s.Q, issueIDs); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *lookups) ref(i dbq.Issue) api.IssueRef {
	return api.IssueRef{
		Id: i.ID, Key: issueKey(l.projects[i.ProjectID], i), Summary: i.Summary,
		Type: TypeRef(l.types[i.TypeID]), Status: StatusView(l.statuses[i.StatusID]),
	}
}

func (l *lookups) issue(i dbq.Issue) api.Issue {
	p := l.projects[i.ProjectID]
	v := api.Issue{
		Id: i.ID, Key: issueKey(p, i), ProjectKey: p.Key, Type: TypeRef(l.types[i.TypeID]), Summary: i.Summary,
		Status: StatusView(l.statuses[i.StatusID]), Priority: api.Priority(i.Priority), SprintId: i.SprintID,
		StoryPoints: i.StoryPoints, StartDate: dateView(i.StartDate), DueDate: dateView(i.DueDate),
		Labels: i.Labels, Resolution: i.Resolution, ResolvedAt: i.ResolvedAt, Rank: i.Rank, Version: int(i.Version),
		Fields: map[string]any{}, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, StatusChangedAt: &i.StatusChangedAt,
		TimeTracking: &api.TimeTracking{OriginalSeconds: i.OriginalEstimateSeconds, RemainingSeconds: i.RemainingEstimateSeconds, SpentSeconds: l.spent[i.ID]},
	}
	if v.Labels == nil {
		v.Labels = []string{}
	}
	_ = json.Unmarshal(i.Fields, &v.Fields)
	if i.AssigneeID != nil {
		if u, ok := l.users[*i.AssigneeID]; ok {
			uv := UserView(u)
			v.Assignee = &uv
		}
	}
	if i.ReporterID != nil {
		if u, ok := l.users[*i.ReporterID]; ok {
			uv := UserView(u)
			v.Reporter = &uv
		}
	}
	if i.ParentID != nil {
		if par, ok := l.parents[*i.ParentID]; ok {
			r := l.ref(par)
			v.Parent = &r
		}
	}
	if d, ok := l.dev[i.ID]; ok {
		ds := api.DevSummary{
			Branches: int(d.Branches), Commits: int(d.Commits), PullRequests: int(d.PullRequests),
			OpenPullRequests: int(d.OpenPullRequests), MergedPullRequests: int(d.MergedPullRequests),
		}
		if d.PipelineState != "" {
			ds.PipelineState = &d.PipelineState
		}
		v.Dev = &ds
	}
	return v
}

// IssueViews renders a list of issues with their related data in a fixed number of queries.
func (s *Service) IssueViews(ctx context.Context, issues []dbq.Issue) ([]api.Issue, error) {
	out := make([]api.Issue, 0, len(issues))
	if len(issues) == 0 {
		return out, nil
	}
	l, err := s.loadLookups(ctx, issues)
	if err != nil {
		return nil, err
	}
	for _, i := range issues {
		out = append(out, l.issue(i))
	}
	return out, nil
}

func (s *Service) IssueView(ctx context.Context, i dbq.Issue) (api.Issue, error) {
	vs, err := s.IssueViews(ctx, []dbq.Issue{i})
	if err != nil {
		return api.Issue{}, err
	}
	return vs[0], nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// SuggestedBranch names a branch from the key and summary: PLAT-12-fix-login-timeout.
func SuggestedBranch(key, summary string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(summary), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		return key
	}
	return key + "-" + slug
}

// IssueDetail renders the full issue view.
func (s *Service) IssueDetail(ctx context.Context, actor *authz.Actor, i dbq.Issue) (api.IssueDetail, error) {
	p, err := s.Q.GetProject(ctx, i.ProjectID)
	if err != nil {
		return api.IssueDetail{}, err
	}
	access, err := s.Authz.Access(ctx, actor, p)
	if err != nil {
		return api.IssueDetail{}, err
	}
	children, err := s.Q.ChildIssues(ctx, &i.ID)
	if err != nil {
		return api.IssueDetail{}, err
	}
	linkRows, err := s.Q.IssueLinks(ctx, i.ID)
	if err != nil {
		return api.IssueDetail{}, err
	}
	var otherIDs []uuid.UUID
	for _, lk := range linkRows {
		if lk.SourceID == i.ID {
			otherIDs = append(otherIDs, lk.TargetID)
		} else {
			otherIDs = append(otherIDs, lk.SourceID)
		}
	}
	others, err := s.Q.GetIssuesByIDs(ctx, otherIDs)
	if err != nil {
		return api.IssueDetail{}, err
	}
	all := append(append([]dbq.Issue{i}, children...), others...)
	l, err := s.loadLookups(ctx, all)
	if err != nil {
		return api.IssueDetail{}, err
	}
	base := l.issue(i)
	d := api.IssueDetail{
		Id: base.Id, Key: base.Key, ProjectKey: base.ProjectKey, Type: base.Type, Summary: base.Summary, Status: base.Status,
		Priority: base.Priority, Assignee: base.Assignee, Reporter: base.Reporter, Parent: base.Parent, SprintId: base.SprintId,
		StoryPoints: base.StoryPoints, StartDate: base.StartDate, DueDate: base.DueDate, Labels: base.Labels,
		Resolution: base.Resolution, ResolvedAt: base.ResolvedAt, Rank: base.Rank, Version: base.Version, Fields: base.Fields,
		Dev: base.Dev, StatusChangedAt: base.StatusChangedAt, CreatedAt: base.CreatedAt, UpdatedAt: base.UpdatedAt, TimeTracking: base.TimeTracking,
		Description: i.Description,
		Project:     api.ProjectRef{Id: p.ID, Key: p.Key, Name: p.Name},
		Children:    []api.Issue{}, Links: []api.IssueLink{}, Development: []api.DevLink{}, Attachments: []api.Attachment{}, Releases: []api.Release{},
	}
	for _, c := range children {
		d.Children = append(d.Children, l.issue(c))
	}
	if t := l.types[i.TypeID]; t.Level > 0 {
		prog := api.IssueProgress{}
		rows, err := s.Q.EpicProgress(ctx, []uuid.UUID{i.ID})
		if err != nil {
			return d, err
		}
		for _, r := range rows {
			prog.Total += int(r.N)
			prog.Points += r.Points
			switch r.Category {
			case "done":
				prog.Done += int(r.N)
				prog.DonePoints += r.Points
			case "in_progress":
				prog.InProgress += int(r.N)
			}
		}
		d.Progress = &prog
	}
	byID := map[uuid.UUID]dbq.Issue{}
	for _, o := range others {
		byID[o.ID] = o
	}
	for _, lk := range linkRows {
		outward := lk.SourceID == i.ID
		otherID := lk.TargetID
		if !outward {
			otherID = lk.SourceID
		}
		other, ok := byID[otherID]
		if !ok {
			continue
		}
		dir := api.LinkDirectionOutward
		if !outward {
			dir = api.LinkDirectionInward
		}
		d.Links = append(d.Links, api.IssueLink{Id: lk.ID, Type: api.LinkType(lk.LinkType), Direction: dir, Label: LinkLabel(lk.LinkType, outward), Issue: l.ref(other)})
	}
	dev, err := s.Q.IssueDevLinks(ctx, i.ID)
	if err != nil {
		return d, err
	}
	for _, dl := range dev {
		data := map[string]any{}
		_ = json.Unmarshal(dl.Data, &data)
		d.Development = append(d.Development, api.DevLink{
			Id: dl.ID, Kind: api.DevLinkKind(dl.Kind), ExternalId: dl.ExternalID, Title: dl.Title, Url: dl.Url, State: dl.State,
			AuthorName: dl.AuthorName, Data: data, UpdatedAt: dl.UpdatedAt,
			Repository: api.RepositoryRef{Id: dl.RepositoryID, Provider: api.ScmProvider(dl.Provider), FullPath: dl.FullPath, WebUrl: dl.RepoWebUrl},
		})
	}
	atts, err := s.Q.ListAttachments(ctx, i.ID)
	if err != nil {
		return d, err
	}
	for _, a := range atts {
		av := api.Attachment{Id: a.ID, Filename: a.Filename, ContentType: a.ContentType, Size: a.SizeBytes, CreatedAt: a.CreatedAt, Url: fmt.Sprintf("/api/v1/attachments/%s", a.ID)}
		if IsServiceProject(p) {
			vis := api.VisibilityInternal
			if a.Public {
				vis = api.VisibilityPublic
			}
			av.Visibility = &vis
		}
		if a.UploaderID != nil {
			if u, err := s.Q.GetUser(ctx, *a.UploaderID); err == nil {
				uv := UserView(u)
				av.Uploader = &uv
			}
		}
		d.Attachments = append(d.Attachments, av)
	}
	rels, err := s.Q.IssueReleases(ctx, i.ID)
	if err != nil {
		return d, err
	}
	for _, r := range rels {
		d.Releases = append(d.Releases, ReleaseView(r, 0, 0))
	}
	if i.SprintID != nil {
		if sp, err := s.Q.GetSprint(ctx, *i.SprintID); err == nil {
			sv := SprintView(sp)
			d.Sprint = &sv
		}
	}
	watchers, err := s.Q.WatcherIDs(ctx, i.ID)
	if err != nil {
		return d, err
	}
	d.WatcherCount = len(watchers)
	if uid := actor.UserID(); uid != nil {
		for _, w := range watchers {
			if w == *uid {
				d.Watching = true
			}
		}
	}
	branch := SuggestedBranch(d.Key, i.Summary)
	d.SuggestedBranch = &branch
	d.Permissions = api.IssuePermissions{
		Edit: canEdit(access, actor, i), Delete: access.Has(authz.IssueDelete), Transition: access.Has(authz.IssueTransition),
		Comment: access.Has(authz.IssueComment), Assign: access.Has(authz.IssueAssign),
	}
	return d, nil
}
