package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	"github.com/workflowproj/workflow/internal/rank"
)

var priorities = []string{"highest", "high", "medium", "low", "lowest"}

// Opt is a PATCH field: Set reports presence, Value may be nil to clear.
type Opt[T any] struct {
	Set   bool
	Value *T
}

func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, Value: &v} }
func Null[T any]() Opt[T]    { return Opt[T]{Set: true} }

type IssueInput struct {
	ProjectKey  string
	TypeKey     string
	Summary     string
	Description string
	Priority    string
	AssigneeID  *uuid.UUID
	ParentKey   string
	SprintID    *uuid.UUID
	StoryPoints *float64
	StartDate   *time.Time
	DueDate     *time.Time
	Labels      []string
	ReleaseIDs  []uuid.UUID
	Fields      map[string]any
	// Estimates in seconds; the remaining estimate defaults to the original one.
	OriginalEstimate  *int64
	RemainingEstimate *int64
	// OnCreate runs in the creating transaction before the issue.created event (the service
	// desk records the request, its participants and its approval).
	OnCreate func(ctx context.Context, tx pgx.Tx, q *dbq.Queries, issue dbq.Issue) error
}

type IssuePatch struct {
	Version     *int
	TypeKey     *string
	Summary     *string
	Description *string
	Priority    *string
	AssigneeID  Opt[uuid.UUID]
	ReporterID  Opt[uuid.UUID]
	ParentKey   Opt[string]
	SprintID    Opt[uuid.UUID]
	StoryPoints Opt[float64]
	StartDate   Opt[time.Time]
	DueDate     Opt[time.Time]
	Labels      *[]string
	ReleaseIDs  *[]uuid.UUID
	Fields      map[string]any
	// Estimates in seconds. Setting the original estimate on an issue without a remaining
	// estimate sets that too.
	OriginalEstimate  Opt[int64]
	RemainingEstimate Opt[int64]
}

