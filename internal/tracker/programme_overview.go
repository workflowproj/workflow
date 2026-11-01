package tracker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/search"
)

const (
	// overviewInitiatives and overviewEpics cap a programme overview; the response says when more matched.
	overviewInitiatives = 500
	overviewEpics       = 2000
	// overviewUnlinked caps the epics listed as belonging to no initiative.
	overviewUnlinked = 50
)

// ProgrammeOverview is a programme's roadmap: its initiatives, the epics under them in the teams
// actor can browse, the work under those epics by status category, forecasts and flags. Teams
// actor can't browse are left out, not counted.
func (s *Service) ProgrammeOverview(ctx context.Context, actor *authz.Actor, ref string, doneDays int) (api.ProgrammeOverview, error) {
	var out api.ProgrammeOverview
	if doneDays < 0 || doneDays > 3650 {
		return out, apperr.Invalid("doneDays", "doneDays must be between 0 and 3650")
	}
	prog, err := s.programmeFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return out, err
	}
	teams, err := s.programmeTeams(ctx, s.Q, actor, prog)
	if err != nil {
		return out, err
	}
	out = api.ProgrammeOverview{
		Programme: api.ProjectRef{Id: prog.ID, Key: prog.Key, Name: prog.Name}, Teams: []api.ProjectRef{},
		HiddenTeamCount: teams.Hidden, Initiatives: []api.ProgrammeInitiative{}, UnlinkedEpics: []api.Issue{},
	}
	teamIDs := make([]uuid.UUID, 0, len(teams.Teams))
	for _, t := range teams.Teams {
		out.Teams = append(out.Teams, api.ProjectRef{Id: t.ID, Key: t.Key, Name: t.Name})
		teamIDs = append(teamIDs, t.ID)
	}

	inits, total, err := s.runSearch(ctx, actor, []uuid.UUID{prog.ID}, SearchQuery{
		Extra: []string{
			"i.type_id IN (SELECT id FROM issue_types WHERE level = 2 AND kind = 'work')",
			"(i.resolved_at IS NULL OR i.resolved_at >= now() - make_interval(days => $2::int))",
		},
		ExtraArgs: []any{doneDays}, DefaultOrder: []search.Order{{Field: "rank"}}, Limit: overviewInitiatives,
	})
	if err != nil {
		return out, err
	}
	out.Truncated = total > len(inits)
	initIDs := make([]uuid.UUID, len(inits))
	for i, in := range inits {
		initIDs[i] = in.ID
	}
	// Epics under the initiatives, in the programme itself or a team the actor can browse.
	var epics []dbq.Issue
	if len(initIDs) > 0 {
		var n int
		epics, n, err = s.runSearch(ctx, actor, teams.IDs(), SearchQuery{
			Extra:     []string{"i.parent_id = ANY($2::uuid[])", "i.type_id IN (SELECT id FROM issue_types WHERE level = 1)"},
			ExtraArgs: []any{initIDs}, DefaultOrder: []search.Order{{Field: "rank"}}, Limit: overviewEpics,
		})
		if err != nil {
			return out, err
		}
		out.Truncated = out.Truncated || n > len(epics)
	}
	epicIDs := make([]uuid.UUID, len(epics))
	for i, e := range epics {
		epicIDs[i] = e.ID
	}
	progress, err := s.childProgress(ctx, epicIDs)
	if err != nil {
		return out, err
	}
	latest, err := s.Q.ChildLatestDue(ctx, epicIDs)
	if err != nil {
		return out, err
	}
	latestDue := map[uuid.UUID]time.Time{}
	for _, r := range latest {
		latestDue[r.ParentID] = r.LatestDue
	}
	blocked, err := s.crossProjectBlockers(ctx, actor, epicIDs)
	if err != nil {
		return out, err
	}

	views, err := s.IssueViews(ctx, actor, append(append([]dbq.Issue{}, inits...), epics...))
	if err != nil {
		return out, err
	}
	viewByID := make(map[uuid.UUID]api.Issue, len(views))
	for _, v := range views {
		viewByID[v.Id] = v
	}
	today := s.Now().UTC().Truncate(24 * time.Hour)
	epicsOf := map[uuid.UUID][]api.ProgrammeEpic{}
	for _, e := range epics {
		pe := api.ProgrammeEpic{Issue: viewByID[e.ID], Progress: progress[e.ID], Flags: []api.PlanningFlag{}}
		forecast := laterDate(e.DueDate, latestDue[e.ID])
		pe.ForecastDate = dateView(forecast)
		done := pe.Issue.Status.Category == api.StatusCategoryDone
		if !done {
			if e.DueDate != nil && e.DueDate.Before(today) {
				pe.Flags = append(pe.Flags, flag(api.PlanningFlagKindOverdue, fmt.Sprintf("Due %s and not done", e.DueDate.Format("Jan 2"))))
			}
			if forecast == nil && e.StartDate == nil {
				pe.Flags = append(pe.Flags, flag(api.PlanningFlagKindUnscheduled, "No dates, and none of its issues has a due date"))
			}
			if b, ok := blocked[e.ID]; ok {
				pe.Flags = append(pe.Flags, b)
			}
		}
		parent := *e.ParentID
		if in := findIssue(inits, parent); in != nil && in.DueDate != nil && forecast != nil && forecast.After(*in.DueDate) && !done {
			pe.Flags = append(pe.Flags, flag(api.PlanningFlagKindLate, fmt.Sprintf("Forecast %s, after the initiative's due date (%s)", forecast.Format("Jan 2"), in.DueDate.Format("Jan 2"))))
		}
		epicsOf[parent] = append(epicsOf[parent], pe)
	}

	for _, in := range inits {
		pi := api.ProgrammeInitiative{Issue: viewByID[in.ID], Epics: epicsOf[in.ID], Flags: []api.PlanningFlag{}}
		if pi.Epics == nil {
			pi.Epics = []api.ProgrammeEpic{}
		}
		var forecast *time.Time
		late, blockedEpics := 0, 0
		for _, pe := range pi.Epics {
			addProgress(&pi.Progress, pe.Progress)
			if pe.ForecastDate != nil {
				forecast = laterDate(forecast, pe.ForecastDate.Time)
			}
			for _, f := range pe.Flags {
				switch f.Kind {
				case api.PlanningFlagKindLate:
					late++
				case api.PlanningFlagKindBlocked:
					blockedEpics++
				}
			}
		}
		pi.ForecastDate = dateView(forecast)
		if pi.Issue.Status.Category != api.StatusCategoryDone {
			if in.DueDate != nil && in.DueDate.Before(today) {
				pi.Flags = append(pi.Flags, flag(api.PlanningFlagKindOverdue, fmt.Sprintf("Due %s and not done", in.DueDate.Format("Jan 2"))))
			}
			if late > 0 {
				pi.Flags = append(pi.Flags, flag(api.PlanningFlagKindLate, fmt.Sprintf("%d %s forecast after its due date", late, plural(late, "epic", "epics"))))
			}
			if blockedEpics > 0 {
				pi.Flags = append(pi.Flags, flag(api.PlanningFlagKindBlocked, fmt.Sprintf("%d %s blocked by unfinished work in another project", blockedEpics, plural(blockedEpics, "epic", "epics"))))
			}
			if forecast == nil && in.StartDate == nil && in.DueDate == nil {
				pi.Flags = append(pi.Flags, flag(api.PlanningFlagKindUnscheduled, "No dates, and none of its epics has a forecast"))
			}
		}
		out.Initiatives = append(out.Initiatives, pi)
	}

	// Open epics in the teams that belong to no initiative.
	if len(teamIDs) > 0 {
		unlinked, n, err := s.runSearch(ctx, actor, teamIDs, SearchQuery{
			Extra: []string{
				"i.type_id IN (SELECT id FROM issue_types WHERE level = 1 AND kind = 'work')", "i.parent_id IS NULL",
				"i.status_id IN (SELECT id FROM statuses WHERE category <> 'done')",
			},
			DefaultOrder: []search.Order{{Field: "rank"}}, Limit: overviewUnlinked,
		})
		if err != nil {
			return out, err
		}
		if out.UnlinkedEpics, err = s.IssueViews(ctx, actor, unlinked); err != nil {
			return out, err
		}
		out.UnlinkedTotal = n
	}
	return out, nil
}

