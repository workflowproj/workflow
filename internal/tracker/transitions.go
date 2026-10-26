package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/workflow"
)

// ---------------------------------------------------------------------------
// CEL variables

func (s *Service) celActor(actor *authz.Actor, access authz.Access) map[string]any {
	id, email, name := "", "", actor.Name()
	if actor.User != nil {
		id, email = actor.User.ID.String(), actor.User.Email
	}
	t := "user"
	if actor.Type == authz.ActorAutomation || actor.Type == authz.ActorSystem {
		t = "automation"
	}
	return workflow.ActorVars(id, email, name, t, actor.OrgRole, access.Roles, access.List())
}

func statusInfo(ctx context.Context, q *dbq.Queries, id uuid.UUID) (dbq.Status, error) {
	sts, err := q.GetStatusesByIDs(ctx, []uuid.UUID{id})
	if err != nil || len(sts) == 0 {
		return dbq.Status{}, fmt.Errorf("status %s not found", id)
	}
	return sts[0], nil
}

// celIssue builds the `issue` variable for conditions and validators.
func (s *Service) celIssue(ctx context.Context, q *dbq.Queries, p dbq.Project, i dbq.Issue) (map[string]any, error) {
	it, err := q.GetIssueType(ctx, i.TypeID)
	if err != nil {
		return nil, err
	}
	st, err := statusInfo(ctx, q, i.StatusID)
	if err != nil {
		return nil, err
	}
	email := func(id *uuid.UUID) string {
		if id == nil {
			return ""
		}
		if u, err := q.GetUser(ctx, *id); err == nil {
			return u.Email
		}
		return ""
	}
	labels := make([]any, len(i.Labels))
	for k, l := range i.Labels {
		labels[k] = l
	}
	fields := map[string]any{}
	_ = json.Unmarshal(i.Fields, &fields)

	children, err := q.ChildIssues(ctx, &i.ID)
	if err != nil {
		return nil, err
	}
	var subtasks []any
	for _, c := range children {
		cs, err := statusInfo(ctx, q, c.StatusID)
		if err != nil {
			return nil, err
		}
		ct, _ := q.GetIssueType(ctx, c.TypeID)
		subtasks = append(subtasks, map[string]any{
			"key": issueKey(p, c), "summary": c.Summary, "type": ct.Key, "status": cs.Name, "category": cs.Category,
		})
	}
	if subtasks == nil {
		subtasks = []any{}
	}
	dev, err := q.IssueDevLinks(ctx, i.ID)
	if err != nil {
		return nil, err
	}
	devLinks := []any{}
	for _, d := range dev {
		kind := d.Kind
		if kind == "pull_request" {
			kind = "pr"
		}
		devLinks = append(devLinks, map[string]any{
			"kind": kind, "state": d.State, "title": d.Title, "merged": d.State == "merged", "repository": d.FullPath,
		})
	}
	linkRows, err := q.IssueLinks(ctx, i.ID)
	if err != nil {
		return nil, err
	}
	links := []any{}
	for _, l := range linkRows {
		otherID, dir := l.TargetID, "outward"
		if l.TargetID == i.ID {
			otherID, dir = l.SourceID, "inward"
		}
		other, err := q.GetIssue(ctx, otherID)
		if err != nil {
			continue
		}
		op, _ := q.GetProject(ctx, other.ProjectID)
		os, _ := statusInfo(ctx, q, other.StatusID)
		links = append(links, map[string]any{"type": l.LinkType, "direction": dir, "key": issueKey(op, other), "category": os.Category})
	}
	parent := map[string]any{}
	if i.ParentID != nil {
		if par, err := q.GetIssue(ctx, *i.ParentID); err == nil {
			ps, _ := statusInfo(ctx, q, par.StatusID)
			pt, _ := q.GetIssueType(ctx, par.TypeID)
			siblings, err := q.ChildIssues(ctx, &par.ID)
			if err != nil {
				return nil, err
			}
			open := 0
			for _, sib := range siblings {
				if ss, err := statusInfo(ctx, q, sib.StatusID); err == nil && ss.Category != "done" {
					open++
				}
			}
			parent = map[string]any{"key": issueKey(p, par), "type": pt.Key, "status": ps.Name, "category": ps.Category,
				"childCount": int64(len(siblings)), "openChildren": int64(open), "startDate": dateStr(par.StartDate), "dueDate": dateStr(par.DueDate)}
		}
	}
	points := 0.0
	if i.StoryPoints != nil {
		points = *i.StoryPoints
	}
	resolution := ""
	if i.Resolution != nil {
		resolution = *i.Resolution
	}
	due := dateStr(i.DueDate)
	spent, err := s.TimeSpent(ctx, q, []uuid.UUID{i.ID})
	if err != nil {
		return nil, err
	}
	var original, remaining int64
	if i.OriginalEstimateSeconds != nil {
		original = *i.OriginalEstimateSeconds
	}
	if i.RemainingEstimateSeconds != nil {
		remaining = *i.RemainingEstimateSeconds
	}
	return map[string]any{
		"key": issueKey(p, i), "project": p.Key, "type": it.Key, "typeLevel": int64(it.Level),
		"summary": i.Summary, "description": i.Description, "priority": i.Priority,
		"status": st.Name, "statusCategory": st.Category,
		"assignee": email(i.AssigneeID), "reporter": email(i.ReporterID),
		"labels": labels, "storyPoints": points, "resolution": resolution, "startDate": dateStr(i.StartDate), "dueDate": due, "fields": fields,
		"subtasks": subtasks, "children": subtasks, "devLinks": devLinks, "links": links, "parent": parent,
		"originalEstimate": original, "remainingEstimate": remaining, "timeSpent": spent[i.ID],
	}, nil
}