func normalizeLabels(in []string) ([]string, error) {
	out := []string{}
	for _, l := range in {
		l = strings.Join(strings.Fields(strings.TrimSpace(l)), "-")
		if l == "" {
			continue
		}
		if len(l) > 50 {
			return nil, apperr.Invalid("labels", "labels must be at most 50 characters")
		}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	if len(out) > 30 {
		return nil, apperr.Invalid("labels", "at most 30 labels per issue")
	}
	return out, nil
}

// assignable checks a prospective assignee can see the project.
func (s *Service) assignable(ctx context.Context, q *dbq.Queries, p dbq.Project, userID uuid.UUID) (dbq.User, error) {
	u, err := q.GetUser(ctx, userID)
	if err != nil {
		return u, apperr.Invalid("assigneeId", "user not found")
	}
	ok, err := s.userCanBrowse(ctx, u, p)
	if err != nil {
		return u, err
	}
	if !ok {
		return u, apperr.Invalid("assigneeId", "%s cannot access %s", u.DisplayName, p.Key)
	}
	return u, nil
}

// requireAssignPermission: changing the assignee needs issue.assign, except assigning yourself.
func requireAssignPermission(access authz.Access, actor *authz.Actor, newAssignee *uuid.UUID) error {
	if access.Has(authz.IssueAssign) {
		return nil
	}
	if uid := actor.UserID(); uid != nil && newAssignee != nil && *newAssignee == *uid {
		return nil
	}
	return apperr.Forbidden("you need the issue.assign permission to change assignees")
}

func (s *Service) sprintFor(ctx context.Context, q *dbq.Queries, p dbq.Project, id uuid.UUID) (dbq.Sprint, error) {
	sp, err := q.GetSprint(ctx, id)
	if err != nil || sp.ProjectID != p.ID {
		return sp, apperr.Invalid("sprintId", "sprint not found in %s", p.Key)
	}
	if sp.State == "closed" {
		return sp, apperr.Invalid("sprintId", "issues cannot be added to a closed sprint")
	}
	return sp, nil
}

// resolveParent loads a parent issue in the same project and enforces parent level = child level + 1.
func (s *Service) resolveParent(ctx context.Context, q *dbq.Queries, p dbq.Project, child dbq.IssueType, parentKey string) (*dbq.Issue, error) {
	if parentKey == "" {
		if child.Level < 0 {
			return nil, apperr.Invalid("parentKey", "a %s must have a parent issue", child.Name)
		}
		return nil, nil
	}
	parent, err := s.ResolveIssue(ctx, q, parentKey)
	if err != nil {
		return nil, apperr.Invalid("parentKey", "parent issue %s not found", parentKey)
	}
	if parent.ProjectID != p.ID {
		return nil, apperr.Invalid("parentKey", "the parent must be in the same project")
	}
	pt, err := q.GetIssueType(ctx, parent.TypeID)
	if err != nil {
		return nil, err
	}
	if pt.Level != child.Level+1 {
		return nil, apperr.Invalid("parentKey", "a %s (level %d) cannot be the parent of a %s (level %d)", pt.Name, pt.Level, child.Name, child.Level)
	}
	return &parent, nil
}

func (s *Service) CreateIssue(ctx context.Context, actor *authz.Actor, in IssueInput) (dbq.Issue, dbq.Project, error) {
	p, access, err := s.ProjectFor(ctx, actor, in.ProjectKey, authz.IssueCreate)
	if err != nil {
		return dbq.Issue{}, p, err
	}
	summary := strings.TrimSpace(in.Summary)
	if summary == "" || len(summary) > 255 {
		return dbq.Issue{}, p, apperr.Invalid("summary", "summary must be 1-255 characters")
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if !slices.Contains(priorities, in.Priority) {
		return dbq.Issue{}, p, apperr.Invalid("priority", "unknown priority %q", in.Priority)
	}
	labels, err := normalizeLabels(in.Labels)
	if err != nil {
		return dbq.Issue{}, p, err
	}
	if in.AssigneeID != nil {
		if err := requireAssignPermission(access, actor, in.AssigneeID); err != nil {
			return dbq.Issue{}, p, err
		}
	}
	if err := checkEstimate("originalEstimate", in.OriginalEstimate); err != nil {
		return dbq.Issue{}, p, err
	}
	if err := checkEstimate("remainingEstimate", in.RemainingEstimate); err != nil {
		return dbq.Issue{}, p, err
	}
	if in.RemainingEstimate == nil && in.OriginalEstimate != nil {
		in.RemainingEstimate = ptr(*in.OriginalEstimate)
	}
	var issue dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		it, err := q.GetIssueTypeByKey(ctx, dbq.GetIssueTypeByKeyParams{ProjectID: p.ID, Key: in.TypeKey})
		if err != nil {
			return apperr.Invalid("typeKey", "unknown issue type %q in %s", in.TypeKey, p.Key)
		}
		parent, err := s.resolveParent(ctx, q, p, it, in.ParentKey)
		if err != nil {
			return err
		}
		if in.AssigneeID != nil {
			if _, err := s.assignable(ctx, q, p, *in.AssigneeID); err != nil {
				return err
			}
		}
		var sprintID *uuid.UUID
		switch {
		case it.Level < 0:
			sprintID = parent.SprintID // sub-tasks follow their parent
		case it.Level > 0:
			if in.SprintID != nil {
				return apperr.Invalid("sprintId", "%s issues cannot be added to a sprint", it.Name)
			}
		case in.SprintID != nil:
			sp, err := s.sprintFor(ctx, q, p, *in.SprintID)
			if err != nil {
				return err
			}
			sprintID = &sp.ID
		}
		fields, err := s.validateFields(ctx, q, p, it, nil, in.Fields)
		if err != nil {
			return err
		}
		def, wv, err := currentVersion(ctx, q, it.WorkflowID)
		if err != nil {
			return err
		}
		initial := def.Initial()

		counter, err := q.LockProjectCounter(ctx, p.ID)
		if err != nil {
			return err
		}
		number := counter + 1
		if err := q.SetProjectCounter(ctx, dbq.SetProjectCounterParams{ID: p.ID, IssueCounter: number}); err != nil {
			return err
		}
		maxRank, err := q.MaxRank(ctx, p.ID)
		if err != nil {
			return err
		}
		r, err := rank.After(maxRank)
		if err != nil {
			return err
		}
		var resolution *string
		var resolvedAt *time.Time
		if initial.Category == "done" {
			resolution, resolvedAt = strp("Done"), ptr(s.Now())
		}
		var parentID *uuid.UUID
		if parent != nil {
			parentID = &parent.ID
		}
		issue, err = q.CreateIssue(ctx, dbq.CreateIssueParams{
			ProjectID: p.ID, Number: number, TypeID: it.ID, ParentID: parentID, Summary: summary,
			Description: in.Description, StatusID: initial.ID, WorkflowVersionID: wv.ID, Priority: in.Priority,
			AssigneeID: in.AssigneeID, ReporterID: actor.UserID(), SprintID: sprintID, StoryPoints: in.StoryPoints,
			StartDate: in.StartDate, DueDate: in.DueDate, Labels: labels, Resolution: resolution, ResolvedAt: resolvedAt,
			Fields: fields, Rank: r, OriginalEstimateSeconds: in.OriginalEstimate, RemainingEstimateSeconds: in.RemainingEstimate,
		})
		if err != nil {
			return err
		}
		if err := s.setReleases(ctx, q, p, issue.ID, in.ReleaseIDs); err != nil {
			return err
		}
		if err := s.history(ctx, q, issue.ID, actor, "created", nil, strp(summary), nil, nil); err != nil {
			return err
		}
		watchers := []*uuid.UUID{actor.UserID(), in.AssigneeID}
		if actor.Type == authz.ActorPortal {
			watchers[0] = nil // customers follow their requests in the portal
		}
		for _, w := range watchers {
			if w != nil {
				if err := q.AddWatcher(ctx, dbq.AddWatcherParams{IssueID: issue.ID, UserID: *w}); err != nil {
					return err
				}
			}
		}
		if in.OnCreate != nil {
			if err := in.OnCreate(ctx, tx, q, issue); err != nil {
				return err
			}
		}
		return s.emit(ctx, tx, actor, p, &issue, "issue.created", map[string]any{"type": it.Key, "status": initial.Name})
	})
	return issue, p, err
}

