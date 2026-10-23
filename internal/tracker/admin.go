package tracker

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

func isUnique(err error) bool { return db.IsUniqueViolation(err) }

// ---------------------------------------------------------------------------
// Custom field definitions (org admins)

type FieldInput struct {
	Key, Name, Description, Type string
	Options                      []string
	ProjectKeys, IssueTypeKeys   []string
}

var fieldTypes = []string{"text", "number", "select", "multiselect", "user", "date", "version", "checkbox", "url"}

func (s *Service) fieldParams(ctx context.Context, in FieldInput) (dbq.CreateFieldDefinitionParams, error) {
	var params dbq.CreateFieldDefinitionParams
	if !slices.Contains(fieldTypes, in.Type) {
		return params, apperr.Invalid("type", "unknown field type %q", in.Type)
	}
	if (in.Type == "select" || in.Type == "multiselect") && len(in.Options) == 0 {
		return params, apperr.Invalid("options", "select fields need at least one option")
	}
	opts := []string{}
	for _, o := range in.Options {
		if o = strings.TrimSpace(o); o != "" && !slices.Contains(opts, o) {
			opts = append(opts, o)
		}
	}
	optJSON, _ := json.Marshal(opts)
	var projectIDs []uuid.UUID
	for _, k := range in.ProjectKeys {
		p, err := s.ResolveProject(ctx, s.Q, k)
		if err != nil {
			return params, apperr.Invalid("projectKeys", "unknown project %s", k)
		}
		projectIDs = append(projectIDs, p.ID)
	}
	return dbq.CreateFieldDefinitionParams{
		Key: in.Key, Name: strings.TrimSpace(in.Name), Description: in.Description, Type: in.Type, Options: optJSON,
		ProjectIds: projectIDs, IssueTypeKeys: nilIfEmpty(in.IssueTypeKeys),
	}, nil
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func (s *Service) CreateField(ctx context.Context, actor *authz.Actor, in FieldInput) (dbq.FieldDefinition, error) {
	if !actor.IsOrgAdmin() {
		return dbq.FieldDefinition{}, apperr.Forbidden("only organisation admins can define custom fields")
	}
	params, err := s.fieldParams(ctx, in)
	if err != nil {
		return dbq.FieldDefinition{}, err
	}
	f, err := s.Q.CreateFieldDefinition(ctx, params)
	if isUnique(err) {
		return f, apperr.Conflict("duplicate_field", "a field with key %q already exists", in.Key)
	}
	if err == nil {
		Audit(ctx, s.Q, actor, "field.create", "field", f.ID.String(), nil, map[string]any{"key": f.Key})
	}
	return f, err
}

func (s *Service) UpdateField(ctx context.Context, actor *authz.Actor, id uuid.UUID, in FieldInput) (dbq.FieldDefinition, error) {
	if !actor.IsOrgAdmin() {
		return dbq.FieldDefinition{}, apperr.Forbidden("only organisation admins can change custom fields")
	}
	cur, err := s.Q.GetFieldDefinition(ctx, id)
	if err != nil {
		return cur, apperr.NotFound("field")
	}
	if cur.Key != in.Key || cur.Type != in.Type {
		return cur, apperr.Invalid("key", "the key and type of a field cannot change once created")
	}
	params, err := s.fieldParams(ctx, in)
	if err != nil {
		return cur, err
	}
	return s.Q.UpdateFieldDefinition(ctx, dbq.UpdateFieldDefinitionParams{
		ID: id, Key: params.Key, Name: params.Name, Description: params.Description, Type: params.Type,
		Options: params.Options, ProjectIds: params.ProjectIds, IssueTypeKeys: params.IssueTypeKeys,
	})
}

func (s *Service) DeleteField(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	if !actor.IsOrgAdmin() {
		return apperr.Forbidden("only organisation admins can delete custom fields")
	}
	f, err := s.Q.GetFieldDefinition(ctx, id)
	if err != nil {
		return apperr.NotFound("field")
	}
	// Values stay in issues.fields; they simply stop being shown or searchable.
	if err := s.Q.DeleteFieldDefinition(ctx, id); err != nil {
		return err
	}
	Audit(ctx, s.Q, actor, "field.delete", "field", id.String(), nil, map[string]any{"key": f.Key})
	return nil
}

// ---------------------------------------------------------------------------
// Reports

type CycleTimeEntry struct {
	Key        string
	Summary    string
	Hours      float64
	ResolvedAt time.Time
}

type CycleTimeReport struct {
	Days                         int
	Average, Median, P85         float64
	Issues                       []CycleTimeEntry
}

// CycleTime measures, from issue history, the time from first entering an in-progress status
// to resolution, for issues resolved in the last `days` days.
func (s *Service) CycleTime(ctx context.Context, actor *authz.Actor, ref string, days int) (CycleTimeReport, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return CycleTimeReport{}, err
	}
	since := s.Now().AddDate(0, 0, -days)
	resolved, err := s.Q.ResolvedIssuesSince(ctx, dbq.ResolvedIssuesSinceParams{ProjectID: p.ID, Since: &since})
	if err != nil {
		return CycleTimeReport{}, err
	}
	hist, err := s.Q.StatusHistorySince(ctx, dbq.StatusHistorySinceParams{ProjectID: p.ID, Since: &since})
	if err != nil {
		return CycleTimeReport{}, err
	}
	statuses, err := s.Q.ListStatuses(ctx)
	if err != nil {
		return CycleTimeReport{}, err
	}
	category := map[string]string{}
	for _, st := range statuses {
		category[st.ID.String()] = st.Category
	}
	started := map[uuid.UUID]time.Time{}
	for _, h := range hist {
		if h.ToRef == nil || category[*h.ToRef] != "in_progress" {
			continue
		}
		if t, ok := started[h.IssueID]; !ok || h.CreatedAt.Before(t) {
			started[h.IssueID] = h.CreatedAt
		}
	}
	rep := CycleTimeReport{Days: days, Issues: []CycleTimeEntry{}}
	var hours []float64
	for _, i := range resolved {
		if i.ResolvedAt == nil {
			continue
		}
		start, ok := started[i.ID]
		if !ok {
			start = i.CreatedAt
		}
		h := i.ResolvedAt.Sub(start).Hours()
		if h < 0 {
			h = 0
		}
		h = math.Round(h*10) / 10
		hours = append(hours, h)
		rep.Issues = append(rep.Issues, CycleTimeEntry{Key: issueKey(p, i), Summary: i.Summary, Hours: h, ResolvedAt: *i.ResolvedAt})
	}
	if len(hours) > 0 {
		sum := 0.0
		for _, h := range hours {
			sum += h
		}
		sort.Float64s(hours)
		rep.Average = math.Round(sum/float64(len(hours))*10) / 10
		rep.Median = hours[len(hours)/2]
		rep.P85 = hours[min(len(hours)-1, int(math.Ceil(0.85*float64(len(hours))))-1)]
	}
	return rep, nil
}

// ---------------------------------------------------------------------------
// Notifications

func (s *Service) Notify(ctx context.Context, q *dbq.Queries, userIDs []uuid.UUID, except *uuid.UUID, issueID *uuid.UUID, kind, title, body string) error {
	seen := map[uuid.UUID]bool{}
	for _, u := range userIDs {
		if seen[u] || (except != nil && u == *except) {
			continue
		}
		seen[u] = true
		if err := q.CreateNotification(ctx, dbq.CreateNotificationParams{UserID: u, IssueID: issueID, Kind: kind, Title: title, Body: body, ActorID: except}); err != nil {
			return err
		}
	}
	return nil
}