// childProgress counts each issue's children by status category.
func (s *Service) childProgress(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]api.IssueProgress, error) {
	out := map[uuid.UUID]api.IssueProgress{}
	rows, err := s.Q.EpicProgress(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ParentID == nil {
			continue
		}
		p := out[*r.ParentID]
		countProgress(&p, r.Category, int(r.N), r.Points)
		out[*r.ParentID] = p
	}
	return out, nil
}

// initiativeProgress is the work under an initiative's epics in projects actor can browse.
func (s *Service) initiativeProgress(ctx context.Context, actor *authz.Actor, id uuid.UUID) (api.IssueProgress, error) {
	var out api.IssueProgress
	visible, err := s.BrowsableProjectIDs(ctx, actor)
	if err != nil {
		return out, err
	}
	rows, err := s.Q.InitiativeProgress(ctx, dbq.InitiativeProgressParams{Ids: []uuid.UUID{id}, ProjectIds: visible})
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		countProgress(&out, r.Category, int(r.N), r.Points)
	}
	return out, nil
}

func countProgress(p *api.IssueProgress, category string, n int, points float64) {
	p.Total += n
	p.Points += points
	switch category {
	case "done":
		p.Done += n
		p.DonePoints += points
	case "in_progress":
		p.InProgress += n
	}
}

