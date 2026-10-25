// Package tracker is the issue-tracking core: projects, issue types, workflows, issues,
// boards, sprints, releases and search. Every mutation runs in one transaction that also
// writes history, publishes a live event and enqueues follow-up jobs (outbox).
package tracker

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/events"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/search"
	"github.com/geoffo-dev/workflow-management/internal/storage"
	"github.com/geoffo-dev/workflow-management/internal/workflow"
)

type Service struct {
	Pool    *pgxpool.Pool
	Q       *dbq.Queries
	Authz   *authz.Resolver
	Eval    *workflow.Evaluator
	Jobs    jobs.Enqueuer
	Storage storage.Store
	Cfg     *config.Config
	Log     *slog.Logger
	Now     func() time.Time

	// AfterCreateProject runs in the transaction that creates a project (the service desk sets
	// up service projects).
	AfterCreateProject func(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, p dbq.Project) error
	// OnEvent runs in the transaction of every change that emits a domain event, before the
	// event is queued (the service desk recomputes SLA clocks and queues customer emails).
	OnEvent func(ctx context.Context, tx pgx.Tx, actor *authz.Actor, p dbq.Project, issue *dbq.Issue, event string, data map[string]any) error
	// SLACalendars lists the calendars JQL's remaining() measures SLA clocks in.
	SLACalendars func(ctx context.Context) ([]search.SLACalendar, error)
}

func New(pool *pgxpool.Pool, eval *workflow.Evaluator, enq jobs.Enqueuer, store storage.Store, cfg *config.Config, log *slog.Logger) *Service {
	q := dbq.New(pool)
	return &Service{Pool: pool, Q: q, Authz: &authz.Resolver{Q: q}, Eval: eval, Jobs: enq, Storage: store, Cfg: cfg, Log: log, Now: time.Now}
}

func (s *Service) tx(ctx context.Context, fn func(tx pgx.Tx, q *dbq.Queries) error) error {
	return db.Tx(ctx, s.Pool, fn)
}

// ---------------------------------------------------------------------------
// Templates

//go:embed templates/*.yaml
var templateFS embed.FS

type Template struct {
	ID          string              `yaml:"id"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	BoardType   string              `yaml:"boardType"`
	Workflows   []yaml.Node         `yaml:"workflows"`
	IssueTypes  []TemplateIssueType `yaml:"issueTypes"`
	Boards      []TemplateBoard     `yaml:"boards"`
}

type TemplateIssueType struct {
	Key      string `yaml:"key"`
	Name     string `yaml:"name"`
	Level    int    `yaml:"level"`
	Icon     string `yaml:"icon"`
	Color    string `yaml:"color"`
	Workflow string `yaml:"workflow"`
}

type TemplateBoard struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Columns []struct {
		Name     string   `yaml:"name"`
		Statuses []string `yaml:"statuses"`
		WipLimit *int     `yaml:"wipLimit"`
	} `yaml:"columns"`
}

// WorkflowSources renders each embedded workflow back to YAML (the authored form we store).
func (t Template) WorkflowSources() ([]string, error) {
	var out []string
	for _, n := range t.Workflows {
		b, err := yaml.Marshal(&n)
		if err != nil {
			return nil, err
		}
		out = append(out, "kind: Workflow\n"+string(b))
	}
	return out, nil
}

var templates = func() []Template {
	var out []Template
	for _, id := range []string{"scrum", "kanban", "bug_tracking", "service"} {
		b, err := templateFS.ReadFile("templates/" + id + ".yaml")
		if err != nil {
			panic(err)
		}
		var t Template
		if err := yaml.Unmarshal(b, &t); err != nil {
			panic(fmt.Sprintf("template %s: %v", id, err))
		}
		out = append(out, t)
	}
	return out
}()

func Templates() []Template { return templates }

func templateByID(id string) (Template, bool) {
	for _, t := range templates {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// ---------------------------------------------------------------------------
// Lookups with permission checks

var issueKeyPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,9})-(\d+)$`)

// ResolveProject finds a project by key, previous key (alias) or id.
func (s *Service) ResolveProject(ctx context.Context, q *dbq.Queries, ref string) (dbq.Project, error) {
	if id, err := uuid.Parse(ref); err == nil {
		p, err := q.GetProject(ctx, id)
		if err != nil {
			return p, apperr.NotFound("project")
		}
		return p, nil
	}
	p, err := q.GetProjectByKey(ctx, ref)
	if err == nil {
		return p, nil
	}
	if !db.IsNoRows(err) {
		return p, err
	}
	p, err = q.GetProjectByAlias(ctx, ref)
	if err != nil {
		return p, apperr.NotFound("project")
	}
	return p, nil
}

