// Package automation runs project automation rules (trigger + CEL condition + actions), outbound
// webhooks and in-app notifications, all from domain events processed by the worker.
// Loops are prevented by capping chain depth and never letting a rule trigger itself.
package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/net/http/httpguts"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/config"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/netguard"
	"github.com/workflowproj/workflow/internal/secure"
	"github.com/workflowproj/workflow/internal/tracker"
	"github.com/workflowproj/workflow/internal/workflow"
)

type Service struct {
	Pool    *pgxpool.Pool
	Q       *dbq.Queries
	Tracker *tracker.Service
	Eval    *workflow.Evaluator
	Jobs    jobs.Enqueuer
	Box     *secure.Box
	Cfg     *config.Config
	Log     *slog.Logger
	HTTP    *http.Client
}

func New(pool *pgxpool.Pool, tr *tracker.Service, eval *workflow.Evaluator, enq jobs.Enqueuer, box *secure.Box, cfg *config.Config, log *slog.Logger) *Service {
	return &Service{
		Pool: pool, Q: dbq.New(pool), Tracker: tr, Eval: eval, Jobs: enq, Box: box, Cfg: cfg, Log: log,
		HTTP: netguard.Client(10*time.Second, cfg.WebhookAllowPrivate, cfg.OutboundAllowedHosts...),
	}
}

// ---------------------------------------------------------------------------
// Rules

var TriggerTypes = []string{"issue.created", "issue.transitioned", "issue.field_changed", "pr.opened", "pr.merged", "commit.pushed", "branch.created", "pipeline.completed", "deployment.completed", "schedule", "sla.breached"}
var ActionTypes = []string{"transition", "assign", "set_field", "comment", "add_label", "webhook", "notify", "rollup_dates", "chat"}

// ActionTargets are the issues an action can apply to, relative to the one that triggered the rule.
var ActionTargets = []string{"", "issue", "parent", "children"}

var categories = []string{"todo", "in_progress", "done"}

type Trigger struct {
	Type       string `json:"type"`
	ToStatus   string `json:"toStatus,omitempty"`
	ToCategory string `json:"toCategory,omitempty"`
	Field      string `json:"field,omitempty"` // comma-separated

	Cron string `json:"cron,omitempty"`
	JQL  string `json:"jql,omitempty"`

	// deployment.completed: only this environment (any case) and outcome (success or failed).
	Environment string `json:"environment,omitempty"`
	Status      string `json:"status,omitempty"`
}

type Action struct {
	Type       string `json:"type"`
	Target     string `json:"target,omitempty"`
	Status     string `json:"status,omitempty"`
	Category   string `json:"category,omitempty"`
	Assignee   string `json:"assignee,omitempty"`
	Field      string `json:"field,omitempty"`
	Value      any    `json:"value,omitempty"`
	Body       string `json:"body,omitempty"`
	Label      string `json:"label,omitempty"`
	URL        string `json:"url,omitempty"`
	Recipients string `json:"recipients,omitempty"`
	ChannelID  string `json:"channelId,omitempty"` // chat: a chat channel of the project

	// webhook: Secret signs the body (X-WFM-Signature-256) and AuthValue is sent in the AuthHeader
	// header (default Authorization). Both are write-only: sealSecrets encrypts them into SecretEnc
	// and AuthEnc, and the plain values are never stored or returned. nil keeps the stored value, ""
	// removes it.
	Secret     *string `json:"secret,omitempty"`
	AuthHeader string  `json:"authHeader,omitempty"`
	AuthValue  *string `json:"authValue,omitempty"`
	SecretEnc  string  `json:"secretEnc,omitempty"`
	AuthEnc    string  `json:"authEnc,omitempty"`
}