func dateStr(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.Format("2006-01-02")
}

// ---------------------------------------------------------------------------
// Available transitions

type ScreenFieldInfo struct {
	Field    string
	Name     string
	Type     string
	Required bool
	Options  []string
}

type AvailableTransition struct {
	Transition workflow.Transition
	To         workflow.Status
	Screen     []ScreenFieldInfo
}

func (s *Service) screenInfo(ctx context.Context, q *dbq.Queries, screen []workflow.ScreenField) []ScreenFieldInfo {
	defs, _ := q.ListFieldDefinitions(ctx)
	var out []ScreenFieldInfo
	for _, f := range screen {
		info := ScreenFieldInfo{Field: f.Field, Required: f.Required}
		switch f.Field {
		case "resolution":
			info.Name, info.Type, info.Options = "Resolution", "select", f.ResolutionOptions()
		case "comment":
			info.Name, info.Type = "Comment", "markdown"
		case "assignee":
			info.Name, info.Type = "Assignee", "user"
		case "priority":
			info.Name, info.Type, info.Options = "Priority", "select", priorities
		case "storyPoints":
			info.Name, info.Type = "Story points", "number"
		case "dueDate":
			info.Name, info.Type = "Due date", "date"
		case "labels":
			info.Name, info.Type = "Labels", "labels"
		case "fixVersions":
			info.Name, info.Type = "Fix versions", "versions"
		default:
			info.Name, info.Type = f.Field, "text"
			for _, d := range defs {
				if d.Key == f.Field {
					info.Name, info.Type, info.Options = d.Name, d.Type, fieldOptions(d)
				}
			}
		}
		out = append(out, info)
	}
	return out
}

// AvailableTransitions lists transitions from the issue's current status whose condition passes
// for the actor, evaluated against the workflow version the issue is pinned to.
func (s *Service) AvailableTransitions(ctx context.Context, actor *authz.Actor, ref string) ([]AvailableTransition, error) {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	if !access.Has(authz.IssueTransition) {
		return []AvailableTransition{}, nil
	}
	return s.availableFor(ctx, s.Q, actor, access, p, i)
}

func (s *Service) availableFor(ctx context.Context, q *dbq.Queries, actor *authz.Actor, access authz.Access, p dbq.Project, i dbq.Issue) ([]AvailableTransition, error) {
	def, _, err := workflowDef(ctx, q, i.WorkflowVersionID)
	if err != nil {
		return nil, err
	}
	var issueVars map[string]any
	out := []AvailableTransition{}
	for _, t := range def.Outgoing(i.StatusID) {
		if t.Condition != "" {
			if issueVars == nil {
				if issueVars, err = s.celIssue(ctx, q, p, i); err != nil {
					return nil, err
				}
			}
			ok, err := s.Eval.Eval(t.Condition, s.vars(issueVars, actor, access, def, i.StatusID, t))
			if err != nil || !ok {
				continue
			}
		}
		to, _ := def.Status(t.To)
		out = append(out, AvailableTransition{Transition: t, To: to, Screen: s.screenInfo(ctx, q, t.Screen)})
	}
	return out, nil
}