func ptr[T any](v T) *T { return &v }

func (s *Service) setReleases(ctx context.Context, q *dbq.Queries, p dbq.Project, issueID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	rels, err := q.GetReleasesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	unique := map[uuid.UUID]bool{}
	for _, id := range ids {
		unique[id] = true
	}
	if len(rels) != len(unique) {
		return apperr.Invalid("releaseIds", "unknown release")
	}
	for _, r := range rels {
		if r.ProjectID != p.ID {
			return apperr.Invalid("releaseIds", "release %s belongs to another project", r.Name)
		}
		if err := q.AddIssueRelease(ctx, dbq.AddIssueReleaseParams{IssueID: issueID, ReleaseID: r.ID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) UpdateIssue(ctx context.Context, actor *authz.Actor, ref string, in IssuePatch) (dbq.Issue, dbq.Project, error) {
	cur, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return cur, p, err
	}
	onlyAssignee := in.TypeKey == nil && in.Summary == nil && in.Description == nil && in.Priority == nil && !in.ReporterID.Set &&
		!in.ParentKey.Set && !in.SprintID.Set && !in.StoryPoints.Set && !in.StartDate.Set && !in.DueDate.Set &&
		in.Labels == nil && in.ReleaseIDs == nil && len(in.Fields) == 0 && !in.OriginalEstimate.Set && !in.RemainingEstimate.Set
	if !onlyAssignee && !canEdit(access, actor, cur) {
		return cur, p, apperr.Forbidden("you cannot edit %s", issueKey(p, cur))
	}
	if in.AssigneeID.Set {
		if err := requireAssignPermission(access, actor, in.AssigneeID.Value); err != nil {
			return cur, p, err
		}
	}
	var out dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		i, err := q.GetIssueForUpdate(ctx, cur.ID)
		if err != nil {
			return err
		}
		if in.Version != nil && int32(*in.Version) != i.Version {
			return apperr.Conflict("version_conflict", "%s was changed by someone else; reload and try again", issueKey(p, i))
		}
		upd := updateFromIssue(i)
		var changes []change
		var changedFields []string
		record := func(field string, from, to, fromRef, toRef *string) {
			changes = append(changes, change{field, from, to, fromRef, toRef})
			changedFields = append(changedFields, field)
		}

		curType, err := q.GetIssueType(ctx, i.TypeID)
		if err != nil {
			return err
		}
		newType := curType
		if in.TypeKey != nil && *in.TypeKey != curType.Key {
			newType, err = q.GetIssueTypeByKey(ctx, dbq.GetIssueTypeByKeyParams{ProjectID: p.ID, Key: *in.TypeKey})
			if err != nil {
				return apperr.Invalid("typeKey", "unknown issue type %q", *in.TypeKey)
			}
			if newType.Level != curType.Level {
				n, err := q.CountChildren(ctx, &i.ID)
				if err != nil {
					return err
				}
				if n > 0 {
					return apperr.Invalid("typeKey", "cannot change the hierarchy level of an issue that has children")
				}
			}
			upd.TypeID = newType.ID
			record("type", &curType.Name, &newType.Name, nil, nil)
			if newType.WorkflowID != curType.WorkflowID {
				// Keep the status if the new workflow has it, otherwise start over.
				def, wv, err := currentVersion(ctx, q, newType.WorkflowID)
				if err != nil {
					return err
				}
				upd.WorkflowVersionID = wv.ID
				if !def.HasStatus(i.StatusID) {
					from, _ := q.GetStatusesByIDs(ctx, []uuid.UUID{i.StatusID})
					to := def.Initial()
					upd.StatusID = to.ID
					fromRef, toRef := i.StatusID.String(), to.ID.String()
					var fromName *string
					if len(from) > 0 {
						fromName = &from[0].Name
					}
					record("status", fromName, &to.Name, &fromRef, &toRef)
					applyResolution(&upd, to.Category, nil, s.Now())
				}
			}
		}

		parentChanged := in.ParentKey.Set
		if parentChanged || newType.ID != curType.ID {
			parentKey := ""
			if in.ParentKey.Set {
				if in.ParentKey.Value != nil {
					parentKey = *in.ParentKey.Value
				}
			} else if i.ParentID != nil {
				parentKey = i.ParentID.String()
			}
			parent, err := s.resolveParent(ctx, q, p, newType, parentKey)
			if err != nil {
				return err
			}
			var newParent *uuid.UUID
			if parent != nil {
				newParent = &parent.ID
				if parent.ID == i.ID {
					return apperr.Invalid("parentKey", "an issue cannot be its own parent")
				}
			}
			if !uuidPtrEqual(newParent, i.ParentID) {
				refs, _ := q.GetIssuesByIDs(ctx, compactIDs(i.ParentID, newParent))
				label := func(id *uuid.UUID) *string {
					for _, r := range refs {
						if id != nil && r.ID == *id {
							k := issueKey(p, r)
							return &k
						}
					}
					return nil
				}
				record("parent", label(i.ParentID), label(newParent), idRef(i.ParentID), idRef(newParent))
				upd.ParentID = newParent
			}
			if newType.Level < 0 && parent != nil {
				upd.SprintID = parent.SprintID
			}
		}

		if in.Summary != nil {
			v := strings.TrimSpace(*in.Summary)
			if v == "" || len(v) > 255 {
				return apperr.Invalid("summary", "summary must be 1-255 characters")
			}
			if v != i.Summary {
				record("summary", &i.Summary, &v, nil, nil)
				upd.Summary = v
			}
		}
		if in.Description != nil && *in.Description != i.Description {
			record("description", nil, nil, nil, nil)
			upd.Description = *in.Description
		}
		if in.Priority != nil && *in.Priority != i.Priority {
			if !slices.Contains(priorities, *in.Priority) {
				return apperr.Invalid("priority", "unknown priority %q", *in.Priority)
			}
			record("priority", &i.Priority, in.Priority, nil, nil)
			upd.Priority = *in.Priority
		}
		if in.AssigneeID.Set && !uuidPtrEqual(in.AssigneeID.Value, i.AssigneeID) {
			var toName *string
			if in.AssigneeID.Value != nil {
				u, err := s.assignable(ctx, q, p, *in.AssigneeID.Value)
				if err != nil {
					return err
				}
				toName = &u.DisplayName
				if err := q.AddWatcher(ctx, dbq.AddWatcherParams{IssueID: i.ID, UserID: u.ID}); err != nil {
					return err
				}
			}
			record("assignee", userName(ctx, q, i.AssigneeID), toName, idRef(i.AssigneeID), idRef(in.AssigneeID.Value))
			upd.AssigneeID = in.AssigneeID.Value
		}
		if in.ReporterID.Set && !uuidPtrEqual(in.ReporterID.Value, i.ReporterID) {
			if in.ReporterID.Value != nil {
				if _, err := q.GetUser(ctx, *in.ReporterID.Value); err != nil {
					return apperr.Invalid("reporterId", "user not found")
				}
			}
			record("reporter", userName(ctx, q, i.ReporterID), userName(ctx, q, in.ReporterID.Value), idRef(i.ReporterID), idRef(in.ReporterID.Value))
			upd.ReporterID = in.ReporterID.Value
		}
		if in.SprintID.Set {
			switch {
			case newType.Level > 0 && in.SprintID.Value != nil:
				return apperr.Invalid("sprintId", "%s issues cannot be added to a sprint", newType.Name)
			case newType.Level < 0:
				if !uuidPtrEqual(in.SprintID.Value, upd.SprintID) {
					return apperr.Invalid("sprintId", "sub-tasks follow their parent's sprint")
				}
			}
			if in.SprintID.Value != nil && newType.Level == 0 {
				if _, err := s.sprintFor(ctx, q, p, *in.SprintID.Value); err != nil {
					return err
				}
			}
			if newType.Level == 0 {
				upd.SprintID = in.SprintID.Value
			}
		}
		if newType.Level > 0 {
			upd.SprintID = nil
		}
		if !uuidPtrEqual(upd.SprintID, i.SprintID) {
			record("sprint", sprintName(ctx, q, i.SprintID), sprintName(ctx, q, upd.SprintID), idRef(i.SprintID), idRef(upd.SprintID))
		}
		if in.StoryPoints.Set && !floatPtrEqual(in.StoryPoints.Value, i.StoryPoints) {
			if v := in.StoryPoints.Value; v != nil && (*v < 0 || *v > 1000) {
				return apperr.Invalid("storyPoints", "story points must be between 0 and 1000")
			}
			record("storyPoints", fmtFloat(i.StoryPoints), fmtFloat(in.StoryPoints.Value), nil, nil)
			upd.StoryPoints = in.StoryPoints.Value
		}
		if in.StartDate.Set && !datePtrEqual(in.StartDate.Value, i.StartDate) {
			record("startDate", fmtDate(i.StartDate), fmtDate(in.StartDate.Value), nil, nil)
			upd.StartDate = in.StartDate.Value
		}
		if in.DueDate.Set && !datePtrEqual(in.DueDate.Value, i.DueDate) {
			record("dueDate", fmtDate(i.DueDate), fmtDate(in.DueDate.Value), nil, nil)
			upd.DueDate = in.DueDate.Value
		}
		if in.Labels != nil {
			labels, err := normalizeLabels(*in.Labels)
			if err != nil {
				return err
			}
			if strings.Join(labels, ",") != strings.Join(i.Labels, ",") {
				record("labels", strp(strings.Join(i.Labels, ", ")), strp(strings.Join(labels, ", ")), nil, nil)
				upd.Labels = labels
			}
		}
		if len(in.Fields) > 0 {
			fields, fieldChanges, err := s.mergeFields(ctx, q, p, newType, i.Fields, in.Fields)
			if err != nil {
				return err
			}
			for _, c := range fieldChanges {
				record(c.field, c.from, c.to, nil, nil)
			}
			upd.Fields = fields
		}
		if in.OriginalEstimate.Set && !int64PtrEqual(in.OriginalEstimate.Value, i.OriginalEstimateSeconds) {
			if err := checkEstimate("originalEstimate", in.OriginalEstimate.Value); err != nil {
				return err
			}
			c := s.estimateChange("originalEstimate", i.OriginalEstimateSeconds, in.OriginalEstimate.Value)
			record(c.field, c.from, c.to, c.fromRef, c.toRef)
			upd.OriginalEstimateSeconds = in.OriginalEstimate.Value
			if !in.RemainingEstimate.Set && i.RemainingEstimateSeconds == nil && in.OriginalEstimate.Value != nil {
				in.RemainingEstimate = Some(*in.OriginalEstimate.Value)
			}
		}
		if in.RemainingEstimate.Set && !int64PtrEqual(in.RemainingEstimate.Value, i.RemainingEstimateSeconds) {
			if err := checkEstimate("remainingEstimate", in.RemainingEstimate.Value); err != nil {
				return err
			}
			c := s.estimateChange("remainingEstimate", i.RemainingEstimateSeconds, in.RemainingEstimate.Value)
			record(c.field, c.from, c.to, c.fromRef, c.toRef)
			upd.RemainingEstimateSeconds = in.RemainingEstimate.Value
		}
		if in.ReleaseIDs != nil {
			before, err := q.IssueReleases(ctx, i.ID)
			if err != nil {
				return err
			}
			if err := q.ClearIssueReleases(ctx, i.ID); err != nil {
				return err
			}
			if err := s.setReleases(ctx, q, p, i.ID, *in.ReleaseIDs); err != nil {
				return err
			}
			after, err := q.IssueReleases(ctx, i.ID)
			if err != nil {
				return err
			}
			if releaseNames(before) != releaseNames(after) {
				record("fixVersions", strp(releaseNames(before)), strp(releaseNames(after)), nil, nil)
			}
		}

		if len(changes) == 0 {
			out = i
			return nil
		}
		out, err = q.UpdateIssue(ctx, upd)
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.Conflict("version_conflict", "%s was changed concurrently; reload and try again", issueKey(p, i))
			}
			return err
		}
		if !uuidPtrEqual(out.SprintID, i.SprintID) && newType.Level == 0 {
			if err := q.SetChildrenSprint(ctx, dbq.SetChildrenSprintParams{ParentID: &i.ID, SprintID: out.SprintID}); err != nil {
				return err
			}
		}
		for _, c := range changes {
			if err := s.history(ctx, q, i.ID, actor, c.field, c.from, c.to, c.fromRef, c.toRef); err != nil {
				return err
			}
		}
		data := map[string]any{"fields": changedFields}
		if slices.Contains(changedFields, "assignee") {
			data["assigneeId"] = out.AssigneeID
		}
		return s.emit(ctx, tx, actor, p, &out, "issue.updated", data)
	})
	return out, p, err
}