type RuleInput struct {
	Name        string
	Description string
	Enabled     bool
	Trigger     Trigger
	Condition   string
	Actions     []Action
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func (s *Service) validate(ctx context.Context, actor *authz.Actor, projectID uuid.UUID, in RuleInput) (*time.Time, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperr.Invalid("name", "name is required")
	}
	if !slices.Contains(TriggerTypes, in.Trigger.Type) {
		return nil, apperr.Invalid("trigger.type", "unknown trigger %q", in.Trigger.Type)
	}
	if in.Trigger.ToCategory != "" && !slices.Contains(categories, in.Trigger.ToCategory) {
		return nil, apperr.Invalid("trigger.toCategory", "must be todo, in_progress or done")
	}
	if in.Trigger.Status != "" && !slices.Contains([]string{"success", "failed"}, in.Trigger.Status) {
		return nil, apperr.Invalid("trigger.status", "must be success or failed")
	}
	var next *time.Time
	if in.Trigger.Type == "schedule" {
		sched, err := cronParser.Parse(in.Trigger.Cron)
		if err != nil {
			return nil, apperr.Invalid("trigger.cron", "invalid cron expression: %v", err)
		}
		n := sched.Next(time.Now().UTC())
		next = &n
		if strings.TrimSpace(in.Trigger.JQL) == "" {
			return nil, apperr.Invalid("trigger.jql", "scheduled rules need a query selecting the issues to act on")
		}
		if err := s.Tracker.ValidateJQL(ctx, actor, in.Trigger.JQL); err != nil {
			return nil, err
		}
	}
	if in.Condition != "" {
		if err := s.Eval.Check(in.Condition); err != nil {
			return nil, apperr.Invalid("condition", "%v", err)
		}
	}
	if len(in.Actions) == 0 {
		return nil, apperr.Invalid("actions", "at least one action is required")
	}
	for i, a := range in.Actions {
		field := fmt.Sprintf("actions[%d]", i)
		if !slices.Contains(ActionTargets, a.Target) {
			return nil, apperr.Invalid(field, "target must be issue, parent or children")
		}
		switch a.Type {
		case "transition":
			switch {
			case a.Category != "":
				if !slices.Contains(categories, a.Category) {
					return nil, apperr.Invalid(field, "category must be todo, in_progress or done")
				}
			case a.Status == "":
				return nil, apperr.Invalid(field, "transition needs a target status or category")
			default:
				if _, err := s.Q.GetStatusByName(ctx, a.Status); err != nil {
					return nil, apperr.Invalid(field, "unknown status %q", a.Status)
				}
			}
		case "chat":
			id, err := uuid.Parse(a.ChannelID)
			if err != nil {
				return nil, apperr.Invalid(field, "chat needs a channel")
			}
			if ch, err := s.Q.GetChatChannel(ctx, id); err != nil || ch.ProjectID != projectID {
				return nil, apperr.Invalid(field, "unknown chat channel")
			}
			if strings.TrimSpace(a.Body) == "" {
				return nil, apperr.Invalid(field, "chat needs a message")
			}
		case "rollup_dates":
		case "assign":
			if a.Assignee == "" {
				return nil, apperr.Invalid(field, "assign needs actor, reporter, unassigned or an email")
			}
		case "set_field":
			if a.Field == "" {
				return nil, apperr.Invalid(field, "set_field needs a field")
			}
		case "comment":
			if strings.TrimSpace(a.Body) == "" {
				return nil, apperr.Invalid(field, "comment needs a body")
			}
		case "add_label":
			if strings.TrimSpace(a.Label) == "" {
				return nil, apperr.Invalid(field, "add_label needs a label")
			}
		case "webhook":
			u, err := url.Parse(a.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return nil, apperr.Invalid(field, "webhook needs an http(s) URL")
			}
			if h := a.AuthHeader; h != "" && (!httpguts.ValidHeaderFieldName(h) || reservedHeader(h)) {
				return nil, apperr.Invalid(field+".authHeader", "%q can't be used as the auth header", h)
			}
		case "notify":
			if !slices.Contains([]string{"assignee", "reporter", "watchers"}, a.Recipients) {
				return nil, apperr.Invalid(field, "notify recipients must be assignee, reporter or watchers")
			}
		default:
			return nil, apperr.Invalid(field, "unknown action %q", a.Type)
		}
	}
	return next, nil
}

func (s *Service) ListRules(ctx context.Context, actor *authz.Actor, projectRef string) ([]dbq.AutomationRule, error) {
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectRef, authz.ProjectAdmin)
	if err != nil {
		return nil, err
	}
	return s.Q.ListAutomationRules(ctx, p.ID)
}

func (s *Service) CreateRule(ctx context.Context, actor *authz.Actor, projectRef string, in RuleInput) (dbq.AutomationRule, error) {
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectRef, authz.ProjectAdmin)
	if err != nil {
		return dbq.AutomationRule{}, err
	}
	next, err := s.validate(ctx, actor, p.ID, in)
	if err != nil {
		return dbq.AutomationRule{}, err
	}
	tc, _ := json.Marshal(in.Trigger)
	acts, _ := json.Marshal(s.sealSecrets(in.Actions, nil))
	r, err := s.Q.CreateAutomationRule(ctx, dbq.CreateAutomationRuleParams{
		ProjectID: p.ID, Name: strings.TrimSpace(in.Name), Description: in.Description, Enabled: in.Enabled, TriggerType: in.Trigger.Type,
		TriggerConfig: tc, Condition: in.Condition, Actions: acts, CreatedBy: actor.UserID(), NextRunAt: next,
	})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "automation.create", "automation_rule", r.ID.String(), &p.ID, map[string]any{"name": r.Name})
	}
	return r, err
}

func (s *Service) ruleForAdmin(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.AutomationRule, dbq.Project, error) {
	r, err := s.Q.GetAutomationRule(ctx, id)
	if err != nil {
		return r, dbq.Project{}, apperr.NotFound("rule")
	}
	p, _, err := s.Tracker.ProjectFor(ctx, actor, r.ProjectID.String(), authz.ProjectAdmin)
	if apperr.IsNotFound(err) {
		return r, p, apperr.NotFound("rule")
	}
	return r, p, err
}

