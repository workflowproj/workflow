package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/notify"
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
	if IsReservedField(in.Key) {
		return dbq.FieldDefinition{}, apperr.Conflict("field_reserved", "the key %q is reserved for risks and decisions", in.Key)
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
	if IsReservedField(cur.Key) {
		return cur, apperr.Conflict("field_reserved", "%s belongs to risks and decisions; turn the section on or off in a project's settings instead", cur.Name)
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
	if IsReservedField(f.Key) {
		return apperr.Conflict("field_reserved", "%s belongs to risks and decisions and cannot be deleted", f.Name)
	}
	// Values stay in issues.fields; they simply stop being shown or searchable.
	if err := s.Q.DeleteFieldDefinition(ctx, id); err != nil {
		return err
	}
	Audit(ctx, s.Q, actor, "field.delete", "field", id.String(), nil, map[string]any{"key": f.Key})
	return nil
}

// ---------------------------------------------------------------------------
// Notifications

// OnlyAgents drops, in a service project, the people who cannot browse it from notification
// recipients: customers and other portal users follow their requests through the portal and its
// emails, never through notifications that may quote internal notes.
func (s *Service) OnlyAgents(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	p, err := s.Q.GetProject(ctx, projectID)
	if err != nil || !IsServiceProject(p) {
		return ids, err
	}
	var out []uuid.UUID
	for _, id := range ids {
		u, err := s.Q.GetUser(ctx, id)
		if err != nil {
			continue
		}
		ok, err := s.userCanBrowse(ctx, u, p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// Notify creates in-app notifications for people (never for the actor, `except`) and decides
// from each person's email settings whether and when it is emailed (see package notify).
func (s *Service) Notify(ctx context.Context, q *dbq.Queries, userIDs []uuid.UUID, except *uuid.UUID, issueID *uuid.UUID, kind, title, body string) error {
	seen := map[uuid.UUID]bool{}
	for _, u := range userIDs {
		if seen[u] || (except != nil && u == *except) {
			continue
		}
		seen[u] = true
		var delivery *string
		if r, err := q.EmailRecipient(ctx, u); err == nil {
			if d := notify.DeliveryFor(r, kind, issueID != nil); d != "" {
				delivery = &d
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.CreateNotification(ctx, dbq.CreateNotificationParams{UserID: u, IssueID: issueID, Kind: kind, Title: title, Body: body, ActorID: except, EmailDelivery: delivery}); err != nil {
			return err
		}
		if delivery != nil && *delivery == notify.Immediate && s.Jobs != nil {
			if err := notify.Schedule(ctx, s.Jobs, s.Cfg.EmailBatchWindow, s.Now(), u, *issueID); err != nil {
				return err
			}
		}
	}
	return nil
}
