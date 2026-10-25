package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

// Dashboards are personal pages of gadgets (charts, counts, issue lists), optionally shared
// with the whole organisation. A dashboard stores only gadget settings; each gadget's data is
// fetched with the viewer's own access, so sharing never reveals a private project.

type DashboardInput struct {
	Name, Description string
	Shared            bool
	Gadgets           []api.Gadget
}

func (s *Service) validateDashboard(ctx context.Context, actor *authz.Actor, in *DashboardInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		return apperr.Invalid("name", "name must be 1 to 80 characters")
	}
	if len(in.Description) > 500 {
		return apperr.Invalid("description", "description must be at most 500 characters")
	}
	if len(in.Gadgets) > 40 {
		return apperr.Invalid("gadgets", "a dashboard holds at most 40 gadgets")
	}
	seen := map[string]bool{}
	for k, g := range in.Gadgets {
		field := func(f string) string { return fmt.Sprintf("gadgets[%d].%s", k, f) }
		if g.Id == "" || len(g.Id) > 40 || seen[g.Id] {
			return apperr.Invalid(field("id"), "gadget ids must be unique and 1 to 40 characters")
		}
		seen[g.Id] = true
		if !g.Type.Valid() {
			return apperr.Invalid(field("type"), "unknown gadget type %q", g.Type)
		}
		if g.Width < 1 || g.Width > 3 {
			return apperr.Invalid(field("width"), "width must be 1, 2 or 3")
		}
		if g.Title != nil && len(*g.Title) > 80 {
			return apperr.Invalid(field("title"), "title must be at most 80 characters")
		}
		c := g.Config
		switch {
		case c.By != nil && !c.By.Valid():
			return apperr.Invalid(field("config.by"), "unknown breakdown field")
		case c.Stat != nil && !c.Stat.Valid():
			return apperr.Invalid(field("config.stat"), "unknown statistic")
		case c.Interval != nil && !c.Interval.Valid(), c.FlowBy != nil && !c.FlowBy.Valid(),
			c.Measure != nil && !c.Measure.Valid(), c.Chart != nil && !c.Chart.Valid():
			return apperr.Invalid(field("config"), "unknown option")
		case c.Days != nil && (*c.Days < 1 || *c.Days > 365):
			return apperr.Invalid(field("config.days"), "days must be between 1 and 365")
		case c.Count != nil && (*c.Count < 1 || *c.Count > 20):
			return apperr.Invalid(field("config.count"), "count must be between 1 and 20")
		case c.Limit != nil && (*c.Limit < 1 || *c.Limit > 50):
			return apperr.Invalid(field("config.limit"), "limit must be between 1 and 50")
		case c.ProjectKeys != nil && len(*c.ProjectKeys) > 20:
			return apperr.Invalid(field("config.projectKeys"), "at most 20 projects")
		}
		if c.Jql != nil && *c.Jql != "" {
			if len(*c.Jql) > 2000 {
				return apperr.Invalid(field("config.jql"), "the query must be at most 2000 characters")
			}
			if err := s.ValidateJQL(ctx, actor, *c.Jql); err != nil {
				if e, ok := err.(*apperr.Error); ok {
					return apperr.Invalid(field("config.jql"), "%s", e.Message)
				}
				return err
			}
		}
	}
	return nil
}

// DashboardView is a dashboard with its owner and whether the viewer may change it.
type DashboardView struct {
	Dashboard dbq.Dashboard
	Owner     dbq.User
	Gadgets   []api.Gadget
	CanEdit   bool
}

func (s *Service) dashboardView(ctx context.Context, actor *authz.Actor, d dbq.Dashboard, owners map[uuid.UUID]dbq.User) (DashboardView, error) {
	v := DashboardView{Dashboard: d, Gadgets: []api.Gadget{}, CanEdit: canEditDashboard(actor, d)}
	if err := json.Unmarshal(d.Gadgets, &v.Gadgets); err != nil {
		return v, fmt.Errorf("dashboard %s gadgets: %w", d.ID, err)
	}
	if u, ok := owners[d.OwnerID]; ok {
		v.Owner = u
		return v, nil
	}
	u, err := s.Q.GetUser(ctx, d.OwnerID)
	v.Owner = u
	return v, err
}