func (s *Service) UpdateRule(ctx context.Context, actor *authz.Actor, id uuid.UUID, in RuleInput) (dbq.AutomationRule, error) {
	old, p, err := s.ruleForAdmin(ctx, actor, id)
	if err != nil {
		return dbq.AutomationRule{}, err
	}
	next, err := s.validate(ctx, actor, p.ID, in)
	if err != nil {
		return dbq.AutomationRule{}, err
	}
	var prev []Action
	_ = json.Unmarshal(old.Actions, &prev)
	tc, _ := json.Marshal(in.Trigger)
	acts, _ := json.Marshal(s.sealSecrets(in.Actions, prev))
	r, err := s.Q.UpdateAutomationRule(ctx, dbq.UpdateAutomationRuleParams{
		ID: id, Name: strings.TrimSpace(in.Name), Description: in.Description, Enabled: in.Enabled, TriggerType: in.Trigger.Type,
		TriggerConfig: tc, Condition: in.Condition, Actions: acts, NextRunAt: next,
	})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "automation.update", "automation_rule", id.String(), &p.ID, map[string]any{"name": r.Name, "enabled": r.Enabled})
	}
	return r, err
}

func (s *Service) DeleteRule(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	r, p, err := s.ruleForAdmin(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := s.Q.DeleteAutomationRule(ctx, id); err != nil {
		return err
	}
	tracker.Audit(ctx, s.Q, actor, "automation.delete", "automation_rule", id.String(), &p.ID, map[string]any{"name": r.Name})
	return nil
}

func (s *Service) Runs(ctx context.Context, actor *authz.Actor, id uuid.UUID, limit int) ([]dbq.ListAutomationRunsRow, error) {
	if _, _, err := s.ruleForAdmin(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.Q.ListAutomationRuns(ctx, dbq.ListAutomationRunsParams{RuleID: id, Limit: int32(limit)})
}

// ---------------------------------------------------------------------------
// Domain events

var triggerForEvent = map[string]string{
	"issue.created": "issue.created", "issue.transitioned": "issue.transitioned", "issue.updated": "issue.field_changed",
	"pr.opened": "pr.opened", "pr.merged": "pr.merged", "commit.pushed": "commit.pushed",
	"branch.created": "branch.created", "pipeline.completed": "pipeline.completed", "sla.breached": "sla.breached",
	"deployment.completed": "deployment.completed",
}

// HandleDomainEvent is the worker entry point for jobs.DomainEvent.
func (s *Service) HandleDomainEvent(ctx context.Context, ev jobs.DomainEvent) error {
	data := map[string]any{}
	if len(ev.Data) > 0 {
		_ = json.Unmarshal(ev.Data, &data)
	}
	if err := s.notifyForEvent(ctx, ev, data); err != nil {
		s.Log.Warn("notifications failed", "event", ev.Event, "error", err)
	}
	if err := s.dispatchWebhooks(ctx, ev, data); err != nil {
		return err
	}
	if err := s.dispatchChat(ctx, ev, data); err != nil {
		return err
	}
	return s.runRules(ctx, ev, data)
}

func (s *Service) runRules(ctx context.Context, ev jobs.DomainEvent, data map[string]any) error {
	triggerType, ok := triggerForEvent[ev.Event]
	if !ok || ev.IssueID == nil {
		return nil
	}
	rules, err := s.Q.EnabledRulesForTrigger(ctx, dbq.EnabledRulesForTriggerParams{ProjectID: ev.ProjectID, TriggerType: triggerType})
	if err != nil || len(rules) == 0 {
		return err
	}
	for _, rule := range rules {
		var t Trigger
		_ = json.Unmarshal(rule.TriggerConfig, &t)
		if ev.RuleID != nil && *ev.RuleID == rule.ID {
			continue // a rule never triggers itself
		}
		if t.ToStatus != "" && !strings.EqualFold(fmt.Sprint(data["to"]), t.ToStatus) {
			continue
		}
		if t.ToCategory != "" && fmt.Sprint(data["toCategory"]) != t.ToCategory {
			continue
		}
		if t.Field != "" && !changedAny(data, t.Field) {
			continue
		}
		if t.Environment != "" && !strings.EqualFold(fmt.Sprint(data["environment"]), strings.TrimSpace(t.Environment)) {
			continue
		}
		if t.Status != "" && fmt.Sprint(data["status"]) != t.Status {
			continue
		}
		if ev.Depth >= s.Cfg.AutomationMaxDepth {
			s.record(ctx, rule.ID, ev.IssueID, "skipped", fmt.Sprintf("stopped: automation chain depth %d reached", ev.Depth), ev.Depth)
			continue
		}
		s.runRule(ctx, rule, *ev.IssueID, ev, data)
	}
	return nil
}

func (s *Service) record(ctx context.Context, ruleID uuid.UUID, issueID *uuid.UUID, status, msg string, depth int) {
	if err := s.Q.InsertAutomationRun(ctx, dbq.InsertAutomationRunParams{RuleID: ruleID, IssueID: issueID, Status: status, Message: msg, Depth: int32(depth)}); err != nil {
		s.Log.Warn("record automation run", "error", err)
	}
	_ = s.Q.TouchRuleLastRun(ctx, ruleID)
}

// runRule evaluates the condition and runs the actions for one issue.
func (s *Service) runRule(ctx context.Context, rule dbq.AutomationRule, issueID uuid.UUID, ev jobs.DomainEvent, data map[string]any) {
	issue, err := s.Q.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	p, err := s.Q.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return
	}
	if rule.Condition != "" {
		issueVars, err := s.Tracker.CELIssue(ctx, p, issue)
		if err != nil {
			s.record(ctx, rule.ID, &issueID, "failed", err.Error(), ev.Depth)
			return
		}
		event := map[string]any{"type": ev.Event, "actorType": ev.ActorType}
		for k, v := range data {
			event[k] = v
		}
		ok, err := s.Eval.Eval(rule.Condition, workflow.Vars{Issue: issueVars, Event: event, Now: time.Now()})
		if err != nil {
			s.record(ctx, rule.ID, &issueID, "failed", "condition: "+err.Error(), ev.Depth)
			return
		}
		if !ok {
			s.record(ctx, rule.ID, &issueID, "skipped", "condition not met", ev.Depth)
			return
		}
	}
	var actions []Action
	_ = json.Unmarshal(rule.Actions, &actions)
	actor := authz.AutomationActor(rule.ID, ev.Depth+1)
	trigger := triggerRef{Key: tracker.IssueKey(p, issue), Summary: issue.Summary}
	var done []string
	for _, a := range actions {
		targets, err := s.targets(ctx, issue, a.Target)
		if err != nil {
			s.record(ctx, rule.ID, &issueID, "failed", fmt.Sprintf("%s: %v", a.Type, err), ev.Depth)
			return
		}
		if len(targets) == 0 {
			done = append(done, fmt.Sprintf("%s: no %s", a.Type, a.Target))
			continue
		}
		for _, target := range targets {
			msg, err := s.runAction(ctx, actor, p, target, a, trigger, ev, data)
			if err != nil {
				s.record(ctx, rule.ID, &issueID, "failed", fmt.Sprintf("%s: %v", a.Type, err), ev.Depth)
				return
			}
			if msg != "" {
				if target != issueID {
					msg = s.keyOf(ctx, p, target) + ": " + msg
				}
				done = append(done, msg)
			}
		}
	}
	s.record(ctx, rule.ID, &issueID, "success", strings.Join(done, "; "), ev.Depth)
}

// changedAny reports whether an issue.updated event changed any of the comma-separated fields.
func changedAny(data map[string]any, fields string) bool {
	changed, _ := data["fields"].([]any)
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" && slices.ContainsFunc(changed, func(c any) bool { return strings.EqualFold(fmt.Sprint(c), f) }) {
			return true
		}
	}
	return false
}