func (s *Service) vars(issueVars map[string]any, actor *authz.Actor, access authz.Access, def *workflow.Definition, from uuid.UUID, t workflow.Transition) workflow.Vars {
	fromSt, _ := def.Status(from)
	toSt, _ := def.Status(t.To)
	return workflow.Vars{
		Issue:      issueVars,
		Actor:      s.celActor(actor, access),
		Transition: map[string]any{"id": t.ID, "name": t.Name, "from": fromSt.Name, "to": toSt.Name},
		Now:        s.Now(),
	}
}

// ---------------------------------------------------------------------------
// Executing a transition

type TransitionInput struct {
	TransitionID string
	Fields       map[string]any
	Comment      string
	// CommentVisibility applies to the comment in service projects (see CommentInput).
	CommentVisibility string
	Version           *int
}

func (s *Service) Transition(ctx context.Context, actor *authz.Actor, ref string, in TransitionInput) (dbq.Issue, dbq.Project, error) {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.IssueTransition)
	if err != nil {
		return i, p, err
	}
	var out dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		locked, err := q.GetIssueForUpdate(ctx, i.ID)
		if err != nil {
			return err
		}
		out, err = s.transitionTx(ctx, tx, q, actor, access, p, locked, in)
		return err
	})
	return out, p, err
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	}
	return false
}

