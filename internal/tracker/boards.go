package tracker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/search"
)

type BoardColumnInput struct {
	Name      string
	StatusIDs []uuid.UUID
	WipLimit  *int
}

type BoardInput struct {
	Name              string
	Type              string
	FilterJQL         string
	Swimlanes         string
	DoneRetentionDays int
	// Columns nil = derive one column per project status.
	Columns []BoardColumnInput
}

func (s *Service) deriveColumns(ctx context.Context, q *dbq.Queries, projectID uuid.UUID) ([]BoardColumnInput, error) {
	sts, err := s.projectStatuses(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	cols := make([]BoardColumnInput, len(sts))
	for i, st := range sts {
		cols[i] = BoardColumnInput{Name: st.Name, StatusIDs: []uuid.UUID{st.ID}}
	}
	return cols, nil
}

func writeColumns(ctx context.Context, q *dbq.Queries, boardID uuid.UUID, cols []BoardColumnInput) error {
	if err := q.DeleteBoardColumns(ctx, boardID); err != nil {
		return err
	}
	for i, c := range cols {
		var wip *int32
		if c.WipLimit != nil && *c.WipLimit > 0 {
			w := int32(*c.WipLimit)
			wip = &w
		}
		ids := c.StatusIDs
		if ids == nil {
			ids = []uuid.UUID{}
		}
		if _, err := q.CreateBoardColumn(ctx, dbq.CreateBoardColumnParams{BoardID: boardID, Name: c.Name, Position: int32(i), StatusIds: ids, WipLimit: wip}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validateBoard(ctx context.Context, actor *authz.Actor, in *BoardInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return apperr.Invalid("name", "name is required")
	}
	if in.Type != "scrum" && in.Type != "kanban" {
		return apperr.Invalid("type", "type must be scrum or kanban")
	}
	if in.Swimlanes == "" {
		in.Swimlanes = "none"
	}
	if !slices.Contains([]string{"none", "assignee", "epic", "priority", "type"}, in.Swimlanes) {
		return apperr.Invalid("swimlanes", "unknown swimlane grouping")
	}
	if in.DoneRetentionDays <= 0 {
		in.DoneRetentionDays = 14
	}
	seen := map[uuid.UUID]string{}
	for _, c := range in.Columns {
		if strings.TrimSpace(c.Name) == "" {
			return apperr.Invalid("columns", "every column needs a name")
		}
		for _, id := range c.StatusIDs {
			if other, ok := seen[id]; ok {
				return apperr.Invalid("columns", "a status can only be in one column (it is in %q and %q)", other, c.Name)
			}
			seen[id] = c.Name
		}
	}
	if in.FilterJQL != "" {
		return s.ValidateJQL(ctx, actor, in.FilterJQL)
	}
	return nil
}

func (s *Service) ListBoards(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.Board, dbq.Project, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, p, err
	}
	bs, err := s.Q.ListBoards(ctx, p.ID)
	return bs, p, err
}

func (s *Service) CreateBoard(ctx context.Context, actor *authz.Actor, ref string, in BoardInput) (dbq.Board, dbq.Project, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.BoardManage)
	if err != nil {
		return dbq.Board{}, p, err
	}
	if err := s.validateBoard(ctx, actor, &in); err != nil {
		return dbq.Board{}, p, err
	}
	var b dbq.Board
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		existing, err := q.ListBoards(ctx, p.ID)
		if err != nil {
			return err
		}
		b, err = q.CreateBoard(ctx, dbq.CreateBoardParams{
			ProjectID: p.ID, Name: in.Name, Type: in.Type, FilterJql: in.FilterJQL, Swimlanes: in.Swimlanes,
			DoneRetentionDays: int32(in.DoneRetentionDays), Position: int32(len(existing)),
		})
		if err != nil {
			return err
		}
		cols := in.Columns
		if cols == nil {
			if cols, err = s.deriveColumns(ctx, q, p.ID); err != nil {
				return err
			}
		}
		return writeColumns(ctx, q, b.ID, cols)
	})
	return b, p, err
}

