package jiraimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

type pgxTx = pgx.Tx

// cursor is where a run resumes: the phase, the project (index in the config) and, inside a
// project, the last Jira issue number written (issues are read in key order) or the last
// mapped Jira id handled (relations).
type cursor struct {
	Phase   string `json:"phase"`
	Project int    `json:"project"`
	After   int    `json:"after"`
	AfterID string `json:"afterId,omitempty"`
}

// run is one job execution of a scan or import.
type run struct {
	s      *Service
	imp    dbq.JiraImport
	runID  uuid.UUID
	mode   string
	cfg    Config
	scan   *Scan
	prog   *Progress
	cur    cursor
	client *Client
	site   string
	actor  *authz.Actor

	meta     *metadata
	projects map[int]*projectCtx
	people   map[string]*uuid.UUID
	aliases  map[string]string
	names    map[uuid.UUID]string
	active   map[uuid.UUID]bool
	finger   string
	// Set when the scan column changes in a step.
	scanDirty bool
}

// Work runs a scan or import job: steps until done, cancelled or out of time, checkpointing
// after every step, and hands over to a continuation job when its time budget is spent.
func (s *Service) Work(ctx context.Context, args jobs.JiraImport) error {
	imp, err := s.Q.GetJiraImport(ctx, args.ImportID)
	if err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	if imp.RunID == nil || *imp.RunID != args.RunID || (imp.Status != "scanning" && imp.Status != "running") {
		return nil // superseded, finished or cancelled
	}
	r, err := s.newRun(ctx, imp, args)
	if err != nil {
		return s.finishRun(ctx, imp, args.RunID, "failed", decodeProgress(imp.Progress), imp.Cursor, err)
	}
	deadline := s.Now().Add(s.Budget)
	for {
		done, err := r.step(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err() // shutting down: River retries from the last checkpoint
			}
			s.Log.Warn("jira import step failed", "import", imp.ID, "phase", r.cur.Phase, "error", err)
			raw, _ := json.Marshal(r.cur)
			return s.finishRun(ctx, imp, args.RunID, "failed", r.prog, raw, err)
		}
		if done {
			return r.complete(ctx)
		}
		cancelled, err := r.save(ctx)
		if err != nil {
			if db.IsNoRows(err) {
				return nil // another run took over
			}
			return err
		}
		if cancelled {
			status := "cancelled"
			if r.mode == "scan" {
				status = "draft"
			}
			raw, _ := json.Marshal(r.cur)
			return s.finishRun(ctx, imp, args.RunID, status, r.prog, raw, nil)
		}
		if s.Now().After(deadline) {
			next := args
			next.Seq++
			if s.Jobs == nil {
				return errors.New("no job queue to continue the import")
			}
			_, err := s.Jobs.Insert(ctx, next, nil)
			return err
		}
	}
}

func (s *Service) finishRun(ctx context.Context, imp dbq.JiraImport, runID uuid.UUID, status string, prog *Progress, cur json.RawMessage, cause error) error {
	if prog == nil {
		prog = newProgress(nil)
	}
	var msg *string
	if cause != nil {
		m := cause.Error()
		if strings.Contains(m, "jira") || strings.Contains(m, "Jira") {
			m = strings.TrimPrefix(m, "jira: ")
		}
		msg = &m
	}
	rawProg, _ := json.Marshal(prog)
	if len(cur) == 0 {
		cur = json.RawMessage("{}")
	}
	return s.Q.FinishJiraImportRun(ctx, dbq.FinishJiraImportRunParams{ID: imp.ID, RunID: &runID, Status: status, Error: msg, Progress: rawProg, Cursor: cur})
}

func (s *Service) newRun(ctx context.Context, imp dbq.JiraImport, args jobs.JiraImport) (*run, error) {
	r := &run{
		s: s, imp: imp, runID: args.RunID, mode: args.Mode, cfg: decodeConfig(imp), scan: decodeScan(imp),
		prog: decodeProgress(imp.Progress), site: imp.Site, projects: map[int]*projectCtx{},
		people: map[string]*uuid.UUID{}, aliases: map[string]string{}, names: map[uuid.UUID]string{}, active: map[uuid.UUID]bool{},
	}
	if r.prog == nil {
		r.prog = newProgress(map[string][]Phase{"scan": scanPhases, "import": importPhases}[args.Mode])
	}
	_ = json.Unmarshal(imp.Cursor, &r.cur)
	c, err := s.client(imp)
	if err != nil {
		return nil, err
	}
	r.client = c
	u, err := s.Q.GetUser(ctx, args.ActorID)
	if err != nil || !u.Active {
		return nil, errors.New("the admin who started the run no longer has an active account")
	}
	role, err := s.Tracker.EffectiveOrgRole(ctx, u)
	if err != nil {
		return nil, err
	}
	if role != "admin" {
		return nil, errors.New("the admin who started the run is no longer an organisation admin")
	}
	r.actor = authz.UserActor(u, role)
	if r.mode == "import" {
		if r.scan == nil {
			return nil, errors.New("analyse the projects before importing")
		}
		targets, err := s.targets(ctx, imp, r.cfg)
		if err != nil {
			return nil, err
		}
		r.cfg.ApplyDefaults(r.scan, targets)
		raw, _ := json.Marshal(struct {
			C Config
			V int
		}{r.cfg, mappingVersion})
		r.finger = fmt.Sprintf("%x", hash(raw))
	}
	return r, nil
}