type change struct {
	field          string
	from, to       *string
	fromRef, toRef *string
}

func updateFromIssue(i dbq.Issue) dbq.UpdateIssueParams {
	return dbq.UpdateIssueParams{
		ID: i.ID, Version: i.Version, TypeID: i.TypeID, ParentID: i.ParentID, Summary: i.Summary, Description: i.Description,
		StatusID: i.StatusID, WorkflowVersionID: i.WorkflowVersionID, Priority: i.Priority, AssigneeID: i.AssigneeID,
		ReporterID: i.ReporterID, SprintID: i.SprintID, StoryPoints: i.StoryPoints, StartDate: i.StartDate, DueDate: i.DueDate,
		Labels: i.Labels, Resolution: i.Resolution, ResolvedAt: i.ResolvedAt, Fields: i.Fields, Rank: i.Rank,
		OriginalEstimateSeconds: i.OriginalEstimateSeconds, RemainingEstimateSeconds: i.RemainingEstimateSeconds,
	}
}

// applyResolution keeps resolution consistent with the status category: entering done sets
// it (default "Done"), leaving done clears it.
func applyResolution(u *dbq.UpdateIssueParams, category string, chosen *string, now time.Time) {
	if category == "done" {
		switch {
		case chosen != nil:
			u.Resolution = chosen
		case u.Resolution == nil:
			u.Resolution = strp("Done")
		}
		if u.ResolvedAt == nil {
			u.ResolvedAt = &now
		}
		return
	}
	u.Resolution = nil
	u.ResolvedAt = nil
}