func (s *Service) boardFor(ctx context.Context, actor *authz.Actor, id uuid.UUID, perm authz.Permission) (dbq.Board, dbq.Project, error) {
	b, err := s.Q.GetBoard(ctx, id)
	if err != nil {
		return b, dbq.Project{}, apperr.NotFound("board")
	}
	p, err := s.Q.GetProject(ctx, b.ProjectID)
	if err != nil {
		return b, p, err
	}
	if _, err := s.requireAccess(ctx, actor, p, perm); err != nil {
		if apperr.IsNotFound(err) {
			return b, p, apperr.NotFound("board")
		}
		return b, p, err
	}
	return b, p, nil
}

func (s *Service) GetBoard(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Board, dbq.Project, []dbq.BoardColumn, error) {
	b, p, err := s.boardFor(ctx, actor, id, authz.ProjectBrowse)
	if err != nil {
		return b, p, nil, err
	}
	cols, err := s.Q.BoardColumns(ctx, b.ID)
	return b, p, cols, err
}

func (s *Service) UpdateBoard(ctx context.Context, actor *authz.Actor, id uuid.UUID, in BoardInput) (dbq.Board, error) {
	b, p, err := s.boardFor(ctx, actor, id, authz.BoardManage)
	if err != nil {
		return b, err
	}
	if err := s.validateBoard(ctx, actor, &in); err != nil {
		return b, err
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		b, err = q.UpdateBoard(ctx, dbq.UpdateBoardParams{
			ID: id, Name: in.Name, Type: in.Type, FilterJql: in.FilterJQL, Swimlanes: in.Swimlanes, DoneRetentionDays: int32(in.DoneRetentionDays),
		})
		if err != nil {
			return err
		}
		if in.Columns != nil {
			if err := writeColumns(ctx, q, id, in.Columns); err != nil {
				return err
			}
		}
		return s.emit(ctx, tx, actor, p, nil, "board.updated", map[string]any{"boardId": id})
	})
	return b, err
}

func (s *Service) DeleteBoard(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	if _, _, err := s.boardFor(ctx, actor, id, authz.BoardManage); err != nil {
		return err
	}
	return s.Q.DeleteBoard(ctx, id)
}

// BoardColumnData is one column with its issues.
type BoardColumnData struct {
	Column    dbq.BoardColumn
	Issues    []dbq.Issue
	OverLimit bool
}

type BoardData struct {
	Board     dbq.Board
	Project   dbq.Project
	Columns   []BoardColumnData
	AllCols   []dbq.BoardColumn
	Sprint    *dbq.Sprint
	Unmapped  []uuid.UUID
}

const boardIssueLimit = 2000

// GetBoardData returns the board's columns with issues. Scrum boards show the active sprint;
// Kanban boards hide done issues resolved before the retention window. Epics are not cards.
func (s *Service) GetBoardData(ctx context.Context, actor *authz.Actor, id uuid.UUID, quickJQL string) (BoardData, error) {
	b, p, cols, err := s.GetBoard(ctx, actor, id)
	if err != nil {
		return BoardData{}, err
	}
	data := BoardData{Board: b, Project: p, AllCols: cols}
	extra := []string{"(SELECT level FROM issue_types t WHERE t.id = i.type_id) <= 0"}
	var extraArgs []any
	if b.Type == "scrum" {
		sp, err := s.Q.ActiveSprint(ctx, p.ID)
		if err != nil {
			// No active sprint: an empty board that points people at the backlog.
			for _, c := range cols {
				data.Columns = append(data.Columns, BoardColumnData{Column: c, Issues: []dbq.Issue{}})
			}
			return data, nil
		}
		data.Sprint = &sp
		extraArgs = append(extraArgs, sp.ID)
		extra = append(extra, "i.sprint_id = $2::uuid")
	} else {
		cutoff := s.Now().Add(-time.Duration(b.DoneRetentionDays) * 24 * time.Hour)
		extraArgs = append(extraArgs, cutoff)
		extra = append(extra, "(i.resolved_at IS NULL OR i.resolved_at >= $2::timestamptz)")
	}
	jql := combineJQL(b.FilterJql, quickJQL)
	issues, _, err := s.runSearch(ctx, actor, []uuid.UUID{p.ID}, SearchQuery{
		JQL: jql, Extra: extra, ExtraArgs: extraArgs, DefaultOrder: []search.Order{{Field: "rank"}}, Limit: boardIssueLimit,
	})
	if err != nil {
		return data, err
	}
	byStatus := map[uuid.UUID]int{}
	for idx, c := range cols {
		for _, st := range c.StatusIds {
			byStatus[st] = idx
		}
		data.Columns = append(data.Columns, BoardColumnData{Column: c, Issues: []dbq.Issue{}})
	}
	for _, i := range issues {
		if idx, ok := byStatus[i.StatusID]; ok {
			data.Columns[idx].Issues = append(data.Columns[idx].Issues, i)
		}
	}
	for idx := range data.Columns {
		c := data.Columns[idx]
		data.Columns[idx].OverLimit = c.Column.WipLimit != nil && len(c.Issues) > int(*c.Column.WipLimit)
	}
	sts, err := s.projectStatuses(ctx, s.Q, p.ID)
	if err != nil {
		return data, err
	}
	for _, st := range sts {
		if _, ok := byStatus[st.ID]; !ok {
			data.Unmapped = append(data.Unmapped, st.ID)
		}
	}
	return data, nil
}

