package tracker

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/search"
)

// timelineLimit caps the issues one timeline draws; the response says when more matched.
const timelineLimit = 2000

// Timeline is what the timeline view draws: epics and standard issues with their dates, the
// dependencies between them, and each project's sprints and releases.
type Timeline struct {
	Projects  []dbq.Project
	Issues    []dbq.Issue
	Links     []dbq.IssueLink
	Sprints   []ProjectSprint
	Releases  []ProjectRelease
	Truncated bool
}

type ProjectSprint struct {
	ProjectKey string
	Sprint     dbq.Sprint
}

type ProjectRelease struct {
	ProjectKey string
	Release    dbq.ListReleasesRow
}

// Timeline loads the given projects (all browsable ones when keys is empty). Sub-tasks are left
// out, and so are issues resolved more than doneDays ago.
func (s *Service) Timeline(ctx context.Context, actor *authz.Actor, keys []string, jql string, doneDays int) (Timeline, error) {
	var out Timeline
	var err error
	if out.Projects, err = s.projectsByKeys(ctx, actor, keys); err != nil {
		return out, err
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
			"i.type_id IN (SELECT id FROM issue_types WHERE level >= 0)",
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
