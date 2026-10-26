package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.yaml.in/yaml/v3"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

/*
 * Risks and decisions: a project's risk register and decision log are issues of special
 * issue-type kinds, so they get keys, history, comments, watchers, notifications, permissions,
 * search and automation like any issue, but they live in their own project section and never in
 * planning: boards, the backlog, sprints, the timeline, delivery metrics and service desk queues
 * leave them out (PlanningWork), they cannot join a sprint, and they have no parent or children.
 * Work links to them instead: mitigates (work → risk), implements (work → decision) and
 * supersedes (decision → decision).
 */

// Issue-type kinds. A type's kind is fixed when it is created.
const (
	KindWork     = "work"
	KindRisk     = "risk"
	KindDecision = "decision"
)

var issueTypeKinds = []string{KindWork, KindRisk, KindDecision}

// PlanningWork limits a query over issues aliased i to work, the only issues planning shows.
const PlanningWork = "i.type_id IN (SELECT id FROM issue_types WHERE kind = 'work')"

// The reserved fields of risks and decisions (see templates/risks_decisions.yaml).
const (
	FieldRiskLikelihood       = "risk_likelihood"
	FieldRiskImpact           = "risk_impact"
	FieldRiskScore            = "risk_score"
	FieldRiskRating           = "risk_rating"
	FieldRiskResponse         = "risk_response"
	FieldRiskMitigation       = "risk_mitigation"
	FieldDecisionContext      = "decision_context"
	FieldDecisionOptions      = "decision_options"
	FieldDecisionOutcome      = "decision_outcome"
	FieldDecisionConsequences = "decision_consequences"
	FieldDecisionDate         = "decision_date"
)

// sprintable reports whether issues of a type are planned in sprints: standard work only
// (sub-tasks follow their parent).
func sprintable(t dbq.IssueType) bool { return t.Level == 0 && t.Kind == KindWork }

// RiskRating names the band of a score (likelihood × impact): 1–4 low, 5–9 medium, 10–16 high,
// 20–25 critical. No product of two numbers from 1 to 5 falls between 16 and 20.
func RiskRating(score int) string {
	switch {
	case score >= 20:
		return "critical"
	case score >= 10:
		return "high"
	case score >= 5:
		return "medium"
	}
	return "low"
}

// ratingOptions are the rating field's values, by RiskRating.
var ratingOptions = map[string]string{"low": "Low", "medium": "Medium", "high": "High", "critical": "Critical"}

// riskLevel reads a likelihood or impact: a whole number from 1 to 5.
func riskLevel(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 1 || f > 5 {
		return 0, false
	}
	return int(f), true
}

// scoreRisk sets a risk's score and rating from its likelihood and impact, and clears them
// while either is missing.
func scoreRisk(fields map[string]any) {
	l, okL := riskLevel(fields[FieldRiskLikelihood])
	i, okI := riskLevel(fields[FieldRiskImpact])
	if !okL || !okI {
		delete(fields, FieldRiskScore)
		delete(fields, FieldRiskRating)
		return
	}
	fields[FieldRiskScore] = float64(l * i)
	fields[FieldRiskRating] = ratingOptions[RiskRating(l*i)]
}

// ---------------------------------------------------------------------------
// The section's definition

type addonField struct {
	Key         string   `yaml:"key"`
	Name        string   `yaml:"name"`
	Kind        string   `yaml:"kind"`
	Type        string   `yaml:"type"`
	Options     []string `yaml:"options"`
	Description string   `yaml:"description"`
}

type risksDecisionsAddon struct {
	Workflows  []yaml.Node         `yaml:"workflows"`
	IssueTypes []TemplateIssueType `yaml:"issueTypes"`
	Fields     []addonField        `yaml:"fields"`
}

var risksDecisions = func() risksDecisionsAddon {
	b, err := templateFS.ReadFile("templates/risks_decisions.yaml")
	if err != nil {
		panic(err)
	}
	var a risksDecisionsAddon
	if err := yaml.Unmarshal(b, &a); err != nil {
		panic(fmt.Sprintf("risks and decisions: %v", err))
	}
	return a
}()