// combineJQL ANDs board and quick filters, keeping a trailing ORDER BY from the board filter.
func combineJQL(parts ...string) string {
	var conds []string
	order := ""
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		upper := strings.ToUpper(p)
		if idx := strings.LastIndex(upper, "ORDER BY"); idx >= 0 {
			if order == "" {
				order = p[idx:]
			}
			p = strings.TrimSpace(p[:idx])
		}
		if p != "" {
			conds = append(conds, "("+p+")")
		}
	}
	return strings.TrimSpace(strings.Join(conds, " AND ") + " " + order)
}

type BacklogSprint struct {
	Sprint dbq.Sprint
	Issues []dbq.Issue
}

type Backlog struct {
	Sprints []BacklogSprint
	Backlog []dbq.Issue
}

// GetBacklog returns active and future sprints with their issues, then unscheduled work.
func (s *Service) GetBacklog(ctx context.Context, actor *authz.Actor, id uuid.UUID, quickJQL string) (Backlog, error) {
	b, p, err := s.boardFor(ctx, actor, id, authz.ProjectBrowse)
	if err != nil {
		return Backlog{}, err
	}
	out := Backlog{Sprints: []BacklogSprint{}}
	jql := combineJQL(b.FilterJql, quickJQL)
	levelCond := "(SELECT level FROM issue_types t WHERE t.id = i.type_id) = 0"
	notDone := "i.status_id IN (SELECT id FROM statuses WHERE category <> 'done')"
	if b.Type == "scrum" {
		sprints, err := s.Q.ListSprints(ctx, dbq.ListSprintsParams{ProjectID: p.ID})
		if err != nil {
			return out, err
		}
		for _, sp := range sprints {
			if sp.State == "closed" {
				continue
			}
			issues, _, err := s.runSearch(ctx, actor, []uuid.UUID{p.ID}, SearchQuery{
				JQL: jql, Extra: []string{"i.sprint_id = $2::uuid", levelCond}, ExtraArgs: []any{sp.ID},
				DefaultOrder: []search.Order{{Field: "rank"}}, Limit: boardIssueLimit,
			})
			if err != nil {
				return out, err
			}
			out.Sprints = append(out.Sprints, BacklogSprint{Sprint: sp, Issues: issues})
		}
	}
	backlog, _, err := s.runSearch(ctx, actor, []uuid.UUID{p.ID}, SearchQuery{
		JQL: jql, Extra: []string{"i.sprint_id IS NULL", levelCond, notDone},
		DefaultOrder: []search.Order{{Field: "rank"}}, Limit: boardIssueLimit,
	})
	out.Backlog = backlog
	return out, err
}

// ---------------------------------------------------------------------------
// Sprints

type SprintInput struct {
	Name      *string
	Goal      *string
	StartDate Opt[time.Time]
	EndDate   Opt[time.Time]
}

func (s *Service) ListSprints(ctx context.Context, actor *authz.Actor, ref, state string) ([]dbq.Sprint, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	return s.Q.ListSprints(ctx, dbq.ListSprintsParams{ProjectID: p.ID, State: state})
}

