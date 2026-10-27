package servicedesk

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
	"github.com/workflowproj/workflow/internal/workflow"
)

// FormField is one field of a request type's portal form, mapped to an issue field.
type FormField struct {
	Field    string `json:"field"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// ApprovalConfig says who approves a request type's requests and where their decision leads.
type ApprovalConfig struct {
	// Mode is "any" (one approval is enough) or "all" (everyone must approve); one decline
	// declines either way.
	Mode           string      `json:"mode"`
	UserIDs        []uuid.UUID `json:"userIds,omitempty"`
	GroupID        *uuid.UUID  `json:"groupId,omitempty"`
	ApprovedStatus string      `json:"approvedStatus"`
	DeclinedStatus string      `json:"declinedStatus"`
}

// Icons offered for request types (the portal draws them).
var Icons = []string{"help", "bug", "laptop", "key", "access", "question", "change", "document"}

// systemFields are the issue fields a form may ask for besides custom fields.
var systemFields = map[string]api.FormFieldType{
	"summary": api.FormFieldTypeText, "description": api.FormFieldTypeTextarea,
	"priority": api.FormFieldTypeSelect, "dueDate": api.FormFieldTypeDate,
}

var systemLabels = map[string]string{"summary": "Summary", "description": "Description", "priority": "Priority", "dueDate": "Due date"}

// customTypes maps the custom field types a form can ask customers for; user and version fields
// would show customers the people and releases of the project, so they are left out.
var customTypes = map[string]api.FormFieldType{
	"text": api.FormFieldTypeText, "url": api.FormFieldTypeUrl, "number": api.FormFieldTypeNumber,
	"select": api.FormFieldTypeSelect, "multiselect": api.FormFieldTypeMultiselect, "date": api.FormFieldTypeDate,
	"checkbox": api.FormFieldTypeCheckbox,
}

var priorityOptions = []string{"highest", "high", "medium", "low", "lowest"}

func encodeForm(fields []FormField) (json.RawMessage, error) {
	if fields == nil {
		fields = []FormField{}
	}
	return json.Marshal(fields)
}

func decodeForm(raw json.RawMessage) []FormField {
	var fields []FormField
	_ = json.Unmarshal(raw, &fields)
	return fields
}

func decodeApproval(raw []byte) *ApprovalConfig {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var a ApprovalConfig
	if json.Unmarshal(raw, &a) != nil {
		return nil
	}
	return &a
}

// resolvedField is a form field with what the portal needs to show and check it.
type resolvedField struct {
	FormField
	Type    api.FormFieldType
	Options []string
	custom  *dbq.FieldDefinition
}

// fieldApplies mirrors the tracker's rule: a custom field limited to projects or issue types
// only applies there.
func fieldApplies(f dbq.FieldDefinition, p dbq.Project, t dbq.IssueType) bool {
	return (f.ProjectIds == nil || slices.Contains(f.ProjectIds, p.ID)) && (f.IssueTypeKeys == nil || slices.Contains(f.IssueTypeKeys, t.Key))
}

// resolveForm checks a form against the issue type it creates and works out each field's type.
// Fields that stopped applying (a deleted custom field) are dropped unless strict.
func resolveForm(defs []dbq.FieldDefinition, p dbq.Project, t dbq.IssueType, form []FormField, strict bool) ([]resolvedField, error) {
	seen := map[string]bool{}
	out := []resolvedField{}
	for i, f := range form {
		where := fmt.Sprintf("fields[%d]", i)
		f.Field = strings.TrimSpace(f.Field)
		f.Label = strings.TrimSpace(f.Label)
		f.Help = strings.TrimSpace(f.Help)
		if seen[f.Field] {
			return nil, apperr.Invalid(where, "%s is on the form twice", f.Field)
		}
		seen[f.Field] = true
		if len(f.Label) > 120 || len(f.Help) > 500 {
			return nil, apperr.Invalid(where, "labels are at most 120 characters and help text at most 500")
		}
		r := resolvedField{FormField: f}
		if typ, ok := systemFields[f.Field]; ok {
			r.Type = typ
			if f.Field == "priority" {
				r.Options = priorityOptions
			}
			if r.Label == "" {
				r.Label = systemLabels[f.Field]
			}
			out = append(out, r)
			continue
		}
		k := slices.IndexFunc(defs, func(d dbq.FieldDefinition) bool { return d.Key == f.Field })
		if k < 0 || !fieldApplies(defs[k], p, t) {
			if strict {
				return nil, apperr.Invalid(where, "unknown field %q for %s issues in %s", f.Field, t.Name, p.Key)
			}
			continue
		}
		def := defs[k]
		typ, ok := customTypes[def.Type]
		if !ok {
			if strict {
				return nil, apperr.Invalid(where, "%s fields cannot be on a portal form (they would show customers the project's %ss)", def.Type, def.Type)
			}
			continue
		}
		r.Type, r.custom = typ, &def
		if def.Type == "select" || def.Type == "multiselect" {
			_ = json.Unmarshal(def.Options, &r.Options)
		}
		if r.Label == "" {
			r.Label = def.Name
		}
		out = append(out, r)
	}
	if len(out) > 30 {
		return nil, apperr.Invalid("fields", "a form has at most 30 fields")
	}
	return out, nil
}

// requestTypeInput is a validated request type.
type requestTypeInput struct {
	params   dbq.CreateRequestTypeParams
	approval *ApprovalConfig
}

func (s *Service) validateRequestType(ctx context.Context, q *dbq.Queries, p dbq.Project, in api.RequestTypeInput) (requestTypeInput, error) {
	var out requestTypeInput
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 120 {
		return out, apperr.Invalid("name", "a request type needs a name of at most 120 characters")
	}
	icon := strings.TrimSpace(deref(in.Icon))
	if icon == "" {
		icon = "help"
	}
	if !slices.Contains(Icons, icon) {
		return out, apperr.Invalid("icon", "icon must be one of %s", strings.Join(Icons, ", "))
	}
	it, err := q.GetIssueTypeByKey(ctx, dbq.GetIssueTypeByKeyParams{ProjectID: p.ID, Key: in.IssueTypeKey})
	if err != nil {
		return out, apperr.Invalid("issueTypeKey", "unknown issue type %q in %s", in.IssueTypeKey, p.Key)
	}
	if it.Level != 0 || it.Kind != tracker.KindWork {
		return out, apperr.Invalid("issueTypeKey", "requests are standard issues, not %ss", it.Name)
	}
	defs, err := q.ListFieldDefinitions(ctx)
	if err != nil {
		return out, err
	}
	var form []FormField
	for _, f := range deref(in.Fields) {
		form = append(form, FormField{Field: f.Field, Label: f.Label, Help: deref(f.Help), Required: f.Required})
	}
	resolved, err := resolveForm(defs, p, it, form, true)
	if err != nil {
		return out, err
	}
	form = form[:0]
	for _, r := range resolved {
		form = append(form, r.FormField)
	}
	raw, err := encodeForm(form)
	if err != nil {
		return out, err
	}
	group := strings.TrimSpace(deref(in.Group))
	if len(group) > 60 {
		return out, apperr.Invalid("group", "group names are at most 60 characters")
	}
	if in.Position != nil && (*in.Position < 0 || *in.Position > maxPosition) {
		return out, apperr.Invalid("position", "positions are between 0 and %d", maxPosition)
	}
	out.params = dbq.CreateRequestTypeParams{
		ProjectID: p.ID, Name: name, Description: strings.TrimSpace(deref(in.Description)), Icon: icon,
		IssueTypeID: it.ID, PortalGroup: group, Position: int32(deref(in.Position)), Form: raw,
	}
	if in.Approval != nil {
		a, err := s.validateApproval(ctx, q, it, *in.Approval)
		if err != nil {
			return out, err
		}
		if out.params.Approval, err = json.Marshal(a); err != nil {
			return out, err
		}
		out.approval = &a
	}
	return out, nil
}

func (s *Service) validateApproval(ctx context.Context, q *dbq.Queries, it dbq.IssueType, in api.RequestApprovalConfig) (ApprovalConfig, error) {
	a := ApprovalConfig{Mode: string(in.Mode), ApprovedStatus: strings.TrimSpace(in.ApprovedStatus), DeclinedStatus: strings.TrimSpace(in.DeclinedStatus), GroupID: in.GroupId}
	if a.Mode != "any" && a.Mode != "all" {
		return a, apperr.Invalid("approval.mode", "mode is any or all")
	}
	for _, id := range deref(in.ApproverIds) {
		u, err := q.GetUser(ctx, id)
		if err != nil || !u.Active || u.DeletedAt != nil || u.Kind != "human" {
			return a, apperr.Invalid("approval.approverIds", "approvers must be active people")
		}
		if !slices.Contains(a.UserIDs, id) {
			a.UserIDs = append(a.UserIDs, id)
		}
	}
	if a.GroupID != nil {
		if _, err := q.GetGroup(ctx, *a.GroupID); err != nil {
			return a, apperr.Invalid("approval.groupId", "group not found")
		}
	}
	if len(a.UserIDs) == 0 && a.GroupID == nil {
		return a, apperr.Invalid("approval.approverIds", "name at least one approver or a group")
	}
	def, err := currentWorkflow(ctx, q, it.WorkflowID)
	if err != nil {
		return a, err
	}
	pending, ok := def.StatusByName(statusPendingApproval)
	if !ok {
		return a, apperr.Invalid("approval", "requests wait for approval in a %q status, which the %s workflow does not have", statusPendingApproval, def.Name)
	}
	if initial := def.Initial(); !slices.ContainsFunc(def.Outgoing(initial.ID), func(t workflow.Transition) bool { return t.To == pending.ID }) {
		return a, apperr.Invalid("approval", "the %s workflow has no transition from %s to %s, so new requests could not wait for approval", def.Name, initial.Name, pending.Name)
	}
	for _, f := range []struct{ field, name string }{{"approval.approvedStatus", a.ApprovedStatus}, {"approval.declinedStatus", a.DeclinedStatus}} {
		st, ok := def.StatusByName(f.name)
		if !ok {
			return a, apperr.Invalid(f.field, "the %s workflow has no status %q", def.Name, f.name)
		}
		if !slices.ContainsFunc(def.Outgoing(pending.ID), func(t workflow.Transition) bool { return t.To == st.ID }) {
			return a, apperr.Invalid(f.field, "the %s workflow has no transition from %s to %s", def.Name, pending.Name, st.Name)
		}
	}
	return a, nil
}

func currentWorkflow(ctx context.Context, q *dbq.Queries, workflowID uuid.UUID) (*workflow.Definition, error) {
	wf, err := q.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	wv, err := q.GetWorkflowVersion(ctx, dbq.GetWorkflowVersionParams{WorkflowID: workflowID, Version: wf.CurrentVersion})
	if err != nil {
		return nil, err
	}
	var def workflow.Definition
	if err := json.Unmarshal(wv.Definition, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// ListRequestTypes lists a desk's request types (agents).
func (s *Service) ListRequestTypes(ctx context.Context, actor *authz.Actor, ref string) ([]api.RequestType, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	rts, err := s.Q.ListRequestTypes(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := []api.RequestType{}
	for _, rt := range rts {
		v, err := s.requestTypeView(ctx, p, rt)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) CreateRequestType(ctx context.Context, actor *authz.Actor, ref string, in api.RequestTypeInput) (api.RequestType, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return api.RequestType{}, err
	}
	v, err := s.validateRequestType(ctx, s.Q, p, in)
	if err != nil {
		return api.RequestType{}, err
	}
	if in.Position == nil {
		n, err := s.Q.CountRequestTypes(ctx, p.ID)
		if err != nil {
			return api.RequestType{}, err
		}
		v.params.Position = int32(n)
	}
	rt, err := s.Q.CreateRequestType(ctx, v.params)
	if db.IsUniqueViolation(err) {
		return api.RequestType{}, apperr.Conflict("duplicate_request_type", "%s already has a request type called %q", p.Key, v.params.Name)
	} else if err != nil {
		return api.RequestType{}, err
	}
	tracker.Audit(ctx, s.Q, actor, "request_type.create", "request_type", rt.ID.String(), &p.ID, map[string]any{"name": rt.Name})
	return s.requestTypeView(ctx, p, rt)
}

// requestTypeFor loads a request type for an agent with perm in its project.
func (s *Service) requestTypeFor(ctx context.Context, actor *authz.Actor, id uuid.UUID, perm authz.Permission) (dbq.RequestType, dbq.Project, error) {
	rt, err := s.Q.GetRequestType(ctx, id)
	if err != nil {
		return rt, dbq.Project{}, apperr.NotFound("request type")
	}
	p, _, _, err := s.desk(ctx, actor, rt.ProjectID.String(), perm)
	if err != nil && apperr.IsNotFound(err) {
		return rt, p, apperr.NotFound("request type")
	}
	return rt, p, err
}

func (s *Service) UpdateRequestType(ctx context.Context, actor *authz.Actor, id uuid.UUID, in api.RequestTypeInput) (api.RequestType, error) {
	rt, p, err := s.requestTypeFor(ctx, actor, id, authz.ProjectAdmin)
	if err != nil {
		return api.RequestType{}, err
	}
	v, err := s.validateRequestType(ctx, s.Q, p, in)
	if err != nil {
		return api.RequestType{}, err
	}
	if in.Position == nil {
		v.params.Position = rt.Position
	}
	c := v.params
	rt, err = s.Q.UpdateRequestType(ctx, dbq.UpdateRequestTypeParams{
		ID: rt.ID, Name: c.Name, Description: c.Description, Icon: c.Icon, IssueTypeID: c.IssueTypeID,
		PortalGroup: c.PortalGroup, Position: c.Position, Form: c.Form, Approval: c.Approval,
	})
	if db.IsUniqueViolation(err) {
		return api.RequestType{}, apperr.Conflict("duplicate_request_type", "%s already has a request type called %q", p.Key, c.Name)
	} else if err != nil {
		return api.RequestType{}, err
	}
	tracker.Audit(ctx, s.Q, actor, "request_type.update", "request_type", rt.ID.String(), &p.ID, map[string]any{"name": rt.Name})
	return s.requestTypeView(ctx, p, rt)
}

func (s *Service) DeleteRequestType(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	rt, p, err := s.requestTypeFor(ctx, actor, id, authz.ProjectAdmin)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(_ pgx.Tx, q *dbq.Queries) error {
		n, err := q.CountRequestsOfType(ctx, &rt.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("request_type_in_use", "%d request(s) were raised with %s; it cannot be deleted", n, rt.Name)
		}
		if err := q.DeleteRequestType(ctx, rt.ID); err != nil {
			return err
		}
		tracker.Audit(ctx, q, actor, "request_type.delete", "request_type", rt.ID.String(), &p.ID, map[string]any{"name": rt.Name})
		return nil
	})
}

func (s *Service) requestTypeView(ctx context.Context, p dbq.Project, rt dbq.RequestType) (api.RequestType, error) {
	it, err := s.Q.GetIssueType(ctx, rt.IssueTypeID)
	if err != nil {
		return api.RequestType{}, err
	}
	defs, err := s.Q.ListFieldDefinitions(ctx)
	if err != nil {
		return api.RequestType{}, err
	}
	n, err := s.Q.CountRequestsOfType(ctx, &rt.ID)
	if err != nil {
		return api.RequestType{}, err
	}
	resolved, _ := resolveForm(defs, p, it, decodeForm(rt.Form), false)
	v := api.RequestType{
		Id: rt.ID, Name: rt.Name, Description: rt.Description, Icon: rt.Icon, IssueType: tracker.TypeRef(it),
		Group: rt.PortalGroup, Position: int(rt.Position), Fields: []api.RequestFormField{}, RequestCount: int(n),
	}
	for _, f := range resolved {
		ff := api.RequestFormField{Field: f.Field, Label: f.Label, Required: f.Required, Type: ptr(f.Type)}
		if f.Help != "" {
			ff.Help = ptr(f.Help)
		}
		if f.Options != nil {
			ff.Options = ptr(f.Options)
		}
		v.Fields = append(v.Fields, ff)
	}
	if a := decodeApproval(rt.Approval); a != nil {
		av := api.RequestApprovalConfig{Mode: api.RequestApprovalConfigMode(a.Mode), ApprovedStatus: a.ApprovedStatus, DeclinedStatus: a.DeclinedStatus, GroupId: a.GroupID}
		ids := a.UserIDs
		av.ApproverIds = &ids
		people := []api.PortalPerson{}
		if users, err := s.Q.GetUsersByIDs(ctx, a.UserIDs); err == nil {
			for _, u := range users {
				people = append(people, personView(u))
			}
		}
		av.Approvers = &people
		if a.GroupID != nil {
			if g, err := s.Q.GetGroup(ctx, *a.GroupID); err == nil {
				av.GroupName = &g.DisplayName
			}
		}
		v.Approval = &av
	}
	return v, nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func ptr[T any](v T) *T { return &v }