// workflowSource renders one of the section's workflows as YAML.
func (a risksDecisionsAddon) workflowSource(name string) (string, error) {
	for _, n := range a.Workflows {
		var w struct {
			Name string `yaml:"name"`
		}
		if err := n.Decode(&w); err != nil {
			return "", err
		}
		if w.Name == name {
			b, err := yaml.Marshal(&n)
			if err != nil {
				return "", err
			}
			return "kind: Workflow\n" + string(b), nil
		}
	}
	return "", fmt.Errorf("risks and decisions: no workflow %q", name)
}

// IsReservedField reports a field key that risks and decisions own.
func IsReservedField(key string) bool {
	return slices.ContainsFunc(risksDecisions.Fields, func(f addonField) bool { return f.Key == key })
}

// ensureKindFields makes the fields of a kind apply to an issue type of that kind in a project,
// creating them the first time.
func (s *Service) ensureKindFields(ctx context.Context, q *dbq.Queries, p dbq.Project, kind, typeKey string) error {
	for _, f := range risksDecisions.Fields {
		if f.Kind != kind {
			continue
		}
		opts, err := json.Marshal(append([]string{}, f.Options...))
		if err != nil {
			return err
		}
		if err := q.EnsureFieldDefinition(ctx, dbq.EnsureFieldDefinitionParams{
			Key: f.Key, Name: f.Name, Description: f.Description, Type: f.Type, Options: opts,
			ProjectIds: []uuid.UUID{p.ID}, IssueTypeKeys: []string{typeKey},
		}); err != nil {
			return err
		}
		def, err := q.GetFieldDefinitionByKey(ctx, f.Key)
		if err != nil {
			return err
		}
		if def.Type != f.Type {
			return apperr.Conflict("field_conflict", "the custom field %q is a %s field; risks and decisions need it as a %s field", f.Key, def.Type, f.Type)
		}
		if err := q.AddFieldDefinitionProject(ctx, dbq.AddFieldDefinitionProjectParams{ID: def.ID, ProjectID: p.ID}); err != nil {
			return err
		}
		if err := q.AddFieldDefinitionIssueType(ctx, dbq.AddFieldDefinitionIssueTypeParams{ID: def.ID, TypeKey: typeKey}); err != nil {
			return err
		}
	}
	return nil
}

// installRisksDecisions turns the section on in a project, setting up what is missing: the Risk
// and Decision workflows and issue types (unless the project has types of those kinds) and
// their fields.
func (s *Service) installRisksDecisions(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, p dbq.Project) (dbq.Project, error) {
	// One install at a time per project: a second one waits, then finds the types in place.
	if _, err := q.LockProjectCounter(ctx, p.ID); err != nil {
		return p, err
	}
	types, err := q.ListIssueTypes(ctx, p.ID)
	if err != nil {
		return p, err
	}
	position := int32(len(types))
	for _, t := range risksDecisions.IssueTypes {
		var have []string
		taken := false
		for _, et := range types {
			switch {
			case et.Kind == t.Kind:
				have = append(have, et.Key)
			case et.Key == t.Key:
				taken = true
			}
		}
		if len(have) == 0 {
			if taken {
				return p, apperr.Conflict("type_conflict", "%s already has an issue type with the key %q; delete it, or move its issues to another type, to turn on risks and decisions", p.Key, t.Key)
			}
			wf, err := q.GetWorkflowByName(ctx, dbq.GetWorkflowByNameParams{ProjectID: &p.ID, Name: t.Workflow})
			if db.IsNoRows(err) {
				src, err := risksDecisions.workflowSource(t.Workflow)
				if err != nil {
					return p, err
				}
				res, err := s.createWorkflowTx(ctx, tx, q, actor, &p.ID, src)
				if err != nil {
					return p, err
				}
				wf = res.Workflow
			} else if err != nil {
				return p, err
			}
			if _, err := q.CreateIssueType(ctx, dbq.CreateIssueTypeParams{
				ProjectID: p.ID, Key: t.Key, Name: t.Name, Icon: t.Icon, Color: t.Color,
				Level: int32(t.Level), Position: position, WorkflowID: wf.ID, Kind: t.Kind,
			}); err != nil {
				return p, err
			}
			position++
			have = []string{t.Key}
		}
		for _, key := range have {
			if err := s.ensureKindFields(ctx, q, p, t.Kind, key); err != nil {
				return p, err
			}
		}
	}
	return q.SetProjectRisksDecisions(ctx, dbq.SetProjectRisksDecisionsParams{ID: p.ID, RisksDecisions: true})
}