func (s *Service) CreateSprint(ctx context.Context, actor *authz.Actor, ref string, in SprintInput) (dbq.Sprint, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.SprintManage)
	if err != nil {
		return dbq.Sprint{}, err
	}
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		n, err := s.Q.CountSprints(ctx, p.ID)
		if err != nil {
			return dbq.Sprint{}, err
		}
		name = fmt.Sprintf("%s Sprint %d", p.Key, n+1)
	}
	goal := ""
	if in.Goal != nil {
		goal = *in.Goal
	}
	var sp dbq.Sprint
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		sp, err = q.CreateSprint(ctx, dbq.CreateSprintParams{ProjectID: p.ID, Name: name, Goal: goal, StartDate: in.StartDate.Value, EndDate: in.EndDate.Value})
		if err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, p, nil, "board.updated", nil)
	})
	return sp, err
}

func (s *Service) sprintForManage(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Sprint, dbq.Project, error) {
	sp, err := s.Q.GetSprint(ctx, id)
	if err != nil {
		return sp, dbq.Project{}, apperr.NotFound("sprint")
	}
	p, err := s.Q.GetProject(ctx, sp.ProjectID)
	if err != nil {
		return sp, p, err
	}
	if _, err := s.requireAccess(ctx, actor, p, authz.SprintManage); err != nil {
		if apperr.IsNotFound(err) {
			return sp, p, apperr.NotFound("sprint")
		}
		return sp, p, err
	}
	return sp, p, nil
}

func (s *Service) UpdateSprint(ctx context.Context, actor *authz.Actor, id uuid.UUID, in SprintInput) (dbq.Sprint, error) {
	sp, p, err := s.sprintForManage(ctx, actor, id)
	if err != nil {
		return sp, err
	}
	params := dbq.UpdateSprintParams{ID: id, Name: sp.Name, Goal: sp.Goal, StartDate: sp.StartDate, EndDate: sp.EndDate}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		params.Name = strings.TrimSpace(*in.Name)
	}
	if in.Goal != nil {
		params.Goal = *in.Goal
	}
	if in.StartDate.Set {
		params.StartDate = in.StartDate.Value
	}
	if in.EndDate.Set {
		params.EndDate = in.EndDate.Value
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		if sp, err = q.UpdateSprint(ctx, params); err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, p, nil, "board.updated", nil)
	})
	return sp, err
}

func (s *Service) DeleteSprint(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	sp, p, err := s.sprintForManage(ctx, actor, id)
	if err != nil {
		return err
	}
	if sp.State != "future" {
		return apperr.Conflict("sprint_not_future", "only sprints that have not started can be deleted")
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteSprint(ctx, id); err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, p, nil, "board.updated", nil)
	})
}

func (s *Service) StartSprint(ctx context.Context, actor *authz.Actor, id uuid.UUID, start, end time.Time, goal *string) (dbq.Sprint, error) {
	sp, p, err := s.sprintForManage(ctx, actor, id)
	if err != nil {
		return sp, err
	}
	if sp.State != "future" {
		return sp, apperr.Conflict("sprint_not_future", "the sprint has already started")
	}
	if !end.After(start) {
		return sp, apperr.Invalid("endDate", "the end date must be after the start date")
	}
	g := sp.Goal
	if goal != nil {
		g = *goal
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if _, err := q.ActiveSprint(ctx, p.ID); err == nil {
			return apperr.Conflict("sprint_active", "complete the active sprint before starting another")
		}
		var err error
		sp, err = q.StartSprint(ctx, dbq.StartSprintParams{ID: id, StartDate: &start, EndDate: &end, Goal: g})
		if err != nil {
			return err
		}
		Audit(ctx, q, actor, "sprint.start", "sprint", id.String(), &p.ID, map[string]any{"name": sp.Name})
		return s.emit(ctx, tx, actor, p, nil, "sprint.started", map[string]any{"sprintId": sp.ID, "name": sp.Name})
	})
	return sp, err
}

func (s *Service) CompleteSprint(ctx context.Context, actor *authz.Actor, id uuid.UUID, moveTo *uuid.UUID) (dbq.Sprint, error) {
	sp, p, err := s.sprintForManage(ctx, actor, id)
	if err != nil {
		return sp, err
	}
	if sp.State != "active" {
		return sp, apperr.Conflict("sprint_not_active", "only the active sprint can be completed")
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if moveTo != nil {
			target, err := q.GetSprint(ctx, *moveTo)
			if err != nil || target.ProjectID != p.ID || target.State != "future" {
				return apperr.Invalid("moveOpenIssuesTo", "unfinished issues can only move to a future sprint in %s", p.Key)
			}
		}
		var err error
		if sp, err = q.CompleteSprint(ctx, id); err != nil {
			return err
		}
		moved, err := q.MoveOpenSprintIssues(ctx, dbq.MoveOpenSprintIssuesParams{SprintID: &id, TargetSprintID: moveTo})
		if err != nil {
			return err
		}
		Audit(ctx, q, actor, "sprint.complete", "sprint", id.String(), &p.ID, map[string]any{"name": sp.Name, "movedIssues": moved})
		return s.emit(ctx, tx, actor, p, nil, "sprint.completed", map[string]any{"sprintId": sp.ID, "name": sp.Name, "movedIssues": moved})
	})
	return sp, err
}