// transitionTx runs a transition on a locked issue inside the caller's transaction:
// version check, condition, screen, validators, status + history, post-functions, events.
func (s *Service) transitionTx(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, access authz.Access, p dbq.Project, i dbq.Issue, in TransitionInput) (dbq.Issue, error) {
	key := issueKey(p, i)
	if in.Version != nil && int32(*in.Version) != i.Version {
		return i, apperr.Conflict("version_conflict", "%s was changed by someone else; reload and try again", key)
	}
	def, _, err := workflowDef(ctx, q, i.WorkflowVersionID)
	if err != nil {
		return i, err
	}
	t, ok := def.Transition(in.TransitionID)
	if !ok {
		return i, apperr.Invalid("transitionId", "unknown transition %q", in.TransitionID)
	}
	if !t.AppliesFrom(i.StatusID) {
		cur, _ := def.Status(i.StatusID)
		return i, apperr.Unprocessable("transition_not_available", "%q is not available from %s", t.Name, cur.Name)
	}
	issueVars, err := s.celIssue(ctx, q, p, i)
	if err != nil {
		return i, err
	}
	if t.Condition != "" {
		ok, err := s.Eval.Eval(t.Condition, s.vars(issueVars, actor, access, def, i.StatusID, t))
		if err != nil || !ok {
			return i, apperr.Forbidden("you are not allowed to run %q on %s", t.Name, key)
		}
	}

	upd := updateFromIssue(i)
	fromSt, _ := def.Status(i.StatusID)
	toSt, _ := def.Status(t.To)
	upd.StatusID = toSt.ID
	var changes []change
	fromRef, toRef := fromSt.ID.String(), toSt.ID.String()
	changes = append(changes, change{"status", &fromSt.Name, &toSt.Name, &fromRef, &toRef})

	// Screen values.
	onScreen := map[string]workflow.ScreenField{}
	for _, f := range t.Screen {
		onScreen[f.Field] = f
	}
	invalid := apperr.New(http.StatusUnprocessableEntity, "screen_invalid", "Some fields on the %q screen need attention", t.Name)
	for k := range in.Fields {
		if _, ok := onScreen[k]; !ok {
			invalid.Fields = append(invalid.Fields, apperr.FieldError{Field: k, Message: "not on this transition's screen"})
		}
	}
	comment := strings.TrimSpace(in.Comment)
	var chosenResolution *string
	customValues := map[string]any{}
	for _, f := range t.Screen {
		v, present := in.Fields[f.Field]
		if f.Field == "comment" && comment != "" {
			v, present = comment, true
		}
		if f.Required && (!present || isEmptyValue(v)) {
			invalid.Fields = append(invalid.Fields, apperr.FieldError{Field: f.Field, Message: "is required"})
			continue
		}
		if !present {
			continue
		}
		if f.Field == "resolution" {
			str, _ := v.(string)
			if opts := f.ResolutionOptions(); !slices.Contains(opts, str) {
				invalid.Fields = append(invalid.Fields, apperr.FieldError{Field: f.Field, Message: "must be one of " + strings.Join(opts, ", ")})
			} else {
				chosenResolution = &str
			}
			continue
		}
		if err := s.applyScreenField(ctx, q, p, i, &upd, f.Field, v, &comment, customValues, &changes); err != nil {
			if ae, ok := apperr.As(err); ok {
				invalid.Fields = append(invalid.Fields, apperr.FieldError{Field: f.Field, Message: ae.Message})
				continue
			}
			return i, err
		}
	}
	if len(invalid.Fields) > 0 {
		return i, invalid
	}
	if len(customValues) > 0 {
		it, err := q.GetIssueType(ctx, i.TypeID)
		if err != nil {
			return i, err
		}
		fields, fc, err := s.mergeFields(ctx, q, p, it, upd.Fields, customValues)
		if err != nil {
			return i, err
		}
		upd.Fields = fields
		for _, c := range fc {
			changes = append(changes, change{c.field, c.from, c.to, nil, nil})
		}
	}
	prevResolution := i.Resolution
	applyResolution(&upd, toSt.Category, chosenResolution, s.Now())
	if !strPtrEqual(prevResolution, upd.Resolution) {
		changes = append(changes, change{"resolution", prevResolution, upd.Resolution, nil, nil})
	}

	// Validators see the issue as it will be after the transition.
	if len(t.Validators) > 0 {
		pending := issueFromUpdate(i, upd)
		pendingVars, err := s.celIssue(ctx, q, p, pending)
		if err != nil {
			return i, err
		}
		if failed := s.Eval.RunValidators(t.Validators, s.vars(pendingVars, actor, access, def, i.StatusID, t)); len(failed) > 0 {
			e := apperr.Unprocessable("validation_failed", "%s cannot move to %s yet", key, toSt.Name)
			for _, f := range failed {
				e.Fields = append(e.Fields, apperr.FieldError{Message: f.Message})
			}
			return i, e
		}
	}

	// Post-functions that change the issue run here; side effects are enqueued below.
	for _, pf := range t.PostFunctions {
		if err := s.applyPostFunction(ctx, q, p, i, &upd, actor, pf, &changes); err != nil {
			return i, err
		}
	}
	if toSt.Category == "done" {
		if err := s.fillDecidedOn(ctx, q, p, i, &upd, &changes); err != nil {
			return i, err
		}
	}

	out, err := q.UpdateIssue(ctx, upd)
	if err != nil {
		if db.IsNoRows(err) {
			return i, apperr.Conflict("version_conflict", "%s was changed concurrently; reload and try again", key)
		}
		return i, err
	}
	for _, c := range changes {
		if err := s.history(ctx, q, i.ID, actor, c.field, c.from, c.to, c.fromRef, c.toRef); err != nil {
			return i, err
		}
	}
	event := map[string]any{
		"transition": t.Name, "transitionId": t.ID, "from": fromSt.Name, "to": toSt.Name,
		"fromCategory": fromSt.Category, "toCategory": toSt.Category,
	}
	if comment != "" {
		internal, err := commentInternal(p, actor, in.CommentVisibility)
		if err != nil {
			return i, err
		}
		c, err := q.CreateComment(ctx, dbq.CreateCommentParams{IssueID: i.ID, AuthorID: actor.UserID(), Body: comment, Source: commentSource(actor), Via: via(actor), Internal: internal})
		if err != nil {
			return i, err
		}
		if err := s.commentRecorded(ctx, q, actor, p, c, map[string]any{}); err != nil {
			return i, err
		}
		// The comment travels with the transition (one event, one notification), so the service
		// desk can include a reply to the customer in its email.
		event["commentId"] = c.ID
	}
	if s.Jobs != nil {
		for _, pf := range t.PostFunctions {
			if pf.Notify == "" && pf.Webhook == "" {
				continue
			}
			args := jobs.PostFunction{IssueID: i.ID, ActorID: actor.UserID(), Transition: t.Name, FromStatus: fromSt.Name, ToStatus: toSt.Name, Notify: pf.Notify, Webhook: pf.Webhook}
			if _, err := s.Jobs.InsertTx(ctx, tx, args, nil); err != nil {
				return i, err
			}
		}
	}
	return out, s.emit(ctx, tx, actor, p, &out, "issue.transitioned", event)
}

