package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/workflow"
)

// problemsError converts workflow validation problems into a 422 with field errors.
func problemsError(probs workflow.Problems) error {
	e := apperr.New(http.StatusUnprocessableEntity, "invalid_workflow", "The workflow definition is invalid")
	for _, p := range probs {
		e.Fields = append(e.Fields, apperr.FieldError{Field: p.Path, Message: p.Message})
	}
	return e
}

// parseAndValidate parses YAML and checks it, returning the document.
func (s *Service) parseAndValidate(src string) (*workflow.Document, error) {
	doc, err := workflow.Parse([]byte(src))
	if err != nil {
		if probs, ok := err.(workflow.Problems); ok {
			return nil, problemsError(probs)
		}
		return nil, err
	}
	if probs := workflow.Validate(doc, s.Eval); len(probs) > 0 {
		return nil, problemsError(probs)
	}
	return doc, nil
}

// resolveStatuses finds or creates the global statuses a document uses. Statuses are shared by
// name across workflows, so a name cannot change category.
func resolveStatuses(ctx context.Context, q *dbq.Queries, doc *workflow.Document, create bool) (map[string]uuid.UUID, workflow.Problems) {
	ids := map[string]uuid.UUID{}
	var probs workflow.Problems
	for i, sd := range doc.Statuses {
		name := strings.TrimSpace(sd.Name)
		st, err := q.GetStatusByName(ctx, name)
		switch {
		case err == nil:
			if st.Category != sd.Category {
				probs = append(probs, workflow.Problem{
					Path:    fmt.Sprintf("statuses[%d].category", i),
					Message: fmt.Sprintf("status %q already exists with category %s; statuses are shared across workflows", st.Name, st.Category),
				})
			}
			ids[strings.ToLower(name)] = st.ID
		case db.IsNoRows(err):
			if !create {
				ids[strings.ToLower(name)] = uuid.New() // placeholder for validation previews
				continue
			}
			st, err = q.CreateStatus(ctx, dbq.CreateStatusParams{Name: name, Category: sd.Category})
			if err != nil {
				probs = append(probs, workflow.Problem{Path: fmt.Sprintf("statuses[%d]", i), Message: err.Error()})
				continue
			}
			ids[strings.ToLower(name)] = st.ID
		default:
			probs = append(probs, workflow.Problem{Path: fmt.Sprintf("statuses[%d]", i), Message: err.Error()})
		}
	}
	return ids, probs
}

// canManageWorkflow: project workflows need project.admin, shared workflows need org admin.
func (s *Service) canManageWorkflow(ctx context.Context, actor *authz.Actor, wf dbq.Workflow) error {
	if wf.ProjectID == nil {
		if !actor.IsOrgAdmin() && !actor.Privileged() {
			return apperr.Forbidden("only organisation admins can change shared workflows")
		}
		return nil
	}
	p, err := s.Q.GetProject(ctx, *wf.ProjectID)
	if err != nil {
		return err
	}
	_, err = s.requireAccess(ctx, actor, p, authz.ProjectAdmin)
	return err
}

func (s *Service) canViewWorkflow(ctx context.Context, actor *authz.Actor, wf dbq.Workflow) error {
	if wf.ProjectID == nil {
		return nil
	}
	p, err := s.Q.GetProject(ctx, *wf.ProjectID)
	if err != nil {
		return err
	}
	_, err = s.requireAccess(ctx, actor, p, authz.ProjectBrowse)
	if apperr.IsNotFound(err) {
		return apperr.NotFound("workflow")
	}
	return err
}

type PublishResult struct {
	Workflow        dbq.Workflow
	Version         dbq.WorkflowVersion
	MigrationQueued bool
	Unchanged       bool
}