func (s *Service) DeleteIssue(ctx context.Context, actor *authz.Actor, ref string) error {
	i, p, _, err := s.IssueFor(ctx, actor, ref, authz.IssueDelete)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteSubtasks(ctx, &i.ID); err != nil {
			return err
		}
		if err := q.DeleteIssue(ctx, i.ID); err != nil {
			return err
		}
		Audit(ctx, q, actor, "issue.delete", "issue", issueKey(p, i), &p.ID, map[string]any{"summary": i.Summary})
		ev := i // the row is gone; the job only needs the key and summary from data
		return s.emit(ctx, tx, actor, p, &ev, "issue.deleted", nil)
	})
}

// ---------------------------------------------------------------------------
// Custom fields

type fieldChange struct {
	field    string
	from, to *string
}

func fieldApplies(f dbq.FieldDefinition, p dbq.Project, t dbq.IssueType) bool {
	if f.ProjectIds != nil && !slices.Contains(f.ProjectIds, p.ID) {
		return false
	}
	if f.IssueTypeKeys != nil && !slices.Contains(f.IssueTypeKeys, t.Key) {
		return false
	}
	return true
}

func fieldOptions(f dbq.FieldDefinition) []string {
	var opts []string
	_ = json.Unmarshal(f.Options, &opts)
	return opts
}