func commentSource(actor *authz.Actor) string {
	switch actor.Type {
	case authz.ActorAutomation:
		return "automation"
	case authz.ActorSCM:
		return "scm"
	case authz.ActorPortal:
		return "portal"
	}
	return "user"
}

func issueFromUpdate(i dbq.Issue, u dbq.UpdateIssueParams) dbq.Issue {
	i.TypeID, i.ParentID, i.Summary, i.Description = u.TypeID, u.ParentID, u.Summary, u.Description
	i.StatusID, i.Priority, i.AssigneeID, i.ReporterID = u.StatusID, u.Priority, u.AssigneeID, u.ReporterID
	i.SprintID, i.StoryPoints, i.StartDate, i.DueDate = u.SprintID, u.StoryPoints, u.StartDate, u.DueDate
	i.Labels, i.Resolution, i.ResolvedAt, i.Fields = u.Labels, u.Resolution, u.ResolvedAt, u.Fields
	return i
}

// applyScreenField applies a screen value other than the resolution (which the screen field's
// options govern; see transitionTx).
func (s *Service) applyScreenField(ctx context.Context, q *dbq.Queries, p dbq.Project, i dbq.Issue, upd *dbq.UpdateIssueParams, field string, v any, comment *string, custom map[string]any, changes *[]change) error {
	switch field {
	case "comment":
		str, _ := v.(string)
		*comment = strings.TrimSpace(str)
	case "assignee":
		var next *uuid.UUID
		if v != nil {
			str, _ := v.(string)
			id, err := uuid.Parse(str)
			if err != nil {
				return apperr.Invalid(field, "expected a user id")
			}
			u, err := s.assignable(ctx, q, p, id)
			if err != nil {
				return err
			}
			next = &u.ID
		}
		if !uuidPtrEqual(next, upd.AssigneeID) {
			*changes = append(*changes, change{"assignee", userName(ctx, q, upd.AssigneeID), userName(ctx, q, next), idRef(upd.AssigneeID), idRef(next)})
			upd.AssigneeID = next
		}
	case "priority":
		str, _ := v.(string)
		if !slices.Contains(priorities, str) {
			return apperr.Invalid(field, "unknown priority")
		}
		if str != upd.Priority {
			*changes = append(*changes, change{"priority", &upd.Priority, &str, nil, nil})
			upd.Priority = str
		}
	case "storyPoints":
		var next *float64
		switch n := v.(type) {
		case float64:
			next = &n
		case string:
			f, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return apperr.Invalid(field, "expected a number")
			}
			next = &f
		case nil:
		default:
			return apperr.Invalid(field, "expected a number")
		}
		if !floatPtrEqual(next, upd.StoryPoints) {
			*changes = append(*changes, change{"storyPoints", fmtFloat(upd.StoryPoints), fmtFloat(next), nil, nil})
			upd.StoryPoints = next
		}
	case "dueDate":
		var next *time.Time
		if v != nil {
			str, _ := v.(string)
			d, err := time.Parse("2006-01-02", str)
			if err != nil {
				return apperr.Invalid(field, "expected YYYY-MM-DD")
			}
			next = &d
		}
		if !datePtrEqual(next, upd.DueDate) {
			*changes = append(*changes, change{"dueDate", fmtDate(upd.DueDate), fmtDate(next), nil, nil})
			upd.DueDate = next
		}
	case "labels":
		arr, _ := v.([]any)
		var raw []string
		for _, x := range arr {
			if str, ok := x.(string); ok {
				raw = append(raw, str)
			}
		}
		labels, err := normalizeLabels(raw)
		if err != nil {
			return err
		}
		*changes = append(*changes, change{"labels", strp(strings.Join(upd.Labels, ", ")), strp(strings.Join(labels, ", ")), nil, nil})
		upd.Labels = labels
	case "fixVersions":
		arr, _ := v.([]any)
		var ids []uuid.UUID
		for _, x := range arr {
			str, _ := x.(string)
			id, err := uuid.Parse(str)
			if err != nil {
				return apperr.Invalid(field, "expected release ids")
			}
			ids = append(ids, id)
		}
		if err := q.ClearIssueReleases(ctx, i.ID); err != nil {
			return err
		}
		if err := s.setReleases(ctx, q, p, i.ID, ids); err != nil {
			return err
		}
	default:
		custom[field] = v
	}
	return nil
}