// publishTx publishes a new version of wf (locked by the caller). mapping maps the names of
// statuses being removed to statuses in the new version.
func (s *Service) publishTx(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, wf dbq.Workflow, doc *workflow.Document, src string, mapping map[string]string, skipIfUnchanged bool) (PublishResult, error) {
	ids, probs := resolveStatuses(ctx, q, doc, true)
	if len(probs) > 0 {
		return PublishResult{}, problemsError(probs)
	}
	def, err := workflow.Compile(doc, ids)
	if err != nil {
		return PublishResult{}, err
	}
	defJSON, err := json.Marshal(def)
	if err != nil {
		return PublishResult{}, err
	}

	if wf.CurrentVersion > 0 && skipIfUnchanged {
		cur, err := q.GetWorkflowVersion(ctx, dbq.GetWorkflowVersionParams{WorkflowID: wf.ID, Version: wf.CurrentVersion})
		if err == nil && jsonEqual(cur.Definition, defJSON) {
			return PublishResult{Workflow: wf, Version: cur, Unchanged: true}, nil
		}
	}

	// Statuses that issues are in but the new version drops must be mapped.
	usage, err := q.WorkflowStatusUsage(ctx, wf.ID)
	if err != nil {
		return PublishResult{}, err
	}
	statusMap := map[string]string{}
	missing := apperr.New(http.StatusUnprocessableEntity, "status_mapping_required", "Some issues are in statuses this version removes; map them to new statuses")
	for _, u := range usage {
		if def.HasStatus(u.StatusID) {
			continue
		}
		old, err := q.GetStatusesByIDs(ctx, []uuid.UUID{u.StatusID})
		if err != nil || len(old) == 0 {
			return PublishResult{}, fmt.Errorf("load status %s: %w", u.StatusID, err)
		}
		target := lookupMapping(mapping, old[0].Name)
		ts, ok := def.StatusByName(target)
		if target == "" || !ok {
			missing.Fields = append(missing.Fields, apperr.FieldError{
				Field:   old[0].Name,
				Message: fmt.Sprintf("%d issue(s) are in %q, which this version removes", u.N, old[0].Name),
			})
			continue
		}
		statusMap[u.StatusID.String()] = ts.ID.String()
	}
	if len(missing.Fields) > 0 {
		return PublishResult{}, missing
	}

	if doc.Name != wf.Name {
		return PublishResult{}, apperr.Invalid("name", "the document name %q does not match workflow %q", doc.Name, wf.Name)
	}
	next := wf.CurrentVersion + 1
	wv, err := q.CreateWorkflowVersion(ctx, dbq.CreateWorkflowVersionParams{
		WorkflowID: wf.ID, Version: next, Definition: defJSON, SourceYaml: src, PublishedBy: actor.UserID(),
	})
	if err != nil {
		return PublishResult{}, err
	}
	if err := q.SetWorkflowVersion(ctx, dbq.SetWorkflowVersionParams{ID: wf.ID, CurrentVersion: next, Description: doc.Description}); err != nil {
		return PublishResult{}, err
	}
	wf.CurrentVersion = next
	wf.Description = doc.Description

	res := PublishResult{Workflow: wf, Version: wv}
	if len(usage) > 0 && next > 1 && s.Jobs != nil {
		if _, err := s.Jobs.InsertTx(ctx, tx, jobs.WorkflowMigrate{WorkflowID: wf.ID, Mapping: statusMap}, nil); err != nil {
			return res, err
		}
		res.MigrationQueued = true
	}
	Audit(ctx, q, actor, "workflow.publish", "workflow", wf.ID.String(), wf.ProjectID, map[string]any{"name": wf.Name, "version": next})
	return res, nil
}

func lookupMapping(mapping map[string]string, name string) string {
	for k, v := range mapping {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ab, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return bytes.Equal(ab, bb)
}

// createWorkflowTx creates a workflow and publishes version 1.
func (s *Service) createWorkflowTx(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, projectID *uuid.UUID, src string) (PublishResult, error) {
	doc, err := s.parseAndValidate(src)
	if err != nil {
		return PublishResult{}, err
	}
	wf, err := q.CreateWorkflow(ctx, dbq.CreateWorkflowParams{ProjectID: projectID, Name: strings.TrimSpace(doc.Name), Description: doc.Description})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return PublishResult{}, apperr.Conflict("duplicate_workflow", "a workflow named %q already exists", doc.Name)
		}
		return PublishResult{}, err
	}
	doc.Name = wf.Name
	return s.publishTx(ctx, tx, q, actor, wf, doc, src, nil, false)
}