// ResolveIssue finds an issue by key (old project keys still resolve) or id.
func (s *Service) ResolveIssue(ctx context.Context, q *dbq.Queries, ref string) (dbq.Issue, error) {
	if id, err := uuid.Parse(ref); err == nil {
		i, err := q.GetIssue(ctx, id)
		if err != nil {
			return i, apperr.NotFound("issue")
		}
		return i, nil
	}
	m := issueKeyPattern.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return dbq.Issue{}, apperr.NotFound("issue")
	}
	p, err := s.ResolveProject(ctx, q, m[1])
	if err != nil {
		return dbq.Issue{}, apperr.NotFound("issue")
	}
	n, _ := strconv.Atoi(m[2])
	i, err := q.GetIssueByNumber(ctx, dbq.GetIssueByNumberParams{ProjectID: p.ID, Number: int32(n)})
	if err != nil {
		return i, apperr.NotFound("issue")
	}
	return i, nil
}

// requireAccess checks a permission. Missing browse access reads as "not found" so private
// projects do not leak their existence.
func (s *Service) requireAccess(ctx context.Context, actor *authz.Actor, p dbq.Project, perm authz.Permission) (authz.Access, error) {
	access, err := s.Authz.Access(ctx, actor, p)
	if err != nil {
		return access, err
	}
	if !access.CanBrowse() {
		return access, apperr.NotFound("project")
	}
	if perm != "" && !access.Has(perm) {
		return access, apperr.Forbidden("you need the %s permission in %s", perm, p.Key)
	}
	return access, nil
}

func (s *Service) ProjectFor(ctx context.Context, actor *authz.Actor, ref string, perm authz.Permission) (dbq.Project, authz.Access, error) {
	p, err := s.ResolveProject(ctx, s.Q, ref)
	if err != nil {
		return p, authz.Access{}, err
	}
	access, err := s.requireAccess(ctx, actor, p, perm)
	return p, access, err
}

func (s *Service) IssueFor(ctx context.Context, actor *authz.Actor, ref string, perm authz.Permission) (dbq.Issue, dbq.Project, authz.Access, error) {
	i, err := s.ResolveIssue(ctx, s.Q, ref)
	if err != nil {
		return i, dbq.Project{}, authz.Access{}, err
	}
	p, err := s.Q.GetProject(ctx, i.ProjectID)
	if err != nil {
		return i, p, authz.Access{}, err
	}
	access, err := s.requireAccess(ctx, actor, p, perm)
	if err != nil && apperr.IsNotFound(err) {
		return i, p, access, apperr.NotFound("issue")
	}
	return i, p, access, err
}

// canEdit implements "edit your own issues" vs "edit others' issues".
func canEdit(access authz.Access, actor *authz.Actor, i dbq.Issue) bool {
	if access.Has(authz.IssueEditOthers) {
		return true
	}
	uid := actor.UserID()
	own := uid != nil && ((i.ReporterID != nil && *i.ReporterID == *uid) || (i.AssigneeID != nil && *i.AssigneeID == *uid))
	return own && access.Has(authz.IssueEdit)
}

// CanEdit reports whether an actor may edit an issue (their own with issue.edit, anyone's with
// issue.edit_others).
func CanEdit(access authz.Access, actor *authz.Actor, i dbq.Issue) bool {
	return canEdit(access, actor, i)
}

// ---------------------------------------------------------------------------
// History, events and audit

func actorType(a *authz.Actor) string { return string(a.Type) }

func (s *Service) history(ctx context.Context, q *dbq.Queries, issueID uuid.UUID, actor *authz.Actor, field string, from, to *string, fromRef, toRef *string) error {
	return q.InsertHistory(ctx, dbq.InsertHistoryParams{
		IssueID: issueID, ActorID: actor.UserID(), ActorType: actorType(actor), Field: field,
		FromValue: from, ToValue: to, FromRef: fromRef, ToRef: toRef, Via: via(actor),
	})
}

