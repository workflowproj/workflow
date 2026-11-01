package tracker

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/search"
)

// timelineLimit caps the issues one timeline draws; the response says when more matched.
const timelineLimit = 2000

// Timeline is what the timeline view draws: epics and standard issues with their dates, the
// dependencies between them, and each project's sprints and releases.
type Timeline struct {
	Projects []dbq.Project
	Issues   []dbq.Issue
	Links    []dbq.IssueLink
	// External holds blocks links from a loaded issue to one that isn't loaded.
	External  []dbq.IssueLink
	Sprints   []ProjectSprint
	Releases  []ProjectRelease
	Truncated bool
}

// TimelineQuery chooses what a timeline shows: the projects with Keys (every browsable project
// when empty), or a programme's initiatives and the teams the actor can browse.
type TimelineQuery struct {
	Keys      []string
	Programme string
	JQL       string
	DoneDays  int
}

type ProjectSprint struct {
	ProjectKey string
	Sprint     dbq.Sprint
}

type ProjectRelease struct {
	ProjectKey string
	Release    dbq.ListReleasesRow
}

// Timeline loads the given projects (all browsable ones when keys is empty). Sub-tasks, risks and
// decisions are left out, and so are issues resolved more than doneDays ago.
func (s *Service) Timeline(ctx context.Context, actor *authz.Actor, tq TimelineQuery) (Timeline, error) {
	var out Timeline
	var err error
	jql, doneDays := tq.JQL, tq.DoneDays
	switch {
	case tq.Programme != "" && len(tq.Keys) > 0:
		return out, apperr.Invalid("programme", "choose a programme or projects, not both")
	case tq.Programme != "":
		// A programme's timeline covers the teams the actor can browse; the others are left out
		// rather than failing the request.
		prog, err := s.programmeFor(ctx, actor, tq.Programme, authz.ProjectBrowse)
		if err != nil {
			return out, err
		}
		teams, err := s.programmeTeams(ctx, s.Q, actor, prog)
		if err != nil {
			return out, err
		}
		out.Projects = append([]dbq.Project{prog}, teams.Teams...)
	default:
		if out.Projects, err = s.projectsByKeys(ctx, actor, tq.Keys); err != nil {
			return out, err
		}
	}
	if doneDays < 0 || doneDays > 3650 {
		return out, apperr.Invalid("doneDays", "doneDays must be between 0 and 3650")
	}
	projectIDs := make([]uuid.UUID, len(out.Projects))
	for i, p := range out.Projects {
		projectIDs[i] = p.ID
	}
	issues, total, err := s.runSearch(ctx, actor, projectIDs, SearchQuery{
		JQL: jql,
		Extra: []string{
			"i.type_id IN (SELECT id FROM issue_types WHERE level >= 0 AND kind = 'work')",
			"(i.resolved_at IS NULL OR i.resolved_at >= now() - make_interval(days => $2::int))",
		},
		ExtraArgs:    []any{doneDays},
		DefaultOrder: []search.Order{{Field: "rank"}},
		Limit:        timelineLimit,
	})
	if err != nil {
		return out, err
	}
	// Group by project (in project order), keeping rank order within each.
	order := make(map[uuid.UUID]int, len(out.Projects))
	for i, p := range out.Projects {
		order[p.ID] = i
	}
	slices.SortStableFunc(issues, func(a, b dbq.Issue) int { return order[a.ProjectID] - order[b.ProjectID] })
	out.Issues, out.Truncated = issues, total > len(issues)

	ids := make([]uuid.UUID, len(issues))
	for i, is := range issues {
		ids[i] = is.ID
	}
	if out.Links, err = s.Q.BlockLinksAmong(ctx, ids); err != nil {
		return out, err
	}
	if out.External, err = s.Q.BlockLinksTouching(ctx, ids); err != nil {
		return out, err
	}
	for _, p := range out.Projects {
		sprints, err := s.Q.ListSprints(ctx, dbq.ListSprintsParams{ProjectID: p.ID})
		if err != nil {
			return out, err
		}
		for _, sp := range sprints {
			out.Sprints = append(out.Sprints, ProjectSprint{ProjectKey: p.Key, Sprint: sp})
		}
		releases, err := s.Q.ListReleases(ctx, p.ID)
		if err != nil {
			return out, err
		}
		for _, r := range releases {
			out.Releases = append(out.Releases, ProjectRelease{ProjectKey: p.Key, Release: r})
		}
	}
	return out, nil
}