// targets resolves an action's target relative to the triggering issue.
func (s *Service) targets(ctx context.Context, issue dbq.Issue, target string) ([]uuid.UUID, error) {
	switch target {
	case "parent":
		if issue.ParentID == nil {
			return nil, nil
		}
		return []uuid.UUID{*issue.ParentID}, nil
	case "children":
		children, err := s.Q.ChildIssues(ctx, &issue.ID)
		if err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, len(children))
		for i, c := range children {
			ids[i] = c.ID
		}
		return ids, nil
	}
	return []uuid.UUID{issue.ID}, nil
}

func (s *Service) keyOf(ctx context.Context, p dbq.Project, id uuid.UUID) string {
	i, err := s.Q.GetIssue(ctx, id)
	if err != nil {
		return id.String()
	}
	if i.ProjectID != p.ID {
		if other, err := s.Q.GetProject(ctx, i.ProjectID); err == nil {
			p = other
		}
	}
	return tracker.IssueKey(p, i)
}

// triggerRef names the issue that triggered a rule; actions may run on its parent or children.
type triggerRef struct{ Key, Summary string }

// render fills a template: {{issue.*}} is the issue the action runs on, {{trigger.*}} the issue
// that triggered the rule (the same unless the action targets a parent or children), and
// {{event.*}} the trigger's data.
func render(tmpl string, key, summary string, trigger triggerRef, data map[string]any) string {
	out := strings.NewReplacer("{{issue.key}}", key, "{{issue.summary}}", summary,
		"{{trigger.key}}", trigger.Key, "{{trigger.summary}}", trigger.Summary).Replace(tmpl)
	for k, v := range data {
		out = strings.ReplaceAll(out, "{{event."+k+"}}", fmt.Sprint(v))
	}
	return out
}