// via is what the actor used to act, for history and comments (nil when nothing special).
func via(a *authz.Actor) *string {
	if a == nil || a.Via == "" {
		return nil
	}
	return &a.Via
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// emit publishes a live update and enqueues the domain event (automation, webhooks, notifications)
// in the caller's transaction.
func (s *Service) emit(ctx context.Context, tx pgx.Tx, actor *authz.Actor, p dbq.Project, issue *dbq.Issue, event string, data map[string]any) error {
	ev := events.Event{Type: event, ProjectID: p.ID, ActorID: actor.UserID()}
	if issue != nil {
		ev.IssueID = &issue.ID
		ev.IssueKey = issueKey(p, *issue)
	}
	if err := events.Publish(ctx, tx, ev); err != nil {
		return err
	}
	if data == nil {
		data = map[string]any{}
	}
	if s.OnEvent != nil && IsServiceProject(p) {
		if err := s.OnEvent(ctx, tx, actor, p, issue, event, data); err != nil {
			return err
		}
	}
	if s.Jobs == nil {
		return nil
	}
	if issue != nil {
		data["issueKey"] = issueKey(p, *issue)
		data["summary"] = issue.Summary
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	args := jobs.DomainEvent{
		Event: event, ProjectID: p.ID, ActorID: actor.UserID(), ActorType: actorType(actor),
		Depth: actor.Depth, RuleID: actor.RuleID, Data: raw,
	}
	if issue != nil {
		args.IssueID = &issue.ID
	}
	_, err = s.Jobs.InsertTx(ctx, tx, args, nil)
	return err
}

// Audit records a security or configuration event.
func Audit(ctx context.Context, q *dbq.Queries, actor *authz.Actor, action, targetType, targetID string, projectID *uuid.UUID, data map[string]any) {
	b, _ := json.Marshal(data)
	if data == nil {
		b = []byte("{}")
	}
	at := "system"
	switch {
	case actor == nil:
	case actor.TokenID != nil:
		at = "token"
	case actor.User != nil:
		at = "user"
	}
	var actorID *uuid.UUID
	if actor != nil {
		actorID = actor.UserID()
	}
	_ = q.InsertAudit(ctx, dbq.InsertAuditParams{
		ActorID: actorID, ActorType: at, Action: action, TargetType: strp(targetType), TargetID: strp(targetID),
		ProjectID: projectID, Data: b,
	})
}

func issueKey(p dbq.Project, i dbq.Issue) string { return fmt.Sprintf("%s-%d", p.Key, i.Number) }

// ServiceTemplate is the template of service projects: a customer portal, request types,
// queues, SLAs and approvals (see the servicedesk package).
const ServiceTemplate = "service"

// IsServiceProject reports a service project.
func IsServiceProject(p dbq.Project) bool { return p.Template == ServiceTemplate }

// workflowDef loads and decodes a workflow version definition.
func workflowDef(ctx context.Context, q *dbq.Queries, versionID uuid.UUID) (*workflow.Definition, dbq.WorkflowVersion, error) {
	wv, err := q.GetWorkflowVersionByID(ctx, versionID)
	if err != nil {
		return nil, wv, err
	}
	var def workflow.Definition
	if err := json.Unmarshal(wv.Definition, &def); err != nil {
		return nil, wv, fmt.Errorf("decode workflow definition: %w", err)
	}
	return &def, wv, nil
}

// currentVersion returns the current version of a workflow.
func currentVersion(ctx context.Context, q *dbq.Queries, workflowID uuid.UUID) (*workflow.Definition, dbq.WorkflowVersion, error) {
	wf, err := q.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, dbq.WorkflowVersion{}, err
	}
	wv, err := q.GetWorkflowVersion(ctx, dbq.GetWorkflowVersionParams{WorkflowID: workflowID, Version: wf.CurrentVersion})
	if err != nil {
		return nil, wv, err
	}
	var def workflow.Definition
	if err := json.Unmarshal(wv.Definition, &def); err != nil {
		return nil, wv, err
	}
	return &def, wv, nil
}

// Emit lets other modules (SCM, automation) record a domain event on an issue in their transaction.
func (s *Service) Emit(ctx context.Context, tx pgx.Tx, actor *authz.Actor, p dbq.Project, issue *dbq.Issue, event string, data map[string]any) error {
	return s.emit(ctx, tx, actor, p, issue, event, data)
}

// Refresh publishes a live update for open boards without a domain event.
func (s *Service) Refresh(ctx context.Context, tx pgx.Tx, actor *authz.Actor, p dbq.Project, issue *dbq.Issue) error {
	return publishOnly(ctx, tx, actor, p, issue)
}

// IssueKey formats PLAT-12.
func IssueKey(p dbq.Project, i dbq.Issue) string { return issueKey(p, i) }

// CELIssue exposes the CEL `issue` variable for automation conditions.
func (s *Service) CELIssue(ctx context.Context, p dbq.Project, i dbq.Issue) (map[string]any, error) {
	return s.celIssue(ctx, s.Q, p, i)
}

// SearchProject runs JQL inside one project (used by scheduled automation).
func (s *Service) SearchProject(ctx context.Context, actor *authz.Actor, projectID uuid.UUID, jql string, limit int) ([]dbq.Issue, error) {
	issues, _, err := s.runSearch(ctx, actor, []uuid.UUID{projectID}, SearchQuery{JQL: jql, DefaultOrder: nil, Limit: limit})
	return issues, err
}