// ProjectsByKeys resolves project keys the actor may browse, in the order given; no keys means
// every browsable project.
func (s *Service) ProjectsByKeys(ctx context.Context, actor *authz.Actor, keys []string) ([]dbq.Project, error) {
	return s.projectsByKeys(ctx, actor, keys)
}

func (s *Service) projectsByKeys(ctx context.Context, actor *authz.Actor, keys []string) ([]dbq.Project, error) {
	var out []dbq.Project
	if len(keys) == 0 {
		ps, err := s.ListProjects(ctx, actor)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			out = append(out, p.Project)
		}
		return out, nil
	}
	seen := map[uuid.UUID]bool{}
	for _, k := range keys {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		p, _, err := s.ProjectFor(ctx, actor, k, authz.ProjectBrowse)
		if err != nil {
			return nil, err
		}
		if !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// ExternalDependencies renders a timeline's links to issues it doesn't show. The other issue is
// named (with a link id to remove it by) only when actor can browse it; otherwise just whether
// it is still open.
func (s *Service) ExternalDependencies(ctx context.Context, actor *authz.Actor, tl Timeline, keyOf map[uuid.UUID]string) ([]api.ExternalDependency, error) {
	out := []api.ExternalDependency{}
	if len(tl.External) == 0 {
		return out, nil
	}
	otherIDs := make([]uuid.UUID, 0, len(tl.External))
	for _, l := range tl.External {
		if _, ok := keyOf[l.SourceID]; ok {
			otherIDs = append(otherIDs, l.TargetID)
		} else {
			otherIDs = append(otherIDs, l.SourceID)
		}
	}
	others, err := s.Q.GetIssuesByIDs(ctx, otherIDs)
	if err != nil {
		return nil, err
	}
	visible, err := s.browsableIssues(ctx, actor, uuid.Nil, others)
	if err != nil {
		return nil, err
	}
	views, err := s.IssueViews(ctx, actor, visible)
	if err != nil {
		return nil, err
	}
	viewByID := map[uuid.UUID]api.Issue{}
	for _, v := range views {
		viewByID[v.Id] = v
	}
	statuses := map[uuid.UUID]uuid.UUID{}
	for _, o := range others {
		statuses[o.ID] = o.StatusID
	}
	var statusIDs []uuid.UUID
	for _, id := range statuses {
		statusIDs = append(statusIDs, id)
	}
	sts, err := s.Q.GetStatusesByIDs(ctx, statusIDs)
	if err != nil {
		return nil, err
	}
	done := map[uuid.UUID]bool{}
	for _, st := range sts {
		done[st.ID] = st.Category == "done"
	}
	for _, l := range tl.External {
		dep := api.ExternalDependency{IssueKey: keyOf[l.SourceID], Direction: api.ExternalDependencyDirectionBlocks}
		other := l.TargetID
		if _, ok := keyOf[l.SourceID]; !ok {
			dep = api.ExternalDependency{IssueKey: keyOf[l.TargetID], Direction: api.ExternalDependencyDirectionBlockedBy}
			other = l.SourceID
		}
		st, ok := statuses[other]
		if !ok {
			continue // deleted meanwhile
		}
		dep.Open = !done[st]
		if v, ok := viewByID[other]; ok {
			id := l.ID
			dep.LinkId, dep.Other = &id, &v
		}
		out = append(out, dep)
	}
	return out, nil
}
