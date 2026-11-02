package tracker

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

// Saved timelines: a named group of projects planned together on one timeline. They follow the
// saved-filter rules: private to their owner unless shared, changed only by the owner (or an org
// admin, for a shared one). Sharing grants no access: everyone sees only the projects they can
// browse, and the rest are counted.

const (
	maxTimelineViewProjects = 50
	maxTimelineViewName     = 100
	maxTimelineViewDesc     = 1000
)

// TimelineView is a saved timeline as one person sees it.
type TimelineView struct {
	View dbq.TimelineView
	// Projects are those the viewer can browse, in the timeline's order.
	Projects []dbq.Project
	// HiddenProjects counts the timeline's projects the viewer can't browse.
	HiddenProjects int
}

// TimelineViewInput creates or changes a saved timeline; Projects are project keys, in order.
type TimelineViewInput struct {
	Name        string
	Description string
	Shared      bool
	Projects    []string
}

// ListTimelineViews lists the actor's saved timelines and those shared with everyone, by name.
func (s *Service) ListTimelineViews(ctx context.Context, actor *authz.Actor) ([]TimelineView, error) {
	if actor.User == nil {
		return []TimelineView{}, nil
	}
	vs, err := s.Q.ListTimelineViews(ctx, actor.User.ID)
	if err != nil {
		return nil, err
	}
	return s.timelineViews(ctx, actor, s.Q, vs)
}

// GetTimelineView loads a saved timeline the actor owns or that is shared.
func (s *Service) GetTimelineView(ctx context.Context, actor *authz.Actor, id uuid.UUID) (TimelineView, error) {
	v, err := s.visibleTimelineView(ctx, s.Q, actor, id)
	if err != nil {
		return TimelineView{}, err
	}
	out, err := s.timelineViews(ctx, actor, s.Q, []dbq.TimelineView{v})
	if err != nil {
		return TimelineView{}, err
	}
	return out[0], nil
}

// CreateTimelineView saves a new timeline owned by the actor.
func (s *Service) CreateTimelineView(ctx context.Context, actor *authz.Actor, in TimelineViewInput) (TimelineView, error) {
	if actor.User == nil {
		return TimelineView{}, apperr.Forbidden("only people can save timelines")
	}
	in, err := checkTimelineViewInput(in)
	if err != nil {
		return TimelineView{}, err
	}
	projects, err := s.timelineViewProjects(ctx, actor, in.Projects)
	if err != nil {
		return TimelineView{}, err
	}
	var out TimelineView
	err = s.tx(ctx, func(_ pgx.Tx, q *dbq.Queries) error {
		v, err := q.CreateTimelineView(ctx, dbq.CreateTimelineViewParams{OwnerID: actor.User.ID, Name: in.Name, Description: in.Description, Shared: in.Shared})
		if err != nil {
			return err
		}
		if err := setTimelineViewProjects(ctx, q, v.ID, projects); err != nil {
			return err
		}
		views, err := s.timelineViews(ctx, actor, q, []dbq.TimelineView{v})
		if err != nil {
			return err
		}
		out = views[0]
		return nil
	})
	return out, err
}

// UpdateTimelineView changes a saved timeline. Its projects that the actor can't browse stay in it,
// after the ones they chose: someone editing a shared timeline mustn't drop what they can't see.
func (s *Service) UpdateTimelineView(ctx context.Context, actor *authz.Actor, id uuid.UUID, in TimelineViewInput) (TimelineView, error) {
	if _, err := s.editableTimelineView(ctx, s.Q, actor, id); err != nil {
		return TimelineView{}, err
	}
	in, err := checkTimelineViewInput(in)
	if err != nil {
		return TimelineView{}, err
	}
	projects, err := s.timelineViewProjects(ctx, actor, in.Projects)
	if err != nil {
		return TimelineView{}, err
	}
	var out TimelineView
	err = s.tx(ctx, func(_ pgx.Tx, q *dbq.Queries) error {
		v, err := s.editableTimelineView(ctx, q, actor, id)
		if err != nil {
			return err
		}
		current, err := q.TimelineViewProjects(ctx, []uuid.UUID{v.ID})
		if err != nil {
			return err
		}
		chosen := make(map[uuid.UUID]bool, len(projects))
		for _, p := range projects {
			chosen[p.ID] = true
		}
		for _, row := range current {
			if chosen[row.Project.ID] {
				continue
			}
			access, err := s.Authz.Access(ctx, actor, row.Project)
			if err != nil {
				return err
			}
			if !access.CanBrowse() {
				projects = append(projects, row.Project)
			}
		}
		if len(projects) > maxTimelineViewProjects {
			return apperr.Invalid("projects", "a timeline can show at most %d projects", maxTimelineViewProjects)
		}
		if v, err = q.UpdateTimelineView(ctx, dbq.UpdateTimelineViewParams{ID: v.ID, Name: in.Name, Description: in.Description, Shared: in.Shared}); err != nil {
			return err
		}
		if err := q.ClearTimelineViewProjects(ctx, v.ID); err != nil {
			return err
		}
		if err := setTimelineViewProjects(ctx, q, v.ID, projects); err != nil {
			return err
		}
		views, err := s.timelineViews(ctx, actor, q, []dbq.TimelineView{v})
		if err != nil {
			return err
		}
		out = views[0]
		return nil
	})
	return out, err
}