// setRisksDecisions turns the section on (installing what is missing) or off (hiding it; the
// issues, types and fields stay).
func (s *Service) setRisksDecisions(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, p dbq.Project, on bool) error {
	var err error
	if on {
		_, err = s.installRisksDecisions(ctx, tx, q, actor, p)
	} else {
		_, err = q.SetProjectRisksDecisions(ctx, dbq.SetProjectRisksDecisionsParams{ID: p.ID, RisksDecisions: false})
	}
	if err == nil && on != p.RisksDecisions {
		Audit(ctx, q, actor, "project.risks_decisions", "project", p.ID.String(), &p.ID, map[string]any{"on": on})
	}
	return err
}

// fillDecidedOn dates a decision on the day it is accepted or rejected (enters a done status),
// unless it already has a date.
func (s *Service) fillDecidedOn(ctx context.Context, q *dbq.Queries, p dbq.Project, i dbq.Issue, upd *dbq.UpdateIssueParams, changes *[]change) error {
	it, err := q.GetIssueType(ctx, i.TypeID)
	if err != nil || it.Kind != KindDecision {
		return err
	}
	cur := map[string]any{}
	_ = json.Unmarshal(upd.Fields, &cur)
	if v, _ := cur[FieldDecisionDate].(string); v != "" {
		return nil
	}
	fields, fc, err := s.mergeFields(ctx, q, p, it, upd.Fields, map[string]any{FieldDecisionDate: s.Now().UTC().Format("2006-01-02")})
	if err != nil {
		return nil // the field no longer applies to the type: nothing to date
	}
	upd.Fields = fields
	for _, c := range fc {
		*changes = append(*changes, change{c.field, c.from, c.to, nil, nil})
	}
	return nil
}

// checkLinkKinds keeps the risk and decision links between the kinds they are for: work
// mitigates a risk, work implements a decision, a decision supersedes a decision.
func checkLinkKinds(ctx context.Context, q *dbq.Queries, linkType string, from, to dbq.Issue) error {
	want, ok := map[string][2]string{
		"mitigates":  {KindWork, KindRisk},
		"implements": {KindWork, KindDecision},
		"supersedes": {KindDecision, KindDecision},
	}[linkType]
	if !ok {
		return nil
	}
	ft, err := q.GetIssueType(ctx, from.TypeID)
	if err != nil {
		return err
	}
	tt, err := q.GetIssueType(ctx, to.TypeID)
	if err != nil {
		return err
	}
	if ft.Kind == want[0] && tt.Kind == want[1] {
		return nil
	}
	return apperr.Invalid("type", "%s", map[string]string{
		"mitigates":  "mitigates links a work issue to the risk it mitigates",
		"implements": "implements links a work issue to the decision it implements",
		"supersedes": "supersedes links a decision to the earlier decision it replaces",
	}[linkType])
}

// ---------------------------------------------------------------------------
// The risk register and the decision log

// registerLimit caps the risks or decisions one register or log returns.
const registerLimit = 2000

// riskAssessment reads a risk's likelihood, impact and response from its fields.
func riskAssessment(raw json.RawMessage) (likelihood, impact int, response string) {
	fields := map[string]any{}
	_ = json.Unmarshal(raw, &fields)
	likelihood, _ = riskLevel(fields[FieldRiskLikelihood])
	impact, _ = riskLevel(fields[FieldRiskImpact])
	response, _ = fields[FieldRiskResponse].(string)
	return likelihood, impact, response
}

// kindIssues loads a project's issues of a kind for the register or log, with their views and
// the progress of the work linked to them with linkType.
func (s *Service) kindIssues(ctx context.Context, actor *authz.Actor, ref, kind, linkType string) (dbq.Project, []dbq.Issue, map[uuid.UUID]api.LinkedWork, bool, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return p, nil, nil, false, err
	}
	issues, err := s.Q.ProjectIssuesOfKind(ctx, dbq.ProjectIssuesOfKindParams{ProjectID: p.ID, Kind: kind, Lim: registerLimit + 1})
	if err != nil {
		return p, nil, nil, false, err
	}
	truncated := len(issues) > registerLimit
	if truncated {
		issues = issues[:registerLimit]
	}
	ids := make([]uuid.UUID, len(issues))
	for k, i := range issues {
		ids[k] = i.ID
	}
	rows, err := s.Q.LinkedWorkProgress(ctx, dbq.LinkedWorkProgressParams{Ids: ids, LinkType: linkType})
	if err != nil {
		return p, nil, nil, false, err
	}
	progress := make(map[uuid.UUID]api.LinkedWork, len(rows))
	for _, r := range rows {
		progress[r.TargetID] = api.LinkedWork{Total: int(r.Total), Done: int(r.Done)}
	}
	return p, issues, progress, truncated, nil
}