// coerceField validates and normalises a custom field value for storage.
func (s *Service) coerceField(ctx context.Context, q *dbq.Queries, p dbq.Project, f dbq.FieldDefinition, v any) (any, error) {
	bad := func(format string, args ...any) error {
		return apperr.Invalid("fields."+f.Key, "%s: %s", f.Name, fmt.Sprintf(format, args...))
	}
	switch f.Type {
	case "text":
		str, ok := v.(string)
		if !ok {
			return nil, bad("expected text")
		}
		if len(str) > 10000 {
			return nil, bad("too long")
		}
		return str, nil
	case "url":
		str, ok := v.(string)
		u, err := url.Parse(str)
		if !ok || err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, bad("expected an http(s) URL")
		}
		return str, nil
	case "number":
		switch n := v.(type) {
		case float64:
			return n, nil
		case string:
			f, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return nil, bad("expected a number")
			}
			return f, nil
		}
		return nil, bad("expected a number")
	case "checkbox":
		b, ok := v.(bool)
		if !ok {
			return nil, bad("expected true or false")
		}
		return b, nil
	case "date":
		str, ok := v.(string)
		if _, err := time.Parse("2006-01-02", str); !ok || err != nil {
			return nil, bad("expected a date (YYYY-MM-DD)")
		}
		return str, nil
	case "select":
		str, ok := v.(string)
		if !ok || !slices.Contains(fieldOptions(f), str) {
			return nil, bad("must be one of %s", strings.Join(fieldOptions(f), ", "))
		}
		return str, nil
	case "multiselect":
		arr, ok := v.([]any)
		if !ok {
			return nil, bad("expected a list")
		}
		var out []string
		for _, x := range arr {
			str, ok := x.(string)
			if !ok || !slices.Contains(fieldOptions(f), str) {
				return nil, bad("values must be among %s", strings.Join(fieldOptions(f), ", "))
			}
			if !slices.Contains(out, str) {
				out = append(out, str)
			}
		}
		return out, nil
	case "user":
		str, _ := v.(string)
		id, err := uuid.Parse(str)
		if err != nil {
			return nil, bad("expected a user id")
		}
		if _, err := s.assignable(ctx, q, p, id); err != nil {
			return nil, bad("user cannot access the project")
		}
		return id.String(), nil
	case "version":
		str, _ := v.(string)
		id, err := uuid.Parse(str)
		if err != nil {
			return nil, bad("expected a release id")
		}
		r, err := q.GetRelease(ctx, id)
		if err != nil || r.ProjectID != p.ID {
			return nil, bad("release not found in %s", p.Key)
		}
		return id.String(), nil
	}
	return nil, bad("unsupported field type %s", f.Type)
}

