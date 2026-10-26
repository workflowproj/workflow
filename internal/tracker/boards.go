package tracker

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/search"
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
	// CardFields nil = keep the current fields (the defaults for a new board).
	CardFields []string
}

// CardFields are what board cards can show besides the summary, type and key.
var CardFields = []string{"epic", "labels", "priority", "dev", "points", "assignee", "dueDate", "daysInColumn", "timeTracking"}

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
	for k, c := range in.Columns {
		if strings.TrimSpace(c.Name) == "" {
			return apperr.Invalid(fmt.Sprintf("columns[%d].name", k), "every column needs a name")
		}
		for _, id := range c.StatusIDs {
			if other, ok := seen[id]; ok {
				return apperr.Invalid(fmt.Sprintf("columns[%d].statusIds", k), "a status can only be in one column (it is in %q and %q)", other, c.Name)
			}
			seen[id] = c.Name
		}
	}
	seenField := map[string]bool{}
	for _, f := range in.CardFields {
		if !slices.Contains(CardFields, f) {
			return apperr.Invalid("cardFields", "unknown card field %q", f)
		}
		if seenField[f] {
			return apperr.Invalid("cardFields", "%q is listed twice", f)
		}
		seenField[f] = true
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
		if in.CardFields != nil {
			if err := q.SetBoardCardFields(ctx, dbq.SetBoardCardFieldsParams{ID: b.ID, CardFields: in.CardFields}); err != nil {
				return err
			}
			b.CardFields = in.CardFields
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
		if in.CardFields != nil {
			if err := q.SetBoardCardFields(ctx, dbq.SetBoardCardFieldsParams{ID: id, CardFields: in.CardFields}); err != nil {
				return err
			}
			b.CardFields = in.CardFields
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
	Board    dbq.Board
	Project  dbq.Project
	Columns  []BoardColumnData
	AllCols  []dbq.BoardColumn
	Sprint   *dbq.Sprint
	Unmapped []uuid.UUID
	// Total is how many issues match; the columns hold at most boardIssueLimit of them.
	Total int
}

// boardIssueLimit caps the issues a board, or one backlog section, loads (in rank order).
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
	issues, total, err := s.runSearch(ctx, actor, []uuid.UUID{p.ID}, SearchQuery{
		JQL: jql, Extra: extra, ExtraArgs: extraArgs, DefaultOrder: []search.Order{{Field: "rank"}}, Limit: boardIssueLimit,
	})
	if err != nil {
		return data, err
	}
	data.Total = total
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
	Total  int // matching issues; Issues holds at most boardIssueLimit
}

type Backlog struct {
	Sprints      []BacklogSprint
	Backlog      []dbq.Issue
	BacklogTotal int
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
			issues, total, err := s.runSearch(ctx, actor, []uuid.UUID{p.ID}, SearchQuery{
				JQL: jql, Extra: []string{"i.sprint_id = $2::uuid", levelCond}, ExtraArgs: []any{sp.ID},
				DefaultOrder: []search.Order{{Field: "rank"}}, Limit: boardIssueLimit,
			})
			if err != nil {
				return out, err
			}
			out.Sprints = append(out.Sprints, BacklogSprint{Sprint: sp, Issues: issues, Total: total})
		}
	}
	backlog, total, err := s.runSearch(ctx, actor, []uuid.UUID{p.ID}, SearchQuery{
		JQL: jql, Extra: []string{"i.sprint_id IS NULL", levelCond, notDone},
		DefaultOrder: []search.Order{{Field: "rank"}}, Limit: boardIssueLimit,
	})
	out.Backlog, out.BacklogTotal = backlog, total
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
	Date                                     time.Time
	Remaining, Ideal, Scope, Completed       float64
	RemainingIssues, ScopeIssues, DoneIssues int
	// Hours, in seconds: remaining estimate of open issues, time logged in the sprint so far,
	// their sum (scope) and the guideline.
	RemainingSeconds, SpentSeconds, ScopeSeconds, IdealSeconds int64
}