func canEditDashboard(actor *authz.Actor, d dbq.Dashboard) bool {
	uid := actor.UserID()
	return (uid != nil && *uid == d.OwnerID) || actor.IsOrgAdmin()
}

func (s *Service) ListDashboards(ctx context.Context, actor *authz.Actor) ([]DashboardView, error) {
	uid := actor.UserID()
	if uid == nil {
		uid = &uuid.Nil
	}
	ds, err := s.Q.ListDashboards(ctx, *uid)
	if err != nil {
		return nil, err
	}
	var ownerIDs []uuid.UUID
	for _, d := range ds {
		ownerIDs = append(ownerIDs, d.OwnerID)
	}
	users, err := s.Q.GetUsersByIDs(ctx, ownerIDs)
	if err != nil {
		return nil, err
	}
	owners := map[uuid.UUID]dbq.User{}
	for _, u := range users {
		owners[u.ID] = u
	}
	out := make([]DashboardView, 0, len(ds))
	for _, d := range ds {
		v, err := s.dashboardView(ctx, actor, d, owners)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// dashboardFor loads a dashboard the actor can see: their own, or a shared one.
func (s *Service) dashboardFor(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Dashboard, error) {
	d, err := s.Q.GetDashboard(ctx, id)
	if err != nil {
		return d, apperr.NotFound("dashboard")
	}
	uid := actor.UserID()
	if !d.Shared && (uid == nil || *uid != d.OwnerID) && !actor.IsOrgAdmin() {
		return d, apperr.NotFound("dashboard")
	}
	return d, nil
}

func (s *Service) GetDashboard(ctx context.Context, actor *authz.Actor, id uuid.UUID) (DashboardView, error) {
	d, err := s.dashboardFor(ctx, actor, id)
	if err != nil {
		return DashboardView{}, err
	}
	return s.dashboardView(ctx, actor, d, nil)
}

func (s *Service) CreateDashboard(ctx context.Context, actor *authz.Actor, in DashboardInput) (DashboardView, error) {
	if actor.User == nil {
		return DashboardView{}, apperr.Forbidden("only people can own dashboards")
	}
	if err := s.validateDashboard(ctx, actor, &in); err != nil {
		return DashboardView{}, err
	}
	gadgets, err := json.Marshal(in.Gadgets)
	if err != nil {
		return DashboardView{}, err
	}
	d, err := s.Q.CreateDashboard(ctx, dbq.CreateDashboardParams{OwnerID: actor.User.ID, Name: in.Name, Description: in.Description, Shared: in.Shared, Gadgets: gadgets})
	if err != nil {
		return DashboardView{}, err
	}
	return s.dashboardView(ctx, actor, d, nil)
}

func (s *Service) UpdateDashboard(ctx context.Context, actor *authz.Actor, id uuid.UUID, in DashboardInput) (DashboardView, error) {
	d, err := s.dashboardFor(ctx, actor, id)
	if err != nil {
		return DashboardView{}, err
	}
	if !canEditDashboard(actor, d) {
		return DashboardView{}, apperr.Forbidden("only the owner can change this dashboard")
	}
	if err := s.validateDashboard(ctx, actor, &in); err != nil {
		return DashboardView{}, err
	}
	gadgets, err := json.Marshal(in.Gadgets)
	if err != nil {
		return DashboardView{}, err
	}
	d, err = s.Q.UpdateDashboard(ctx, dbq.UpdateDashboardParams{ID: id, Name: in.Name, Description: in.Description, Shared: in.Shared, Gadgets: gadgets})
	if err != nil {
		return DashboardView{}, err
	}
	return s.dashboardView(ctx, actor, d, nil)
}

func (s *Service) DeleteDashboard(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	d, err := s.dashboardFor(ctx, actor, id)
	if err != nil {
		return err
	}
	if !canEditDashboard(actor, d) {
		return apperr.Forbidden("only the owner can delete this dashboard")
	}
	return s.Q.DeleteDashboard(ctx, id)
}