func (s *Service) validateFields(ctx context.Context, q *dbq.Queries, p dbq.Project, t dbq.IssueType, existing json.RawMessage, in map[string]any) (json.RawMessage, error) {
	fields, _, err := s.mergeFields(ctx, q, p, t, existing, in)
	return fields, err
}

func (s *Service) mergeFields(ctx context.Context, q *dbq.Queries, p dbq.Project, t dbq.IssueType, existing json.RawMessage, in map[string]any) (json.RawMessage, []fieldChange, error) {
	cur := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cur); err != nil {
			return nil, nil, err
		}
	}
	if len(in) == 0 {
		b, err := json.Marshal(cur)
		return b, nil, err
	}
	defs, err := q.ListFieldDefinitions(ctx)
	if err != nil {
		return nil, nil, err
	}
	byKey := map[string]dbq.FieldDefinition{}
	for _, d := range defs {
		byKey[d.Key] = d
	}
	var changes []fieldChange
	for k, v := range in {
		d, ok := byKey[k]
		if !ok || !fieldApplies(d, p, t) {
			return nil, nil, apperr.Invalid("fields."+k, "unknown field %q for %s issues in %s", k, t.Name, p.Key)
		}
		before := display(cur[k])
		if v == nil {
			delete(cur, k)
		} else {
			cv, err := s.coerceField(ctx, q, p, d, v)
			if err != nil {
				return nil, nil, err
			}
			cur[k] = cv
		}
		if after := display(cur[k]); !strPtrEqual(before, after) {
			changes = append(changes, fieldChange{d.Name, before, after})
		}
	}
	b, err := json.Marshal(cur)
	return b, changes, err
}