// RiskRegister lists a project's risks with their scores and mitigating work, and sums up the
// open ones: how many, how many rated high or critical, whose review is overdue, and the
// likelihood × impact matrix.
func (s *Service) RiskRegister(ctx context.Context, actor *authz.Actor, ref string) (api.RiskRegister, error) {
	out := api.RiskRegister{Risks: []api.RiskEntry{}, Matrix: []api.RiskCell{}}
	_, issues, progress, truncated, err := s.kindIssues(ctx, actor, ref, KindRisk, "mitigates")
	if err != nil {
		return out, err
	}
	out.Truncated = truncated
	views, err := s.IssueViews(ctx, issues)
	if err != nil {
		return out, err
	}
	var matrix [5][5]int
	today := dayStart(s.Now())
	for k, i := range issues {
		v := views[k]
		e := api.RiskEntry{Issue: v, Mitigation: progress[i.ID]}
		likelihood, impact, response := riskAssessment(i.Fields)
		if likelihood > 0 {
			e.Likelihood = ptr(likelihood)
		}
		if impact > 0 {
			e.Impact = ptr(impact)
		}
		if likelihood > 0 && impact > 0 {
			e.Score = ptr(likelihood * impact)
			e.Rating = ptr(api.RiskRating(RiskRating(likelihood * impact)))
		}
		if r := api.RiskEntryResponse(response); r.Valid() {
			e.Response = &r
		}
		out.Risks = append(out.Risks, e)
		if v.Status.Category == api.StatusCategoryDone {
			continue
		}
		out.Summary.Open++
		if e.Rating != nil && (*e.Rating == api.RiskRatingHigh || *e.Rating == api.RiskRatingCritical) {
			out.Summary.HighOrCritical++
		}
		if i.DueDate != nil && i.DueDate.Before(today) {
			out.Summary.ReviewsOverdue++
		}
		if e.Score == nil {
			out.Summary.Unassessed++
		} else {
			matrix[likelihood-1][impact-1]++
		}
	}
	for l := 5; l >= 1; l-- {
		for i := 1; i <= 5; i++ {
			out.Matrix = append(out.Matrix, api.RiskCell{Likelihood: l, Impact: i, Score: l * i, Rating: api.RiskRating(RiskRating(l * i)), Count: matrix[l-1][i-1]})
		}
	}
	return out, nil
}