// mappingVersion changes when the way issues are written changes, so re-runs rewrite them.
const mappingVersion = 1

func hash(b []byte) uint64 {
	// FNV-1a: a fingerprint, not a security boundary.
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

func (r *run) step(ctx context.Context) (bool, error) {
	if r.mode == "scan" {
		return r.scanStep(ctx)
	}
	return r.importStep(ctx)
}

// save checkpoints the cursor, progress and (for scans) the partial scan.
func (r *run) save(ctx context.Context) (bool, error) {
	rawCur, _ := json.Marshal(r.cur)
	rawProg, _ := json.Marshal(r.prog)
	params := dbq.SaveJiraImportStepParams{ID: r.imp.ID, RunID: &r.runID, Cursor: rawCur, Progress: rawProg}
	if r.scanDirty {
		params.Scan, _ = json.Marshal(r.scan)
		r.scanDirty = false
	}
	return r.s.Q.SaveJiraImportStep(ctx, params)
}

// complete records the final state of a successful run.
func (r *run) complete(ctx context.Context) error {
	r.prog.finish()
	status := "succeeded"
	if r.mode == "scan" {
		status = "scanned"
		now := r.s.Now().UTC()
		r.scan.Complete, r.scan.ScannedAt = true, &now
		r.scanDirty = true
	}
	r.cur = cursor{Phase: "done"}
	if _, err := r.save(ctx); err != nil && !db.IsNoRows(err) {
		return err
	}
	raw, _ := json.Marshal(r.cur)
	if err := r.s.finishRun(ctx, r.imp, r.runID, status, r.prog, raw, nil); err != nil {
		return err
	}
	if r.mode == "import" {
		tracker.Audit(ctx, r.s.Q, r.actor, "jira_import.complete", "jira_import", r.imp.ID.String(), nil, map[string]any{"counters": r.prog.Counters})
	}
	return nil
}

// ---------------------------------------------------------------------------
// Site metadata, loaded once per job execution

type metadata struct {
	fields     []Field
	statuses   map[string]Status
	priorities map[string]Priority
	// Custom field ids.
	sprintField, rankField, epicLinkField, startDateField, pointsField string
	pointsName                                                         string
}

func (r *run) loadMeta(ctx context.Context) (*metadata, error) {
	if r.meta != nil {
		return r.meta, nil
	}
	m := &metadata{statuses: map[string]Status{}, priorities: map[string]Priority{}}
	var err error
	if m.fields, err = r.client.Fields(ctx); err != nil {
		return nil, err
	}
	for _, f := range m.fields {
		switch {
		case f.Schema.Custom == sprintFieldType && m.sprintField == "":
			m.sprintField = f.ID
		case f.Schema.Custom == rankFieldType && m.rankField == "":
			m.rankField = f.ID
		case f.Schema.Custom == epicLinkFieldType && m.epicLinkField == "":
			m.epicLinkField = f.ID
		case f.Custom && f.Schema.Type == "date" && strings.EqualFold(f.Name, "start date") && m.startDateField == "":
			m.startDateField = f.ID
		}
	}
	if r.cfg.StoryPointsField != "" && r.cfg.StoryPointsField != "none" {
		for _, f := range m.fields {
			if f.ID == r.cfg.StoryPointsField {
				m.pointsField, m.pointsName = f.ID, f.Name
			}
		}
	}
	sts, err := r.client.Statuses(ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range sts {
		m.statuses[st.ID] = st
	}
	prs, err := r.client.Priorities(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range prs {
		m.priorities[p.ID] = p
	}
	r.meta = m
	return m, nil
}

// customFields are the custom fields the importer reads from search results.
func (m *metadata) customFields() []string {
	var out []string
	for _, f := range []string{m.sprintField, m.rankField, m.epicLinkField, m.startDateField, m.pointsField} {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func jqlProject(key string, after int) string {
	q := fmt.Sprintf(`project = "%s"`, key)
	if after > 0 {
		q += fmt.Sprintf(` AND key > "%s-%d"`, key, after)
	}
	return q + " ORDER BY key ASC"
}

func dateOnly(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}