func display(v any) *string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return strp(x)
	case []string:
		return strp(strings.Join(x, ", "))
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = fmt.Sprint(e)
		}
		return strp(strings.Join(parts, ", "))
	default:
		return strp(fmt.Sprint(x))
	}
}

// ---------------------------------------------------------------------------
// Small helpers

func uuidPtrEqual(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func floatPtrEqual(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func strPtrEqual(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func datePtrEqual(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Format("2006-01-02") == b.Format("2006-01-02"))
}

func compactIDs(ids ...*uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, id := range ids {
		if id != nil {
			out = append(out, *id)
		}
	}
	return out
}

func idRef(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func fmtFloat(f *float64) *string {
	if f == nil {
		return nil
	}
	return strp(strconv.FormatFloat(*f, 'f', -1, 64))
}

func fmtDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return strp(t.Format("2006-01-02"))
}

func userName(ctx context.Context, q *dbq.Queries, id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	u, err := q.GetUser(ctx, *id)
	if err != nil {
		return nil
	}
	return &u.DisplayName
}

func sprintName(ctx context.Context, q *dbq.Queries, id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	sp, err := q.GetSprint(ctx, *id)
	if err != nil {
		return nil
	}
	return &sp.Name
}

func releaseNames(rs []dbq.Release) string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

// rankBetween computes a rank to place an issue after `after` or before `before`, using the
// globally adjacent rank so keys stay unique across the project.
func (s *Service) rankBetween(ctx context.Context, q *dbq.Queries, projectID, self uuid.UUID, after, before *dbq.Issue) (string, error) {
	if after != nil {
		next, err := q.NextRanksAfter(ctx, dbq.NextRanksAfterParams{ProjectID: projectID, Rank: after.Rank})
		if err != nil {
			return "", err
		}
		upper := ""
		for _, n := range next {
			if n.ID != self {
				upper = n.Rank
				break
			}
		}
		return rank.Between(after.Rank, upper)
	}
	prev, err := q.PrevRanksBefore(ctx, dbq.PrevRanksBeforeParams{ProjectID: projectID, Rank: before.Rank})
	if err != nil {
		return "", err
	}
	lower := ""
	for _, n := range prev {
		if n.ID != self {
			lower = n.Rank
			break
		}
	}
	return rank.Between(lower, before.Rank)
}
