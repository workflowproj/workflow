package tracker

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/search"
)

// jqlError converts a search error into a 400 carrying the character position.
func jqlError(err error) error {
	if se, ok := err.(*search.Error); ok {
		pos := se.Pos
		e := apperr.New(http.StatusBadRequest, "invalid_jql", "%s", se.Msg)
		e.Position = &pos
		return e
	}
	return err
}

func (s *Service) searchEnv(ctx context.Context, actor *authz.Actor) (search.Env, error) {
	defs, err := s.Q.ListFieldDefinitions(ctx)
	if err != nil {
		return search.Env{}, err
	}
	env := search.Env{UserID: actor.UserID(), Now: s.Now()}
	for _, d := range defs {
		env.CustomFields = append(env.CustomFields, search.CustomField{Key: d.Key, Name: d.Name, Type: d.Type})
	}
	return env, nil
}

// SearchQuery is a compiled search with extra SQL constraints (used by boards).
type SearchQuery struct {
	JQL          string
	Extra        []string // additional SQL conditions over alias i, may use $n placeholders after Args
	ExtraArgs    []any
	DefaultOrder []search.Order
	Limit        int
	Offset       int
}

// Search runs JQL over the projects the actor can browse.
func (s *Service) Search(ctx context.Context, actor *authz.Actor, jql string, limit, offset int) ([]dbq.Issue, int, error) {
	ids, err := s.BrowsableProjectIDs(ctx, actor)
	if err != nil {
		return nil, 0, err
	}
	return s.runSearch(ctx, actor, ids, SearchQuery{
		JQL: jql, DefaultOrder: []search.Order{{Field: "created", Desc: true}}, Limit: limit, Offset: offset,
	})
}

// SearchIn runs JQL over the given projects (callers decide which projects are visible).
func (s *Service) SearchIn(ctx context.Context, actor *authz.Actor, projectIDs []uuid.UUID, jql string, limit int) ([]dbq.Issue, int, error) {
	return s.runSearch(ctx, actor, projectIDs, SearchQuery{JQL: jql, DefaultOrder: []search.Order{{Field: "updated", Desc: true}}, Limit: limit})
}

// IssueMatches reports whether an issue satisfies a JQL query (chat channel filters). It runs
// without a user, so currentUser() matches nobody.
func (s *Service) IssueMatches(ctx context.Context, projectID, issueID uuid.UUID, jql string) (bool, error) {
	issues, _, err := s.runSearch(ctx, authz.SystemActor(), []uuid.UUID{projectID}, SearchQuery{
		JQL: jql, Extra: []string{"i.id = $2::uuid"}, ExtraArgs: []any{issueID}, Limit: 1,
	})
	return len(issues) == 1, err
}

// ValidateJQL checks a query compiles (used when saving filters and boards).
func (s *Service) ValidateJQL(ctx context.Context, actor *authz.Actor, jql string) error {
	env, err := s.searchEnv(ctx, actor)
	if err != nil {
		return err
	}
	_, err = search.CompileString(jql, env, 1, nil)
	return jqlError(err)
}

func (s *Service) runSearch(ctx context.Context, actor *authz.Actor, projectIDs []uuid.UUID, sq SearchQuery) ([]dbq.Issue, int, error) {
	if len(projectIDs) == 0 {
		return []dbq.Issue{}, 0, nil
	}
	env, err := s.searchEnv(ctx, actor)
	if err != nil {
		return nil, 0, err
	}
	args := []any{projectIDs}
	args = append(args, sq.ExtraArgs...)
	compiled, err := search.CompileString(sq.JQL, env, len(args), sq.DefaultOrder)
	if err != nil {
		return nil, 0, jqlError(err)
	}
	args = append(args, compiled.Args...)
	where := "i.project_id = ANY($1::uuid[]) AND " + compiled.Where
	for _, e := range sq.Extra {
		where += " AND " + e
	}
	if sq.Limit <= 0 {
		sq.Limit = 50
	}
	var total int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM issues i WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("search count: %w", err)
	}
	rows, err := s.Pool.Query(ctx, fmt.Sprintf("SELECT i.id FROM issues i WHERE %s ORDER BY %s LIMIT %d OFFSET %d", where, compiled.OrderBy, sq.Limit, sq.Offset), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, 0, err
	}
	issues, err := s.Q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[uuid.UUID]dbq.Issue, len(issues))
	for _, i := range issues {
		byID[i.ID] = i
	}
	ordered := make([]dbq.Issue, 0, len(ids))
	for _, id := range ids {
		if i, ok := byID[id]; ok {
			ordered = append(ordered, i)
		}
	}
	return ordered, total, nil
}

// ---------------------------------------------------------------------------
// Saved filters

func (s *Service) CreateFilter(ctx context.Context, actor *authz.Actor, name, desc, jql string, shared bool) (dbq.SavedFilter, error) {
	if actor.User == nil {
		return dbq.SavedFilter{}, apperr.Forbidden("only people can save filters")
	}
	if err := s.ValidateJQL(ctx, actor, jql); err != nil {
		return dbq.SavedFilter{}, err
	}
	return s.Q.CreateFilter(ctx, dbq.CreateFilterParams{OwnerID: actor.User.ID, Name: name, Description: desc, Jql: jql, Shared: shared})
}

func (s *Service) UpdateFilter(ctx context.Context, actor *authz.Actor, id uuid.UUID, name, desc, jql string, shared bool) (dbq.SavedFilter, error) {
	f, err := s.Q.GetFilter(ctx, id)
	if err != nil || actor.User == nil || (f.OwnerID != actor.User.ID && !f.Shared) {
		return f, apperr.NotFound("filter")
	}
	if f.OwnerID != actor.User.ID && !actor.IsOrgAdmin() {
		return f, apperr.Forbidden("only the owner can change this filter")
	}
	if err := s.ValidateJQL(ctx, actor, jql); err != nil {
		return f, err
	}
	return s.Q.UpdateFilter(ctx, dbq.UpdateFilterParams{ID: id, Name: name, Description: desc, Jql: jql, Shared: shared})
}

func (s *Service) DeleteFilter(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	f, err := s.Q.GetFilter(ctx, id)
	if err != nil || actor.User == nil || (f.OwnerID != actor.User.ID && !f.Shared) {
		return apperr.NotFound("filter")
	}
	if f.OwnerID != actor.User.ID && !actor.IsOrgAdmin() {
		return apperr.Forbidden("only the owner can delete this filter")
	}
	return s.Q.DeleteFilter(ctx, id)
}