func addProgress(p *api.IssueProgress, q api.IssueProgress) {
	p.Total += q.Total
	p.Done += q.Done
	p.InProgress += q.InProgress
	p.Points += q.Points
	p.DonePoints += q.DonePoints
}

// crossProjectBlockers flags epics blocked (directly or through an issue under them) by unfinished
// work in another project. Blockers actor can't browse are counted, not named.
func (s *Service) crossProjectBlockers(ctx context.Context, actor *authz.Actor, epicIDs []uuid.UUID) (map[uuid.UUID]api.PlanningFlag, error) {
	out := map[uuid.UUID]api.PlanningFlag{}
	if len(epicIDs) == 0 {
		return out, nil
	}
	rows, err := s.Q.CrossProjectBlockers(ctx, epicIDs)
	if err != nil {
		return nil, err
	}
	var blockerIDs []uuid.UUID
	for _, r := range rows {
		blockerIDs = append(blockerIDs, r.BlockerID)
	}
	blockers, err := s.Q.GetIssuesByIDs(ctx, blockerIDs)
	if err != nil {
		return nil, err
	}
	visible, err := s.browsableIssues(ctx, actor, uuid.Nil, blockers)
	if err != nil {
		return nil, err
	}
	keys := map[uuid.UUID]string{}
	projects := map[uuid.UUID]dbq.Project{}
	for _, b := range visible {
		p, ok := projects[b.ProjectID]
		if !ok {
			if p, err = s.Q.GetProject(ctx, b.ProjectID); err != nil {
				return nil, err
			}
			projects[b.ProjectID] = p
		}
		keys[b.ID] = issueKey(p, b)
	}
	seen := map[[2]uuid.UUID]bool{}
	for _, r := range rows {
		if seen[[2]uuid.UUID{r.EpicID, r.BlockerID}] {
			continue
		}
		seen[[2]uuid.UUID{r.EpicID, r.BlockerID}] = true
		f, ok := out[r.EpicID]
		if !ok {
			f = api.PlanningFlag{Kind: api.PlanningFlagKindBlocked, IssueKeys: []string{}}
		}
		if k, ok := keys[r.BlockerID]; ok {
			f.IssueKeys = append(f.IssueKeys, k)
		} else {
			f.HiddenCount++
		}
		out[r.EpicID] = f
	}
	for id, f := range out {
		n := len(f.IssueKeys) + f.HiddenCount
		names := strings.Join(f.IssueKeys, ", ")
		switch {
		case f.HiddenCount == 0:
			f.Message = "Blocked by unfinished work in another project: " + names
		case len(f.IssueKeys) == 0:
			f.Message = fmt.Sprintf("Blocked by %d unfinished %s in a project you can't see", n, plural(n, "issue", "issues"))
		default:
			f.Message = fmt.Sprintf("Blocked by unfinished work in another project: %s, and %d you can't see", names, f.HiddenCount)
		}
		out[id] = f
	}
	return out, nil
}

func flag(kind api.PlanningFlagKind, msg string) api.PlanningFlag {
	return api.PlanningFlag{Kind: kind, Message: msg, IssueKeys: []string{}}
}

func laterDate(a *time.Time, b time.Time) *time.Time {
	if b.IsZero() {
		return a
	}
	if a == nil || b.After(*a) {
		return &b
	}
	return a
}

func findIssue(list []dbq.Issue, id uuid.UUID) *dbq.Issue {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}