func (s *Service) CreateWorkflow(ctx context.Context, actor *authz.Actor, projectKey *string, src string) (PublishResult, error) {
	var projectID *uuid.UUID
	if projectKey != nil && *projectKey != "" {
		p, _, err := s.ProjectFor(ctx, actor, *projectKey, authz.ProjectAdmin)
		if err != nil {
			return PublishResult{}, err
		}
		projectID = &p.ID
	} else if !actor.IsOrgAdmin() {
		return PublishResult{}, apperr.Forbidden("only organisation admins can create shared workflows")
	}
	var res PublishResult
	err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		res, err = s.createWorkflowTx(ctx, tx, q, actor, projectID, src)
		return err
	})
	return res, err
}

func (s *Service) PublishWorkflow(ctx context.Context, actor *authz.Actor, id uuid.UUID, src string, mapping map[string]string) (PublishResult, error) {
	wf, err := s.Q.GetWorkflow(ctx, id)
	if err != nil {
		return PublishResult{}, apperr.NotFound("workflow")
	}
	if err := s.canManageWorkflow(ctx, actor, wf); err != nil {
		return PublishResult{}, err
	}
	doc, err := s.parseAndValidate(src)
	if err != nil {
		return PublishResult{}, err
	}
	var res PublishResult
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		locked, err := q.GetWorkflowForUpdate(ctx, id)
		if err != nil {
			return err
		}
		res, err = s.publishTx(ctx, tx, q, actor, locked, doc, src, mapping, false)
		return err
	})
	return res, err
}

// ApplyWorkflow is the GitOps entry point: create, publish if changed, or report unchanged.
func (s *Service) ApplyWorkflow(ctx context.Context, actor *authz.Actor, projectKey *string, src string, mapping map[string]string, dryRun bool) (PublishResult, string, error) {
	doc, err := s.parseAndValidate(src)
	if err != nil {
		return PublishResult{}, "", err
	}
	var projectID *uuid.UUID
	if projectKey != nil && *projectKey != "" {
		p, _, err := s.ProjectFor(ctx, actor, *projectKey, authz.ProjectAdmin)
		if err != nil {
			return PublishResult{}, "", err
		}
		projectID = &p.ID
	} else if !actor.IsOrgAdmin() {
		return PublishResult{}, "", apperr.Forbidden("only organisation admins can apply shared workflows")
	}
	var res PublishResult
	action := ""
	errDryRun := fmt.Errorf("dry run")
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		wf, err := q.GetWorkflowByName(ctx, dbq.GetWorkflowByNameParams{ProjectID: projectID, Name: doc.Name})
		switch {
		case db.IsNoRows(err):
			res, err = s.createWorkflowTx(ctx, tx, q, actor, projectID, src)
			action = "created"
		case err != nil:
			return err
		default:
			wf, err = q.GetWorkflowForUpdate(ctx, wf.ID)
			if err != nil {
				return err
			}
			doc.Name = wf.Name
			res, err = s.publishTx(ctx, tx, q, actor, wf, doc, src, mapping, true)
			action = "published"
			if res.Unchanged {
				action = "unchanged"
			}
		}
		if err != nil {
			return err
		}
		if dryRun {
			return errDryRun // roll back, but report what would happen
		}
		return nil
	})
	if err == errDryRun {
		err = nil
	}
	return res, action, err
}

// ValidationResult is returned by the editor's live validation.
type ValidationResult struct {
	Problems   workflow.Problems
	Definition *workflow.Definition
	Removed    []RemovedStatus
}

type RemovedStatus struct {
	Name       string
	IssueCount int
}