type BurndownPoint struct {
	Date      time.Time
	Remaining float64
	Ideal     float64
}

type SprintReport struct {
	Sprint          dbq.Sprint
	CommittedPoints float64
	CompletedPoints float64
	CommittedIssues int
	CompletedIssues int
	Burndown        []BurndownPoint
}

// SprintReport derives commitment from the snapshot taken at start and burndown from history.
func (s *Service) SprintReport(ctx context.Context, actor *authz.Actor, id uuid.UUID) (SprintReport, error) {
	sp, err := s.Q.GetSprint(ctx, id)
	if err != nil {
		return SprintReport{}, apperr.NotFound("sprint")
	}
	p, err := s.Q.GetProject(ctx, sp.ProjectID)
	if err != nil {
		return SprintReport{}, err
	}
	if _, err := s.requireAccess(ctx, actor, p, authz.ProjectBrowse); err != nil {
		return SprintReport{}, apperr.NotFound("sprint")
	}
	rep := SprintReport{Sprint: sp, CommittedPoints: sp.CommittedPoints, CommittedIssues: len(sp.CommittedIssueIds)}
	if sp.State == "future" || sp.StartDate == nil {
		stats, err := s.Q.SprintIssueStats(ctx, &sp.ID)
		if err != nil {
			return rep, err
		}
		rep.CommittedPoints, rep.CommittedIssues = stats.Points, int(stats.Issues)
		return rep, nil
	}
	committed, err := s.Q.GetIssuesByIDs(ctx, sp.CommittedIssueIds)
	if err != nil {
		return rep, err
	}
	points := map[uuid.UUID]float64{}
	for _, i := range committed {
		if i.StoryPoints != nil {
			points[i.ID] = *i.StoryPoints
		}
	}
	doneAt, err := s.Q.SprintDoneEvents(ctx, sp.CommittedIssueIds)
	if err != nil {
		return rep, err
	}
	end := s.Now()
	if sp.EndDate != nil && sp.EndDate.Before(end) {
		end = *sp.EndDate
	}
	if sp.CompletedAt != nil {
		end = *sp.CompletedAt
	}
	start := *sp.StartDate
	completedSet := map[uuid.UUID]bool{}
	if sp.State == "closed" {
		for _, cid := range sp.CompletedIssueIds {
			completedSet[cid] = true
		}
	} else {
		for _, i := range committed {
			if st, err := statusInfo(ctx, s.Q, i.StatusID); err == nil && st.Category == "done" && i.SprintID != nil && *i.SprintID == sp.ID {
				completedSet[i.ID] = true
			}
		}
	}
	for cid := range completedSet {
		if slices.Contains(sp.CommittedIssueIds, cid) {
			rep.CompletedIssues++
			rep.CompletedPoints += points[cid]
		}
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	totalDays := days
	if sp.EndDate != nil {
		totalDays = max(int(sp.EndDate.Sub(start).Hours()/24)+1, 1)
	}
	for d := 0; d < days && d < 120; d++ {
		day := start.AddDate(0, 0, d)
		dayEnd := day.AddDate(0, 0, 1)
		remaining := rep.CommittedPoints
		for _, ev := range doneAt {
			if ev.DoneAt.Before(dayEnd) && completedSet[ev.IssueID] {
				remaining -= points[ev.IssueID]
			}
		}
		ideal := rep.CommittedPoints * (1 - float64(d)/float64(max(totalDays-1, 1)))
		rep.Burndown = append(rep.Burndown, BurndownPoint{Date: day, Remaining: remaining, Ideal: max(ideal, 0)})
	}
	return rep, nil
}

// ---------------------------------------------------------------------------
// Releases

type ReleaseInput struct {
	Name        *string
	Description *string
	StartDate   Opt[time.Time]
	ReleaseDate Opt[time.Time]
	State       *string
}

func (s *Service) ListReleases(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.ListReleasesRow, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	return s.Q.ListReleases(ctx, p.ID)
}

func (s *Service) CreateRelease(ctx context.Context, actor *authz.Actor, ref string, in ReleaseInput) (dbq.Release, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ReleaseManage)
	if err != nil {
		return dbq.Release{}, err
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return dbq.Release{}, apperr.Invalid("name", "name is required")
	}
	desc := ""
	if in.Description != nil {
		desc = *in.Description
	}
	r, err := s.Q.CreateRelease(ctx, dbq.CreateReleaseParams{ProjectID: p.ID, Name: strings.TrimSpace(*in.Name), Description: desc, StartDate: in.StartDate.Value, ReleaseDate: in.ReleaseDate.Value})
	if err != nil && isUnique(err) {
		return r, apperr.Conflict("duplicate_release", "a release named %q already exists", *in.Name)
	}
	return r, err
}