// DecisionLog lists a project's decisions with the decisions they supersede and are superseded
// by, and the work implementing them.
func (s *Service) DecisionLog(ctx context.Context, actor *authz.Actor, ref string) (api.DecisionLog, error) {
	out := api.DecisionLog{Decisions: []api.DecisionEntry{}}
	_, issues, progress, truncated, err := s.kindIssues(ctx, actor, ref, KindDecision, "implements")
	if err != nil {
		return out, err
	}
	out.Truncated = truncated
	ids := make([]uuid.UUID, len(issues))
	for k, i := range issues {
		ids[k] = i.ID
	}
	links, err := s.Q.LinksOfType(ctx, dbq.LinksOfTypeParams{LinkType: "supersedes", Ids: ids})
	if err != nil {
		return out, err
	}
	// Superseding decisions may be beyond the register's limit (or, after moves, elsewhere).
	known := make(map[uuid.UUID]dbq.Issue, len(issues))
	for _, i := range issues {
		known[i.ID] = i
	}
	var missing []uuid.UUID
	for _, l := range links {
		for _, id := range []uuid.UUID{l.SourceID, l.TargetID} {
			if _, ok := known[id]; !ok && !slices.Contains(missing, id) {
				missing = append(missing, id)
			}
		}
	}
	others, err := s.Q.GetIssuesByIDs(ctx, missing)
	if err != nil {
		return out, err
	}
	for _, o := range others {
		known[o.ID] = o
	}
	all := make([]dbq.Issue, 0, len(known))
	all = append(all, issues...)
	all = append(all, others...)
	l, err := s.loadLookups(ctx, all)
	if err != nil {
		return out, err
	}
	for _, i := range issues {
		e := api.DecisionEntry{Issue: l.issue(i), Supersedes: []api.IssueRef{}, SupersededBy: []api.IssueRef{}, Implementation: progress[i.ID]}
		fields := map[string]any{}
		_ = json.Unmarshal(i.Fields, &fields)
		if d, ok := fields[FieldDecisionDate].(string); ok {
			if t, err := time.Parse("2006-01-02", d); err == nil {
				e.DecidedOn = dateView(&t)
			}
		}
		for _, lk := range links {
			switch {
			case lk.SourceID == i.ID:
				if o, ok := known[lk.TargetID]; ok {
					e.Supersedes = append(e.Supersedes, l.ref(o))
				}
			case lk.TargetID == i.ID:
				if o, ok := known[lk.SourceID]; ok {
					e.SupersededBy = append(e.SupersededBy, l.ref(o))
				}
			}
		}
		out.Decisions = append(out.Decisions, e)
	}
	return out, nil
}

// Supersede marks a decision as superseded by a newer one: it links the newer decision with
// supersedes (unless they are linked already) and runs the transition to Superseded, in one
// transaction, so neither happens without the other.
func (s *Service) Supersede(ctx context.Context, actor *authz.Actor, ref, byRef string, version *int, comment string) (dbq.Issue, error) {
	old, p, access, err := s.IssueFor(ctx, actor, ref, authz.IssueTransition)
	if err != nil {
		return old, err
	}
	if !canEdit(access, actor, old) {
		return old, apperr.Forbidden("you cannot edit %s", issueKey(p, old))
	}
	newer, np, _, err := s.IssueFor(ctx, actor, byRef, authz.ProjectBrowse)
	if err != nil {
		return old, apperr.Invalid("supersededByKey", "issue %s not found", byRef)
	}
	if newer.ID == old.ID {
		return old, apperr.Invalid("supersededByKey", "a decision cannot supersede itself")
	}
	if err := checkLinkKinds(ctx, s.Q, "supersedes", newer, old); err != nil {
		return old, apperr.Invalid("supersededByKey", "only a decision can supersede another decision")
	}
	var out dbq.Issue
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		i, err := q.GetIssueForUpdate(ctx, old.ID)
		if err != nil {
			return err
		}
		if version != nil && int32(*version) != i.Version {
			return apperr.Conflict("version_conflict", "%s was changed by someone else; reload and try again", issueKey(p, i))
		}
		links, err := q.IssueLinks(ctx, i.ID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(links, func(l dbq.IssueLink) bool {
			return l.LinkType == "supersedes" && l.SourceID == newer.ID && l.TargetID == i.ID
		}) {
			if _, err := q.CreateIssueLink(ctx, dbq.CreateIssueLinkParams{SourceID: newer.ID, TargetID: i.ID, LinkType: "supersedes", CreatedBy: actor.UserID()}); err != nil {
				return err
			}
			label := fmt.Sprintf("%s %s", LinkLabel("supersedes", false), issueKey(np, newer))
			if err := s.history(ctx, q, i.ID, actor, "link", nil, &label, nil, nil); err != nil {
				return err
			}
			if err := publishOnly(ctx, tx, actor, np, &newer); err != nil {
				return err
			}
		}
		avail, err := s.availableFor(ctx, q, actor, access, p, i)
		if err != nil {
			return err
		}
		k := slices.IndexFunc(avail, func(at AvailableTransition) bool { return strings.EqualFold(at.To.Name, "Superseded") })
		if k < 0 {
			cur, _ := statusInfo(ctx, q, i.StatusID)
			return apperr.Unprocessable("transition_not_available", "%s cannot be superseded from %s; only an accepted decision can be", issueKey(p, i), cur.Name)
		}
		out, err = s.transitionTx(ctx, tx, q, actor, access, p, i, TransitionInput{TransitionID: avail[k].Transition.ID, Comment: comment})
		return err
	})
	return out, err
}