func (s *Service) applyPostFunction(ctx context.Context, q *dbq.Queries, p dbq.Project, i dbq.Issue, upd *dbq.UpdateIssueParams, actor *authz.Actor, pf workflow.PostFunction, changes *[]change) error {
	switch {
	case pf.AssignTo != "":
		var next *uuid.UUID
		switch pf.AssignTo {
		case "actor":
			next = actor.UserID()
			if next == nil {
				return nil // automation has no user to assign
			}
		case "reporter":
			next = upd.ReporterID
		case "unassigned":
		default:
			u, err := q.GetUserByEmail(ctx, pf.AssignTo)
			if err != nil {
				s.Log.Warn("post-function assignTo: unknown user", "email", pf.AssignTo, "issue", issueKey(p, i))
				return nil
			}
			next = &u.ID
		}
		if next != nil {
			if _, err := s.assignable(ctx, q, p, *next); err != nil {
				return nil // never fail a transition because the target cannot be assigned
			}
		}
		if !uuidPtrEqual(next, upd.AssigneeID) {
			*changes = append(*changes, change{"assignee", userName(ctx, q, upd.AssigneeID), userName(ctx, q, next), idRef(upd.AssigneeID), idRef(next)})
			upd.AssigneeID = next
			if next != nil {
				_ = q.AddWatcher(ctx, dbq.AddWatcherParams{IssueID: i.ID, UserID: *next})
			}
		}
	case pf.SetField != nil:
		return s.setSystemOrCustom(ctx, q, p, i, upd, pf.SetField.Field, pf.SetField.Value, changes)
	case pf.ClearField != "":
		return s.setSystemOrCustom(ctx, q, p, i, upd, pf.ClearField, nil, changes)
	}
	return nil
}

func (s *Service) setSystemOrCustom(ctx context.Context, q *dbq.Queries, p dbq.Project, i dbq.Issue, upd *dbq.UpdateIssueParams, field string, v any, changes *[]change) error {
	switch field {
	case "priority", "storyPoints", "dueDate", "labels", "assignee":
		var c string
		return s.applyScreenField(ctx, q, p, i, upd, field, v, &c, nil, changes)
	case "resolution":
		var next *string
		if str, ok := v.(string); ok && str != "" {
			next = &str
		}
		if !strPtrEqual(next, upd.Resolution) {
			*changes = append(*changes, change{"resolution", upd.Resolution, next, nil, nil})
			upd.Resolution = next
		}
		return nil
	}
	it, err := q.GetIssueType(ctx, i.TypeID)
	if err != nil {
		return err
	}
	fields, fc, err := s.mergeFields(ctx, q, p, it, upd.Fields, map[string]any{field: v})
	if err != nil {
		s.Log.Warn("post-function setField failed", "field", field, "error", err)
		return nil
	}
	upd.Fields = fields
	for _, c := range fc {
		*changes = append(*changes, change{c.field, c.from, c.to, nil, nil})
	}
	return nil
}

// TransitionToStatus runs the first available transition into a status. Used by automation and
// Git events; the workflow engine still applies conditions and validators.
func (s *Service) TransitionToStatus(ctx context.Context, actor *authz.Actor, issueID uuid.UUID, statusName string, comment string) (bool, error) {
	return s.runNamedTransition(ctx, actor, issueID, "", func(at AvailableTransition) bool {
		return strings.EqualFold(at.To.Name, statusName)
	}, comment)
}