func (s *Service) runAction(ctx context.Context, actor *authz.Actor, p dbq.Project, issueID uuid.UUID, a Action, trigger triggerRef, ev jobs.DomainEvent, data map[string]any) (string, error) {
	issue, err := s.Q.GetIssue(ctx, issueID)
	if err != nil {
		return "", err
	}
	key := tracker.IssueKey(p, issue)
	ref := issueID.String()
	switch a.Type {
	case "transition":
		if a.Category != "" {
			to, err := s.Tracker.TransitionToCategory(ctx, actor, issueID, a.Category)
			if err != nil {
				return "", err
			}
			if to == "" {
				return fmt.Sprintf("already %s or no transition to it", strings.ReplaceAll(a.Category, "_", " ")), nil
			}
			return "moved to " + to, nil
		}
		ran, err := s.Tracker.TransitionToStatus(ctx, actor, issueID, a.Status, "")
		if err != nil {
			return "", err
		}
		if !ran {
			return fmt.Sprintf("no transition to %s from the current status", a.Status), nil
		}
		return "moved to " + a.Status, nil
	case "rollup_dates":
		return s.rollupDates(ctx, actor, issue, ref)
	case "chat":
		return s.chatAction(ctx, p, issue, a, render(a.Body, key, issue.Summary, trigger, data))
	case "assign":
		var target tracker.Opt[uuid.UUID]
		switch a.Assignee {
		case "actor":
			if ev.ActorID == nil {
				return "no actor to assign", nil
			}
			target = tracker.Some(*ev.ActorID)
		case "reporter":
			if issue.ReporterID == nil {
				return "no reporter", nil
			}
			target = tracker.Some(*issue.ReporterID)
		case "unassigned":
			target = tracker.Null[uuid.UUID]()
		default:
			u, err := s.Q.GetUserByEmail(ctx, a.Assignee)
			if err != nil {
				return "", fmt.Errorf("unknown user %s", a.Assignee)
			}
			target = tracker.Some(u.ID)
		}
		_, _, err := s.Tracker.UpdateIssue(ctx, actor, ref, tracker.IssuePatch{AssigneeID: target})
		return "assigned", err
	case "set_field":
		patch := tracker.IssuePatch{}
		switch a.Field {
		case "priority":
			v := fmt.Sprint(a.Value)
			patch.Priority = &v
		case "storyPoints":
			if f, ok := a.Value.(float64); ok {
				patch.StoryPoints = tracker.Some(f)
			} else {
				patch.StoryPoints = tracker.Null[float64]()
			}
		case "summary":
			v := render(fmt.Sprint(a.Value), key, issue.Summary, trigger, data)
			patch.Summary = &v
		default:
			patch.Fields = map[string]any{a.Field: a.Value}
		}
		_, _, err := s.Tracker.UpdateIssue(ctx, actor, ref, patch)
		return "set " + a.Field, err
	case "add_label":
		labels := append(slices.Clone(issue.Labels), a.Label)
		_, _, err := s.Tracker.UpdateIssue(ctx, actor, ref, tracker.IssuePatch{Labels: &labels})
		return "labelled " + a.Label, err
	case "comment":
		_, err := s.Tracker.AddComment(ctx, actor, ref, render(a.Body, key, issue.Summary, trigger, data))
		return "commented", err
	case "webhook":
		payload, _ := json.Marshal(map[string]any{"event": "automation", "issue": key, "trigger": ev.Event, "data": data, "sentAt": time.Now().UTC()})
		secret, auth, err := s.openSecrets(a)
		if err != nil {
			return "", err
		}
		return "webhook sent", s.post(ctx, a.URL, secret, "automation", uuid.NewString(), payload, auth)
	case "notify":
		if err := s.notifyRecipients(ctx, issue, a.Recipients, nil, "automation", key+": "+issue.Summary, "Automation: "+ev.Event); err != nil {
			return "", err
		}
		return "notified " + a.Recipients, nil
	}
	return "", fmt.Errorf("unknown action %s", a.Type)
}

// rollupDates sets an issue's start and due dates to span its children's (ignoring children
// without dates), so an epic's bar on the timeline covers its stories.
func (s *Service) rollupDates(ctx context.Context, actor *authz.Actor, issue dbq.Issue, ref string) (string, error) {
	children, err := s.Q.ChildIssues(ctx, &issue.ID)
	if err != nil {
		return "", err
	}
	var start, due *time.Time
	for _, c := range children {
		for _, d := range []*time.Time{c.StartDate, c.DueDate} {
			if d == nil {
				continue
			}
			if start == nil || d.Before(*start) {
				start = d
			}
			if due == nil || d.After(*due) {
				due = d
			}
		}
	}
	if start == nil {
		return "no dated children", nil
	}
	if sameDay(start, issue.StartDate) && sameDay(due, issue.DueDate) {
		return "dates already span the children", nil
	}
	patch := tracker.IssuePatch{StartDate: tracker.Some(*start), DueDate: tracker.Some(*due)}
	if _, _, err := s.Tracker.UpdateIssue(ctx, actor, ref, patch); err != nil {
		return "", err
	}
	return fmt.Sprintf("dates set to %s – %s", start.Format("2006-01-02"), due.Format("2006-01-02")), nil
}