type SprintReport struct {
	Sprint                     dbq.Sprint
	CommittedPoints            float64
	CompletedPoints            float64
	CommittedIssues            int
	CompletedIssues            int
	AddedPoints, RemovedPoints float64
	AddedIssues, RemovedIssues int
	Burndown                   []BurndownPoint
	// Hours, in seconds: remaining estimate at the start, time logged during the sprint, and
	// the remaining estimate of issues added or taken out (when they were).
	CommittedSeconds, SpentSeconds, AddedSeconds, RemovedSeconds int64
}

// SprintReport is the burndown and completion of a sprint the actor can see.
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
	return s.sprintReport(ctx, sp)
}

// sprintReport replays the sprint from its start: which issues were in it (the commitment
// snapshot, then sprint changes from history), their story points and whether they were done,
// at the end of each day. Scope added after the start raises the line; removed scope lowers it.
func (s *Service) sprintReport(ctx context.Context, sp dbq.Sprint) (SprintReport, error) {
	rep := SprintReport{Sprint: sp, CommittedPoints: sp.CommittedPoints, CommittedIssues: len(sp.CommittedIssueIds)}
	if sp.State == "future" || sp.StartDate == nil {
		stats, err := s.Q.SprintIssueStats(ctx, &sp.ID)
		if err != nil {
			return rep, err
		}
		rep.CommittedPoints, rep.CommittedIssues = stats.Points, int(stats.Issues)
		return rep, nil
	}
	ids, err := s.Q.IssuesEverInSprint(ctx, dbq.IssuesEverInSprintParams{SprintID: &sp.ID, Committed: sp.CommittedIssueIds})
	if err != nil {
		return rep, err
	}
	issues, err := s.Q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return rep, err
	}
	hist, err := s.historyByIssue(ctx, ids)
	if err != nil {
		return rep, err
	}
	worklogs, err := s.Q.WorklogsOfIssues(ctx, ids)
	if err != nil {
		return rep, err
	}
	statuses, err := s.statusMap(ctx)
	if err != nil {
		return rep, err
	}
	start := *sp.StartDate
	end := s.Now()
	if sp.CompletedAt != nil {
		end = *sp.CompletedAt
	}
	committed := map[uuid.UUID]bool{}
	for _, cid := range sp.CommittedIssueIds {
		committed[cid] = true
	}
	sprintRef := sp.ID.String()

	// Membership: committed issues start in the sprint; others start in it only when their
	// first sprint change after the start takes them out (they were created into it). Each
	// later change moves them in or out. Completing a sprint moves unfinished issues on
	// without history, so nothing after `end` is replayed.
	type member struct {
		issue    dbq.Issue
		from     time.Time
		initial  bool
		changes  []historyRow
		timeline []statusChange
		worklogs []dbq.WorklogsOfIssuesRow
	}
	members := make([]member, 0, len(issues))
	for _, i := range issues {
		m := member{issue: i, from: start, initial: committed[i.ID], timeline: statusTimeline(i.CreatedAt, i.StatusID, hist[i.ID])}
		for _, h := range hist[i.ID] {
			if h.Field == "sprint" && h.CreatedAt.After(start) && (deref(h.ToRef) == sprintRef || deref(h.FromRef) == sprintRef) {
				m.changes = append(m.changes, h)
			}
		}
		if !m.initial {
			if len(m.changes) > 0 {
				m.initial = deref(m.changes[0].FromRef) == sprintRef
			} else {
				m.initial = i.SprintID != nil && *i.SprintID == sp.ID
			}
			if i.CreatedAt.After(start) {
				m.from = i.CreatedAt
			}
		}
		for _, w := range worklogs {
			if w.IssueID == i.ID {
				m.worklogs = append(m.worklogs, w)
			}
		}
		members = append(members, m)
	}
	inAt := func(m member, t time.Time) bool {
		if t.Before(m.from) {
			return false
		}
		in := m.initial
		for _, h := range m.changes {
			if h.CreatedAt.After(t) {
				break
			}
			in = deref(h.ToRef) == sprintRef
		}
		return in
	}
	doneAt := func(m member, t time.Time) bool {
		id, ok := statusAt(m.timeline, t)
		return ok && statuses[id].Category == "done"
	}

	totalDays := 1
	if sp.EndDate != nil {
		totalDays = max(int(sp.EndDate.Sub(start).Hours()/24)+1, 1)
	}
	// Hours: the remaining estimate of the open committed issues when the sprint started, and
	// the time logged on the sprint's issues while they were in it. Work counts when it was
	// logged, the moment the remaining estimate moves with it, so logging work with the
	// estimate adjusted leaves the scope (logged + remaining) where it was.
	for _, m := range members {
		if committed[m.issue.ID] && !doneAt(m, start) {
			rep.CommittedSeconds += remainingAt(m.issue.RemainingEstimateSeconds, hist[m.issue.ID], start)
		}
	}
	spentUntil := func(t time.Time) int64 {
		var n int64
		for _, m := range members {
			for _, w := range m.worklogs {
				if !w.CreatedAt.Before(start) && w.CreatedAt.Before(t) && inAt(m, w.CreatedAt) {
					n += w.DurationSeconds
				}
			}
		}
		return n
	}
	for d := 0; d < 120; d++ {
		day := start.AddDate(0, 0, d)
		if day.After(end) {
			break
		}
		at := day.AddDate(0, 0, 1)
		if at.After(end) {
			at = end
		}
		pt := BurndownPoint{Date: day}
		for _, m := range members {
			if !inAt(m, at) {
				continue
			}
			pts := pointsAt(m.issue.StoryPoints, hist[m.issue.ID], at)
			pt.Scope += pts
			pt.ScopeIssues++
			if doneAt(m, at) {
				pt.Completed += pts
				pt.DoneIssues++
			} else {
				pt.RemainingSeconds += remainingAt(m.issue.RemainingEstimateSeconds, hist[m.issue.ID], at)
			}
		}
		pt.Remaining, pt.RemainingIssues = pt.Scope-pt.Completed, pt.ScopeIssues-pt.DoneIssues
		pt.Ideal = max(rep.CommittedPoints*(1-float64(d)/float64(max(totalDays-1, 1))), 0)
		pt.SpentSeconds = spentUntil(at)
		pt.ScopeSeconds = pt.SpentSeconds + pt.RemainingSeconds
		pt.IdealSeconds = max(int64(math.Round(float64(rep.CommittedSeconds)*(1-float64(d)/float64(max(totalDays-1, 1))))), 0)
		rep.Burndown = append(rep.Burndown, pt)
	}

	// Totals at the end: completion (the snapshot taken when a sprint is completed), and scope
	// that came and went after the start.
	completedSet := map[uuid.UUID]bool{}
	if sp.State == "closed" {
		for _, cid := range sp.CompletedIssueIds {
			completedSet[cid] = true
		}
	}
	rep.SpentSeconds = spentUntil(end.Add(time.Nanosecond))
	for _, m := range members {
		id := m.issue.ID
		pts := pointsAt(m.issue.StoryPoints, hist[id], end)
		inEnd := inAt(m, end)
		if sp.State != "closed" && inEnd && doneAt(m, end) {
			completedSet[id] = true
		}
		if completedSet[id] {
			rep.CompletedIssues++
			rep.CompletedPoints += pts
		}
		everIn := committed[id] || m.initial || slices.ContainsFunc(m.changes, func(h historyRow) bool { return deref(h.ToRef) == sprintRef })
		if !committed[id] && everIn && !m.from.After(end) {
			rep.AddedIssues++
			rep.AddedPoints += pts
			joined := m.from
			if !m.initial {
				if k := slices.IndexFunc(m.changes, func(h historyRow) bool { return deref(h.ToRef) == sprintRef }); k >= 0 {
					joined = m.changes[k].CreatedAt
				}
			}
			rep.AddedSeconds += remainingAt(m.issue.RemainingEstimateSeconds, hist[id], joined)
		}
		if everIn && !inEnd && !completedSet[id] {
			rep.RemovedIssues++
			rep.RemovedPoints += pts
			left := end
			for _, h := range m.changes {
				if deref(h.FromRef) == sprintRef && !h.CreatedAt.After(end) {
					left = h.CreatedAt
				}
			}
			rep.RemovedSeconds += remainingAt(m.issue.RemainingEstimateSeconds, hist[id], left)
		}
	}
	return rep, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type VelocitySprint struct {
	Sprint                           dbq.Sprint
	CommittedPoints, CompletedPoints float64
	CommittedIssues, CompletedIssues int
}