func (s *Service) ValidateWorkflow(ctx context.Context, actor *authz.Actor, src string, workflowID *uuid.UUID) (ValidationResult, error) {
	doc, err := workflow.Parse([]byte(src))
	if err != nil {
		if probs, ok := err.(workflow.Problems); ok {
			return ValidationResult{Problems: probs}, nil
		}
		return ValidationResult{}, err
	}
	probs := workflow.Validate(doc, s.Eval)
	ids, statusProbs := resolveStatuses(ctx, s.Q, doc, false)
	probs = append(probs, statusProbs...)
	res := ValidationResult{Problems: probs}
	if len(probs) > 0 {
		return res, nil
	}
	def, err := workflow.Compile(doc, ids)
	if err != nil {
		return res, err
	}
	res.Definition = def
	if workflowID != nil {
		wf, err := s.Q.GetWorkflow(ctx, *workflowID)
		if err != nil {
			return res, apperr.NotFound("workflow")
		}
		if err := s.canViewWorkflow(ctx, actor, wf); err != nil {
			return res, err
		}
		usage, err := s.Q.WorkflowStatusUsage(ctx, wf.ID)
		if err != nil {
			return res, err
		}
		for _, u := range usage {
			if def.HasStatus(u.StatusID) {
				continue
			}
			sts, _ := s.Q.GetStatusesByIDs(ctx, []uuid.UUID{u.StatusID})
			if len(sts) > 0 {
				res.Removed = append(res.Removed, RemovedStatus{Name: sts[0].Name, IssueCount: int(u.N)})
			}
		}
		sort.Slice(res.Removed, func(i, j int) bool { return res.Removed[i].Name < res.Removed[j].Name })
	}
	return res, nil
}

func (s *Service) GetWorkflow(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Workflow, error) {
	wf, err := s.Q.GetWorkflow(ctx, id)
	if err != nil {
		return wf, apperr.NotFound("workflow")
	}
	return wf, s.canViewWorkflow(ctx, actor, wf)
}

func (s *Service) DeleteWorkflow(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	wf, err := s.Q.GetWorkflow(ctx, id)
	if err != nil {
		return apperr.NotFound("workflow")
	}
	if err := s.canManageWorkflow(ctx, actor, wf); err != nil {
		return err
	}
	usage, err := s.Q.WorkflowUsage(ctx, id)
	if err != nil {
		return err
	}
	if len(usage) > 0 {
		return apperr.Conflict("workflow_in_use", "the workflow is used by %d issue type(s); map them to another workflow first", len(usage))
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteWorkflow(ctx, id); err != nil {
			if db.IsForeignKeyViolation(err) {
				return apperr.Conflict("workflow_in_use", "issues are still pinned to this workflow")
			}
			return err
		}
		Audit(ctx, q, actor, "workflow.delete", "workflow", id.String(), wf.ProjectID, map[string]any{"name": wf.Name})
		return nil
	})
}

// MigrateWorkflowIssues moves issues onto the workflow's current version in batches.
// It runs as a worker job and is idempotent.
func (s *Service) MigrateWorkflowIssues(ctx context.Context, workflowID uuid.UUID, mapping map[string]string) (int, error) {
	def, wv, err := currentVersion(ctx, s.Q, workflowID)
	if err != nil {
		return 0, err
	}
	actor := authz.SystemActor()
	touched := map[uuid.UUID]bool{}
	total := 0
	for {
		n := 0
		err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
			batch, err := q.IssuesToMigrate(ctx, dbq.IssuesToMigrateParams{WorkflowID: workflowID, VersionID: wv.ID, Lim: 200})
			if err != nil {
				return err
			}
			n = len(batch)
			for _, it := range batch {
				target := it.StatusID
				if !def.HasStatus(it.StatusID) {
					if mapped, ok := mapping[it.StatusID.String()]; ok {
						target = uuid.MustParse(mapped)
					} else {
						target = def.Initial().ID
					}
				}
				if err := q.RepinIssue(ctx, dbq.RepinIssueParams{ID: it.ID, WorkflowVersionID: wv.ID, StatusID: target}); err != nil {
					return err
				}
				if target != it.StatusID {
					sts, _ := q.GetStatusesByIDs(ctx, []uuid.UUID{it.StatusID, target})
					names := map[uuid.UUID]string{}
					for _, st := range sts {
						names[st.ID] = st.Name
					}
					from, to := it.StatusID.String(), target.String()
					if err := s.history(ctx, q, it.ID, actor, "status", strp(names[it.StatusID]), strp(names[target]), &from, &to); err != nil {
						return err
					}
				}
				issue, err := q.GetIssue(ctx, it.ID)
				if err == nil {
					touched[issue.ProjectID] = true
				}
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += n
		if n == 0 {
			break
		}
	}
	for projectID := range touched {
		p, err := s.Q.GetProject(ctx, projectID)
		if err != nil {
			continue
		}
		_ = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
			return s.emit(ctx, tx, actor, p, nil, "board.updated", map[string]any{"reason": "workflow_migration"})
		})
	}
	return total, nil
}