// DeleteTimelineView deletes a saved timeline (its owner, or an org admin for a shared one).
func (s *Service) DeleteTimelineView(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	v, err := s.editableTimelineView(ctx, s.Q, actor, id)
	if err != nil {
		return err
	}
	return s.Q.DeleteTimelineView(ctx, v.ID)
}

// visibleTimelineView loads a timeline the actor owns or that is shared; others read as not found.
func (s *Service) visibleTimelineView(ctx context.Context, q *dbq.Queries, actor *authz.Actor, id uuid.UUID) (dbq.TimelineView, error) {
	v, err := q.GetTimelineView(ctx, id)
	if err != nil || actor.User == nil || (v.OwnerID != actor.User.ID && !v.Shared) {
		return dbq.TimelineView{}, apperr.NotFound("timeline")
	}
	return v, nil
}

// editableTimelineView loads a timeline the actor may change: their own, or a shared one for an
// org admin.
func (s *Service) editableTimelineView(ctx context.Context, q *dbq.Queries, actor *authz.Actor, id uuid.UUID) (dbq.TimelineView, error) {
	v, err := s.visibleTimelineView(ctx, q, actor, id)
	if err != nil {
		return v, err
	}
	if v.OwnerID != actor.User.ID && !actor.IsOrgAdmin() {
		return v, apperr.Forbidden("only the owner can change this timeline")
	}
	return v, nil
}

func checkTimelineViewInput(in TimelineViewInput) (TimelineViewInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		return in, apperr.Invalid("name", "give the timeline a name")
	case n > maxTimelineViewName:
		return in, apperr.Invalid("name", "a timeline's name can be at most %d characters", maxTimelineViewName)
	}
	if utf8.RuneCountInString(in.Description) > maxTimelineViewDesc {
		return in, apperr.Invalid("description", "a timeline's description can be at most %d characters", maxTimelineViewDesc)
	}
	return in, nil
}

// timelineViewProjects resolves the chosen keys: each must be a project the actor can browse (else
// not found, as on the timeline itself) and not a programme, which has its own timeline.
func (s *Service) timelineViewProjects(ctx context.Context, actor *authz.Actor, keys []string) ([]dbq.Project, error) {
	var out []dbq.Project
	seen := map[uuid.UUID]bool{}
	for _, k := range keys {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		p, _, err := s.ProjectFor(ctx, actor, k, authz.ProjectBrowse)
		if err != nil {
			return nil, err
		}
		if IsProgramme(p) {
			return nil, apperr.Unprocessable("programme_timeline", "%s is a programme, which has its own timeline", p.Key)
		}
		if !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	switch {
	case len(out) == 0:
		return nil, apperr.Invalid("projects", "choose at least one project")
	case len(out) > maxTimelineViewProjects:
		return nil, apperr.Invalid("projects", "a timeline can show at most %d projects", maxTimelineViewProjects)
	}
	return out, nil
}

func setTimelineViewProjects(ctx context.Context, q *dbq.Queries, viewID uuid.UUID, projects []dbq.Project) error {
	for i, p := range projects {
		if err := q.AddTimelineViewProject(ctx, dbq.AddTimelineViewProjectParams{ViewID: viewID, ProjectID: p.ID, Position: int32(i)}); err != nil {
			return err
		}
	}
	return nil
}

// timelineViews attaches each view's projects, keeping those the actor can browse and counting the rest.
func (s *Service) timelineViews(ctx context.Context, actor *authz.Actor, q *dbq.Queries, vs []dbq.TimelineView) ([]TimelineView, error) {
	out := make([]TimelineView, len(vs))
	if len(vs) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(vs))
	index := make(map[uuid.UUID]int, len(vs))
	for i, v := range vs {
		ids[i] = v.ID
		index[v.ID] = i
		out[i] = TimelineView{View: v, Projects: []dbq.Project{}}
	}
	rows, err := q.TimelineViewProjects(ctx, ids)
	if err != nil {
		return nil, err
	}
	browse := map[uuid.UUID]bool{}
	for _, row := range rows {
		ok, known := browse[row.Project.ID]
		if !known {
			access, err := s.Authz.Access(ctx, actor, row.Project)
			if err != nil {
				return nil, err
			}
			ok = access.CanBrowse()
			browse[row.Project.ID] = ok
		}
		v := &out[index[row.ViewID]]
		if ok {
			v.Projects = append(v.Projects, row.Project)
		} else {
			v.HiddenProjects++
		}
	}
	return out, nil
}