// Velocity is committed and completed work of a project's last `count` completed sprints,
// oldest first.
func (s *Service) Velocity(ctx context.Context, actor *authz.Actor, ref string, count int) ([]VelocitySprint, error) {
	if count < 1 || count > 20 {
		return nil, apperr.Invalid("count", "count must be between 1 and 20")
	}
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	sprints, err := s.Q.ClosedSprints(ctx, dbq.ClosedSprintsParams{ProjectID: p.ID, Limit: int32(count)})
	if err != nil {
		return nil, err
	}
	// Each sprint is replayed from its history; a few at a time.
	out := make([]VelocitySprint, len(sprints))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for k, sp := range sprints {
		g.Go(func() error {
			rep, err := s.sprintReport(gctx, sp)
			if err != nil {
				return err
			}
			out[len(sprints)-1-k] = VelocitySprint{Sprint: rep.Sprint, CommittedPoints: rep.CommittedPoints, CompletedPoints: rep.CompletedPoints,
				CommittedIssues: rep.CommittedIssues, CompletedIssues: rep.CompletedIssues}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Releases

type ReleaseInput struct {
	Name        *string
	Description *string
	StartDate   Opt[time.Time]
	ReleaseDate Opt[time.Time]
	State       *string
	// Notes and NotesPublished set the release notes and whether help centres show them.
	Notes          *string
	NotesPublished *bool
	// TagRepositoryID and TagName link the release to a Git tag; both null unlink it.
	TagRepositoryID Opt[uuid.UUID]
	TagName         Opt[string]
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
	var r dbq.Release
	err = s.tx(ctx, func(_ pgx.Tx, q *dbq.Queries) error {
		var err error
		r, err = q.CreateRelease(ctx, dbq.CreateReleaseParams{ProjectID: p.ID, Name: strings.TrimSpace(*in.Name), Description: desc, StartDate: in.StartDate.Value, ReleaseDate: in.ReleaseDate.Value})
		if err != nil {
			return err
		}
		r, err = s.setReleaseExtras(ctx, q, p, r, in)
		return err
	})
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
	r, p, err := s.releaseForManage(ctx, actor, id)
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
	var out dbq.Release
	err = s.tx(ctx, func(_ pgx.Tx, q *dbq.Queries) error {
		var err error
		if out, err = q.UpdateRelease(ctx, params); err != nil {
			return err
		}
		out, err = s.setReleaseExtras(ctx, q, p, out, in)
		return err
	})
	if err != nil && isUnique(err) {
		return out, apperr.Conflict("duplicate_release", "a release with that name already exists")
	}
	if err == nil && in.NotesPublished != nil && *in.NotesPublished != (r.NotesPublishedAt != nil) {
		Audit(ctx, s.Q, actor, "release.notes_publish", "release", id.String(), &p.ID, map[string]any{"name": out.Name, "published": *in.NotesPublished})
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
		if err := s.moveOpenReleaseIssues(ctx, q, actor, id, moveTo); err != nil {
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