// TransitionFromStatus is TransitionToStatus for an issue that must still be in status from
// (checked under the issue's row lock): it does nothing, and reports false, once the issue has
// moved on.
func (s *Service) TransitionFromStatus(ctx context.Context, actor *authz.Actor, issueID uuid.UUID, from, statusName string) (bool, error) {
	return s.runNamedTransition(ctx, actor, issueID, from, func(at AvailableTransition) bool {
		return strings.EqualFold(at.To.Name, statusName)
	}, "")
}

// TransitionToCategory runs the first available transition into a status of the category,
// returning the status entered ("" when the issue is already in that category or none is allowed).
func (s *Service) TransitionToCategory(ctx context.Context, actor *authz.Actor, issueID uuid.UUID, category string) (string, error) {
	i, err := s.Q.GetIssue(ctx, issueID)
	if err != nil {
		return "", apperr.NotFound("issue")
	}
	if cur, err := statusInfo(ctx, s.Q, i.StatusID); err != nil || cur.Category == category {
		return "", err
	}
	var entered string
	ran, err := s.runNamedTransition(ctx, actor, issueID, "", func(at AvailableTransition) bool {
		if at.To.Category == category {
			entered = at.To.Name
			return true
		}
		return false
	}, "")
	if err != nil || !ran {
		return "", err
	}
	return entered, nil
}

// TransitionByName runs a transition matched by smart-commit name: the slug of the transition
// name or of its target status ("#in-review", "#done", "#start-work").
func (s *Service) TransitionByName(ctx context.Context, actor *authz.Actor, issueID uuid.UUID, name string) (bool, error) {
	slug := workflow.Slug(name)
	return s.runNamedTransition(ctx, actor, issueID, "", func(at AvailableTransition) bool {
		return workflow.Slug(at.Transition.Name) == slug || workflow.Slug(at.To.Name) == slug || at.Transition.ID == slug
	}, "")
}

// runNamedTransition runs the first available transition that match accepts. With from set, it
// runs only while the issue is in that status.
func (s *Service) runNamedTransition(ctx context.Context, actor *authz.Actor, issueID uuid.UUID, from string, match func(AvailableTransition) bool, comment string) (bool, error) {
	ran := false
	err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		i, err := q.GetIssueForUpdate(ctx, issueID)
		if err != nil {
			return err
		}
		if from != "" {
			cur, err := statusInfo(ctx, q, i.StatusID)
			if err != nil {
				return err
			}
			if !strings.EqualFold(cur.Name, from) {
				return nil
			}
		}
		p, err := q.GetProject(ctx, i.ProjectID)
		if err != nil {
			return err
		}
		access, err := s.Authz.Access(ctx, actor, p)
		if err != nil {
			return err
		}
		if !access.Has(authz.IssueTransition) {
			return apperr.Forbidden("%s cannot transition issues in %s", actor.Name(), p.Key)
		}
		avail, err := s.availableFor(ctx, q, actor, access, p, i)
		if err != nil {
			return err
		}
		for _, at := range avail {
			if !match(at) {
				continue
			}
			for _, f := range at.Screen {
				if f.Required && !(f.Field == "comment" && comment != "") {
					return apperr.Unprocessable("screen_required", "%q needs %s, which cannot be supplied here", at.Transition.Name, f.Name)
				}
			}
			in := TransitionInput{TransitionID: at.Transition.ID, Comment: comment}
			if _, err := s.transitionTx(ctx, tx, q, actor, access, p, i, in); err != nil {
				return err
			}
			ran = true
			return nil
		}
		return nil
	})
	return ran, err
}

// ---------------------------------------------------------------------------
// Drag and drop

type MoveInput struct {
	StatusID      *uuid.UUID
	TransitionID  *string
	SprintID      Opt[uuid.UUID]
	AfterIssueID  *uuid.UUID
	BeforeIssueID *uuid.UUID
}