func (s *Service) releaseForManage(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Release, dbq.Project, error) {
	r, err := s.Q.GetRelease(ctx, id)
	if err != nil {
		return r, dbq.Project{}, apperr.NotFound("release")
	}
	p, err := s.Q.GetProject(ctx, r.ProjectID)
	if err != nil {
		return r, p, err
	}
	if _, err := s.requireAccess(ctx, actor, p, authz.ReleaseManage); err != nil {
		if apperr.IsNotFound(err) {
			return r, p, apperr.NotFound("release")
		}
		return r, p, err
	}
	return r, p, nil
}

func (s *Service) UpdateRelease(ctx context.Context, actor *authz.Actor, id uuid.UUID, in ReleaseInput) (dbq.Release, error) {
	r, _, err := s.releaseForManage(ctx, actor, id)
	if err != nil {
		return r, err
	}
	params := dbq.UpdateReleaseParams{ID: id, Name: r.Name, Description: r.Description, StartDate: r.StartDate, ReleaseDate: r.ReleaseDate, State: r.State}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		params.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		params.Description = *in.Description
	}
	if in.StartDate.Set {
		params.StartDate = in.StartDate.Value
	}
	if in.ReleaseDate.Set {
		params.ReleaseDate = in.ReleaseDate.Value
	}
	if in.State != nil {
		if !slices.Contains([]string{"unreleased", "released", "archived"}, *in.State) {
			return r, apperr.Invalid("state", "unknown state")
		}
		params.State = *in.State
	}
	out, err := s.Q.UpdateRelease(ctx, params)
	if err != nil && isUnique(err) {
		return out, apperr.Conflict("duplicate_release", "a release with that name already exists")
	}
	return out, err
}

func (s *Service) DeleteRelease(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	if _, _, err := s.releaseForManage(ctx, actor, id); err != nil {
		return err
	}
	return s.Q.DeleteRelease(ctx, id)
}

func (s *Service) ShipRelease(ctx context.Context, actor *authz.Actor, id uuid.UUID, moveTo *uuid.UUID) (dbq.Release, error) {
	r, p, err := s.releaseForManage(ctx, actor, id)
	if err != nil {
		return r, err
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if moveTo != nil {
			target, err := q.GetRelease(ctx, *moveTo)
			if err != nil || target.ProjectID != p.ID || target.ID == id {
				return apperr.Invalid("moveOpenIssuesTo", "release not found in %s", p.Key)
			}
		}
		if err := q.MoveOpenReleaseIssues(ctx, dbq.MoveOpenReleaseIssuesParams{ReleaseID: id, TargetReleaseID: moveTo}); err != nil {
			return err
		}
		var err error
		r, err = q.UpdateRelease(ctx, dbq.UpdateReleaseParams{ID: id, Name: r.Name, Description: r.Description, StartDate: r.StartDate, ReleaseDate: r.ReleaseDate, State: "released"})
		if err != nil {
			return err
		}
		Audit(ctx, q, actor, "release.ship", "release", id.String(), &p.ID, map[string]any{"name": r.Name})
		return s.emit(ctx, tx, actor, p, nil, "release.released", map[string]any{"releaseId": r.ID, "name": r.Name})
	})
	return r, err
}