func sameDay(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

// RunScheduled runs scheduled rules that are due (periodic job, every minute).
func (s *Service) RunScheduled(ctx context.Context) error {
	var due []dbq.AutomationRule
	err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		var err error
		if due, err = q.DueScheduledRules(ctx); err != nil {
			return err
		}
		for _, r := range due {
			var t Trigger
			_ = json.Unmarshal(r.TriggerConfig, &t)
			var next *time.Time
			if sched, err := cronParser.Parse(t.Cron); err == nil {
				n := sched.Next(time.Now().UTC())
				next = &n
			}
			if err := q.MarkRuleRun(ctx, dbq.MarkRuleRunParams{ID: r.ID, NextRunAt: next}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, r := range due {
		var t Trigger
		_ = json.Unmarshal(r.TriggerConfig, &t)
		issues, err := s.Tracker.SearchProject(ctx, authz.AutomationActor(r.ID, 0), r.ProjectID, t.JQL, 100)
		if err != nil {
			s.record(ctx, r.ID, nil, "failed", "query: "+err.Error(), 0)
			continue
		}
		if len(issues) == 0 {
			s.record(ctx, r.ID, nil, "skipped", "no issues matched", 0)
		}
		for _, i := range issues {
			s.runRule(ctx, r, i.ID, jobs.DomainEvent{Event: "schedule", ProjectID: r.ProjectID, IssueID: &i.ID, ActorType: "automation"}, map[string]any{})
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Notifications

func (s *Service) notifyRecipients(ctx context.Context, issue dbq.Issue, who string, except *uuid.UUID, kind, title, body string) error {
	var ids []uuid.UUID
	switch who {
	case "assignee":
		if issue.AssigneeID != nil {
			ids = append(ids, *issue.AssigneeID)
		}
	case "reporter":
		if issue.ReporterID != nil {
			ids = append(ids, *issue.ReporterID)
		}
	case "watchers", "involved":
		// involved: the reporter and assignee too, even if they stopped watching.
		if who == "involved" {
			for _, u := range []*uuid.UUID{issue.ReporterID, issue.AssigneeID} {
				if u != nil {
					ids = append(ids, *u)
				}
			}
		}
		w, err := s.Q.WatcherIDs(ctx, issue.ID)
		if err != nil {
			return err
		}
		ids = append(ids, w...)
	}
	ids, err := s.Tracker.OnlyAgents(ctx, issue.ProjectID, ids)
	if err != nil {
		return err
	}
	return s.Tracker.Notify(ctx, s.Q, ids, except, &issue.ID, kind, title, body)
}

func (s *Service) notifyForEvent(ctx context.Context, ev jobs.DomainEvent, data map[string]any) error {
	if ev.IssueID == nil {
		return nil
	}
	issue, err := s.Q.GetIssue(ctx, *ev.IssueID)
	if err != nil {
		return nil // deleted since
	}
	key, _ := data["issueKey"].(string)
	title := key + ": " + issue.Summary
	switch ev.Event {
	case "issue.created":
		if issue.AssigneeID != nil {
			return s.Tracker.Notify(ctx, s.Q, []uuid.UUID{*issue.AssigneeID}, ev.ActorID, &issue.ID, "assigned", title, "You were assigned a new issue")
		}
	case "issue.updated":
		if fields, _ := data["fields"].([]any); slices.Contains(fields, any("assignee")) && issue.AssigneeID != nil {
			return s.Tracker.Notify(ctx, s.Q, []uuid.UUID{*issue.AssigneeID}, ev.ActorID, &issue.ID, "assigned", title, "You were assigned this issue")
		}
	case "issue.transitioned":
		return s.notifyRecipients(ctx, issue, "involved", ev.ActorID, "transitioned", title, fmt.Sprintf("Moved from %v to %v", data["from"], data["to"]))
	case "comment.created":
		return s.notifyRecipients(ctx, issue, "involved", ev.ActorID, "commented", title, fmt.Sprint(data["body"]))
	case "sla.breached":
		return s.notifyRecipients(ctx, issue, "assignee", nil, "sla", title, fmt.Sprintf("%v breached", data["sla"]))
	}
	return nil
}

// HandlePostFunction runs side-effecting workflow post-functions (notify, webhook).
func (s *Service) HandlePostFunction(ctx context.Context, pf jobs.PostFunction) error {
	issue, err := s.Q.GetIssue(ctx, pf.IssueID)
	if err != nil {
		return nil
	}
	p, err := s.Q.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return err
	}
	key := tracker.IssueKey(p, issue)
	if pf.Notify != "" {
		return s.notifyRecipients(ctx, issue, pf.Notify, pf.ActorID, "transitioned", key+": "+issue.Summary, fmt.Sprintf("%s: %s → %s", pf.Transition, pf.FromStatus, pf.ToStatus))
	}
	if pf.Webhook != "" {
		view, err := s.Tracker.IssueView(ctx, issue)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"event": "workflow.post_function", "transition": pf.Transition, "from": pf.FromStatus, "to": pf.ToStatus,
			"issue": view, "sentAt": time.Now().UTC(),
		})
		return s.post(ctx, pf.Webhook, "", "workflow.post_function", uuid.NewString(), payload, nil)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Outbound webhooks

var WebhookEvents = []string{"*", "issue.created", "issue.updated", "issue.transitioned", "issue.deleted", "comment.created", "sprint.started", "sprint.completed", "release.released", "sla.breached", "deployment.completed"}

func (s *Service) dispatchWebhooks(ctx context.Context, ev jobs.DomainEvent, data map[string]any) error {
	if !slices.Contains(WebhookEvents[1:], ev.Event) {
		return nil
	}
	subs, err := s.Q.MatchingSubscriptions(ctx, dbq.MatchingSubscriptionsParams{ProjectID: &ev.ProjectID, Event: ev.Event})
	if err != nil || len(subs) == 0 {
		return err
	}
	p, err := s.Q.GetProject(ctx, ev.ProjectID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"event": ev.Event, "timestamp": time.Now().UTC(), "project": map[string]any{"id": p.ID, "key": p.Key, "name": p.Name}, "data": data,
	}
	if ev.ActorID != nil {
		if u, err := s.Q.GetUser(ctx, *ev.ActorID); err == nil {
			payload["actor"] = tracker.UserView(u)
		}
	}
	if ev.IssueID != nil {
		if issue, err := s.Q.GetIssue(ctx, *ev.IssueID); err == nil {
			if view, err := s.Tracker.IssueView(ctx, issue); err == nil {
				payload["issue"] = view
			}
		}
	}
	body, _ := json.Marshal(payload)
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		for _, sub := range subs {
			d, err := q.CreateWebhookDelivery(ctx, dbq.CreateWebhookDeliveryParams{SubscriptionID: sub.ID, Event: ev.Event, Payload: body})
			if err != nil {
				return err
			}
			if _, err := s.Jobs.InsertTx(ctx, tx, jobs.DeliverWebhook{DeliveryID: d.ID}, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// Deliver sends one outbound webhook delivery; errors make River retry with backoff.
func (s *Service) Deliver(ctx context.Context, deliveryID uuid.UUID) error {
	d, err := s.Q.GetWebhookDelivery(ctx, deliveryID)
	if err != nil {
		return nil
	}
	sub, err := s.Q.GetWebhookSubscription(ctx, d.SubscriptionID)
	if err != nil || !sub.Active {
		return nil
	}
	secret, err := s.Box.Open(sub.SecretEnc)
	if err != nil {
		return err
	}
	err = s.post(ctx, sub.Url, string(secret), d.Event, d.ID.String(), d.Payload, nil)
	status, code, msg := "delivered", (*int32)(nil), (*string)(nil)
	if err != nil {
		status = "failed"
		m := err.Error()
		msg = &m
		if se, ok := err.(*statusError); ok {
			c := int32(se.code)
			code = &c
		}
	} else {
		c := int32(200)
		code = &c
	}
	if uerr := s.Q.FinishWebhookDelivery(ctx, dbq.FinishWebhookDeliveryParams{ID: d.ID, Status: status, ResponseStatus: code, LastError: msg}); uerr != nil {
		s.Log.Warn("record webhook delivery", "error", uerr)
	}
	return err
}

// reservedHeader reports headers an automation webhook's auth header must not replace.
func reservedHeader(h string) bool {
	h = strings.ToLower(h)
	return strings.HasPrefix(h, "x-wfm-") || slices.Contains([]string{"host", "content-type", "content-length", "user-agent"}, h)
}

// sealSecrets encrypts webhook actions' write-only secrets. A webhook that sends no value keeps the
// one stored on the previous version of the rule for the same URL: changing the URL drops it, so
// someone who can edit the rule can't redirect a secret they never knew to a server of their own.
func (s *Service) sealSecrets(actions, prev []Action) []Action {
	out := slices.Clone(actions)
	used := make([]bool, len(prev))
	for i := range out {
		a := &out[i]
		if a.Type != "webhook" {
			a.Secret, a.AuthValue, a.AuthHeader, a.SecretEnc, a.AuthEnc = nil, nil, "", "", ""
			continue
		}
		a.SecretEnc, a.AuthEnc = "", ""
		for j, p := range prev {
			if !used[j] && p.Type == "webhook" && p.URL == a.URL {
				used[j] = true
				a.SecretEnc, a.AuthEnc = p.SecretEnc, p.AuthEnc
				break
			}
		}
		if a.Secret != nil {
			a.SecretEnc = s.sealOrEmpty(*a.Secret)
		}
		if a.AuthValue != nil {
			a.AuthEnc = s.sealOrEmpty(*a.AuthValue)
		}
		if a.AuthEnc == "" {
			a.AuthHeader = ""
		}
		a.Secret, a.AuthValue = nil, nil
	}
	return out
}

func (s *Service) sealOrEmpty(v string) string {
	if v == "" {
		return ""
	}
	return s.Box.Seal([]byte(v))
}

// openSecrets decrypts a webhook action's signing secret and auth header.
func (s *Service) openSecrets(a Action) (string, http.Header, error) {
	var secret string
	if a.SecretEnc != "" {
		b, err := s.Box.Open(a.SecretEnc)
		if err != nil {
			return "", nil, fmt.Errorf("open webhook secret: %w", err)
		}
		secret = string(b)
	}
	if a.AuthEnc == "" {
		return secret, nil, nil
	}
	b, err := s.Box.Open(a.AuthEnc)
	if err != nil {
		return "", nil, fmt.Errorf("open webhook auth: %w", err)
	}
	name := a.AuthHeader
	if name == "" {
		name = "Authorization"
	}
	return secret, http.Header{http.CanonicalHeaderKey(name): {string(b)}}, nil
}

type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("receiver answered HTTP %d", e.code) }

// post sends a JSON payload. With a secret, the body is signed: X-WFM-Signature-256: sha256=<hmac>.
// extra headers (an automation webhook's auth header) are added as they are.
func (s *Service) post(ctx context.Context, target, secret, event, deliveryID string, body []byte, extra http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "workflow-platform-webhooks/1.0")
	req.Header.Set("X-WFM-Event", event)
	req.Header.Set("X-WFM-Delivery", deliveryID)
	if secret != "" {
		req.Header.Set("X-WFM-Signature-256", "sha256="+secure.HMACSHA256Hex(secret, body))
	}
	for k, v := range extra {
		req.Header[k] = v
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &statusError{res.StatusCode}
	}
	return nil
}

type WebhookInput struct {
	ProjectKey string
	Name       string
	URL        string
	Events     []string
	Active     bool
}

func (s *Service) canManageWebhooks(ctx context.Context, actor *authz.Actor, projectKey string) (*uuid.UUID, error) {
	if projectKey == "" {
		if !actor.IsOrgAdmin() {
			return nil, apperr.Forbidden("only organisation admins manage organisation-wide webhooks")
		}
		return nil, nil
	}
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectKey, authz.ProjectAdmin)
	if err != nil {
		return nil, err
	}
	return &p.ID, nil
}

func validateWebhook(in WebhookInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return apperr.Invalid("name", "name is required")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return apperr.Invalid("url", "an http(s) URL is required")
	}
	if len(in.Events) == 0 {
		return apperr.Invalid("events", "choose at least one event")
	}
	for _, e := range in.Events {
		if !slices.Contains(WebhookEvents, e) {
			return apperr.Invalid("events", "unknown event %q", e)
		}
	}
	return nil
}

func (s *Service) ListWebhooks(ctx context.Context, actor *authz.Actor, projectKey string) ([]dbq.WebhookSubscription, error) {
	pid, err := s.canManageWebhooks(ctx, actor, projectKey)
	if err != nil {
		return nil, err
	}
	return s.Q.ListWebhookSubscriptions(ctx, pid)
}

func (s *Service) CreateWebhook(ctx context.Context, actor *authz.Actor, in WebhookInput) (dbq.WebhookSubscription, string, error) {
	pid, err := s.canManageWebhooks(ctx, actor, in.ProjectKey)
	if err != nil {
		return dbq.WebhookSubscription{}, "", err
	}
	if err := validateWebhook(in); err != nil {
		return dbq.WebhookSubscription{}, "", err
	}
	secret := secure.Token("whsec_", 24)
	sub, err := s.Q.CreateWebhookSubscription(ctx, dbq.CreateWebhookSubscriptionParams{
		ProjectID: pid, Name: in.Name, Url: in.URL, SecretEnc: s.Box.Seal([]byte(secret)), Events: in.Events, Active: in.Active, CreatedBy: actor.UserID(),
	})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "webhook.create", "webhook", sub.ID.String(), pid, map[string]any{"url": in.URL, "events": in.Events})
	}
	return sub, secret, err
}

func (s *Service) webhookForManage(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.WebhookSubscription, error) {
	sub, err := s.Q.GetWebhookSubscription(ctx, id)
	if err != nil {
		return sub, apperr.NotFound("webhook")
	}
	key := ""
	if sub.ProjectID != nil {
		key = sub.ProjectID.String()
	}
	if _, err := s.canManageWebhooks(ctx, actor, key); err != nil {
		if apperr.IsNotFound(err) {
			return sub, apperr.NotFound("webhook")
		}
		return sub, err
	}
	return sub, nil
}

func (s *Service) UpdateWebhook(ctx context.Context, actor *authz.Actor, id uuid.UUID, in WebhookInput) (dbq.WebhookSubscription, error) {
	if _, err := s.webhookForManage(ctx, actor, id); err != nil {
		return dbq.WebhookSubscription{}, err
	}
	if err := validateWebhook(in); err != nil {
		return dbq.WebhookSubscription{}, err
	}
	return s.Q.UpdateWebhookSubscription(ctx, dbq.UpdateWebhookSubscriptionParams{ID: id, Name: in.Name, Url: in.URL, Events: in.Events, Active: in.Active})
}

func (s *Service) DeleteWebhook(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	sub, err := s.webhookForManage(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := s.Q.DeleteWebhookSubscription(ctx, id); err != nil {
		return err
	}
	tracker.Audit(ctx, s.Q, actor, "webhook.delete", "webhook", id.String(), sub.ProjectID, map[string]any{"url": sub.Url})
	return nil
}

func (s *Service) Deliveries(ctx context.Context, actor *authz.Actor, id uuid.UUID, limit int) ([]dbq.WebhookDelivery, error) {
	if _, err := s.webhookForManage(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.Q.ListWebhookDeliveries(ctx, dbq.ListWebhookDeliveriesParams{SubscriptionID: id, Limit: int32(limit)})
}

func (s *Service) TestWebhook(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	sub, err := s.webhookForManage(ctx, actor, id)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"event": "ping", "timestamp": time.Now().UTC(), "webhook": sub.Name})
	d, err := s.Q.CreateWebhookDelivery(ctx, dbq.CreateWebhookDeliveryParams{SubscriptionID: id, Event: "ping", Payload: body})
	if err != nil {
		return err
	}
	_, err = s.Jobs.Insert(ctx, jobs.DeliverWebhook{DeliveryID: d.ID}, nil)
	return err
}