// Move applies a board/backlog drop atomically: optional transition, sprint change and re-rank.
func (s *Service) Move(ctx context.Context, actor *authz.Actor, ref string, in MoveInput) (dbq.Issue, dbq.Project, error) {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return i, p, err
	}
	var out dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		// Lock the project row: ranks are generated from neighbours and must not race.
		if _, err := q.LockProjectCounter(ctx, p.ID); err != nil {
			return err
		}
		cur, err := q.GetIssueForUpdate(ctx, i.ID)
		if err != nil {
			return err
		}
		emitUpdate := map[string]any{}

		if in.TransitionID != nil || (in.StatusID != nil && *in.StatusID != cur.StatusID) {
			if !access.Has(authz.IssueTransition) {
				return apperr.Forbidden("you need the issue.transition permission")
			}
			avail, err := s.availableFor(ctx, q, actor, access, p, cur)
			if err != nil {
				return err
			}
			var chosen *AvailableTransition
			for k := range avail {
				at := avail[k]
				if (in.TransitionID != nil && at.Transition.ID == *in.TransitionID) ||
					(in.TransitionID == nil && in.StatusID != nil && at.To.ID == *in.StatusID) {
					chosen = &at
					break
				}
			}
			if chosen == nil {
				return apperr.Unprocessable("transition_not_allowed", "the workflow does not allow that move from the current status")
			}
			for _, f := range chosen.Screen {
				if f.Required {
					e := apperr.Unprocessable("screen_required", "%q needs more information", chosen.Transition.Name)
					e.Fields = []apperr.FieldError{{Field: "transitionId", Message: chosen.Transition.ID}}
					return e
				}
			}
			cur, err = s.transitionTx(ctx, tx, q, actor, access, p, cur, TransitionInput{TransitionID: chosen.Transition.ID})
			if err != nil {
				return err
			}
		}

		scheduling := access.Has(authz.SprintManage) || canEdit(access, actor, cur)
		if in.SprintID.Set && !uuidPtrEqual(in.SprintID.Value, cur.SprintID) {
			if !scheduling {
				return apperr.Forbidden("you cannot move %s between sprints", issueKey(p, cur))
			}
			it, err := q.GetIssueType(ctx, cur.TypeID)
			if err != nil {
				return err
			}
			if !sprintable(it) {
				return apperr.Invalid("sprintId", "only standard work can be moved between sprints")
			}
			if in.SprintID.Value != nil {
				if _, err := s.sprintFor(ctx, q, p, *in.SprintID.Value); err != nil {
					return err
				}
			}
			upd := updateFromIssue(cur)
			upd.SprintID = in.SprintID.Value
			prev := cur.SprintID
			if cur, err = q.UpdateIssue(ctx, upd); err != nil {
				return err
			}
			if err := q.SetChildrenSprint(ctx, dbq.SetChildrenSprintParams{ParentID: &cur.ID, SprintID: cur.SprintID}); err != nil {
				return err
			}
			if err := s.history(ctx, q, cur.ID, actor, "sprint", sprintName(ctx, q, prev), sprintName(ctx, q, cur.SprintID), idRef(prev), idRef(cur.SprintID)); err != nil {
				return err
			}
			emitUpdate["fields"] = []string{"sprint"}
		}

		if in.AfterIssueID != nil || in.BeforeIssueID != nil {
			if !scheduling {
				return apperr.Forbidden("you cannot rank %s", issueKey(p, cur))
			}
			neighbour := func(id *uuid.UUID) (*dbq.Issue, error) {
				if id == nil {
					return nil, nil
				}
				if *id == cur.ID {
					return nil, apperr.Invalid("afterIssueId", "an issue cannot be ranked relative to itself")
				}
				n, err := q.GetIssue(ctx, *id)
				if err != nil || n.ProjectID != p.ID {
					return nil, apperr.Invalid("afterIssueId", "neighbouring issue not found in %s", p.Key)
				}
				return &n, nil
			}
			after, err := neighbour(in.AfterIssueID)
			if err != nil {
				return err
			}
			var before *dbq.Issue
			if after == nil {
				if before, err = neighbour(in.BeforeIssueID); err != nil {
					return err
				}
			}
			r, err := s.rankBetween(ctx, q, p.ID, cur.ID, after, before)
			if err != nil {
				return err
			}
			if r != cur.Rank {
				if cur, err = q.SetIssueRank(ctx, dbq.SetIssueRankParams{ID: cur.ID, Rank: r}); err != nil {
					return err
				}
			}
		}
		out = cur
		if len(emitUpdate) > 0 {
			return s.emit(ctx, tx, actor, p, &out, "issue.updated", emitUpdate)
		}
		// Rank-only changes just refresh open boards.
		return publishOnly(ctx, tx, actor, p, &out)
	})
	return out, p, err
}
