// Package jiraimport imports projects from Jira Cloud (REST v3) and Jira Data Center / Server
// (REST v2): issues with their keys and numbers, comments, attachments, links, sprints, versions
// and the changelog, which becomes issue history so cycle time, cumulative flow and burndowns
// work for the imported past.
//
// An import is a wizard: connect (credentials sealed at rest), choose projects and their
// targets, scan them (counts, values in use, people), map values, dry-run, then import. Scans
// and imports are background jobs that work in small checkpointed steps; a mapping table from
// Jira ids to ids here makes re-runs update or skip instead of duplicating, and lets an
// interrupted run resume.
package jiraimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
	Box     *secure.Box
	Cfg     *config.Config
	Tracker *tracker.Service
	Jobs    jobs.Enqueuer
	Log     *slog.Logger
	// HTTP calls Jira (SSRF-guarded; WFM_OUTBOUND_ALLOWED_HOSTS admits a private Data Center).
	HTTP *http.Client
	// Download fetches attachment content (longer timeout).
	Download *http.Client
	Now      func() time.Time
	// Budget is how long one job execution works before a continuation job takes over.
	Budget time.Duration
	// Sleep waits between retries of rate-limited calls (tests shorten it).
	Sleep func(ctx context.Context, d time.Duration) error
}

func New(pool *pgxpool.Pool, box *secure.Box, cfg *config.Config, tr *tracker.Service, enq jobs.Enqueuer, log *slog.Logger) *Service {
	return &Service{
		Pool: pool, Q: dbq.New(pool), Box: box, Cfg: cfg, Tracker: tr, Jobs: enq, Log: log,
		HTTP:     netguard.Client(90*time.Second, cfg.WebhookAllowPrivate, cfg.OutboundAllowedHosts...),
		Download: netguard.Client(15*time.Minute, cfg.WebhookAllowPrivate, cfg.OutboundAllowedHosts...),
		Now:      time.Now, Budget: 8 * time.Minute,
	}
}

// stallAfter: a run without a checkpoint for this long is treated as interrupted.
const stallAfter = 5 * time.Minute

// ---------------------------------------------------------------------------
// Connection

type ConnectionInput struct {
	Flavor  string
	BaseURL string
	Email   string
	Token   string
}

type ConnectionTest struct {
	ServerTitle    string `json:"serverTitle"`
	Version        string `json:"version"`
	DeploymentType string `json:"deploymentType"`
	User           struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email,omitempty"`
	} `json:"user"`
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return "", apperr.Invalid("baseUrl", "enter the site URL, e.g. https://acme.atlassian.net")
	}
	path := strings.TrimRight(u.Path, "/")
	// People paste a browse or board URL; keep a Data Center context path such as /jira.
	for _, cut := range []string{"/browse/", "/secure/", "/jira/software", "/projects/", "/issues/", "/rest/"} {
		if k := strings.Index(path+"/", cut); k >= 0 {
			path = path[:k]
		}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(path, "/"), nil
}

func (in *ConnectionInput) validate() error {
	if in.Flavor != Cloud && in.Flavor != DataCenter {
		return apperr.Invalid("flavor", "choose Jira Cloud or Jira Data Center / Server")
	}
	base, err := normalizeBaseURL(in.BaseURL)
	if err != nil {
		return err
	}
	in.BaseURL = base
	in.Email = strings.TrimSpace(in.Email)
	in.Token = strings.TrimSpace(in.Token)
	if in.Flavor == Cloud && in.Email == "" {
		return apperr.Invalid("email", "Jira Cloud needs the email of the Atlassian account that owns the API token")
	}
	if in.Token == "" {
		return apperr.Invalid("token", "a token is required")
	}
	return nil
}

func (s *Service) newClient(flavor, base, email, token string) *Client {
	return &Client{Flavor: flavor, BaseURL: base, Email: email, Token: token, HTTP: s.HTTP, Download: s.Download, Sleep: s.Sleep}
}

// testConnection checks the site answers, is the chosen flavor and accepts the credentials.
func (s *Service) testConnection(ctx context.Context, in ConnectionInput) (ConnectionTest, error) {
	var out ConnectionTest
	if err := in.validate(); err != nil {
		return out, err
	}
	c := s.newClient(in.Flavor, in.BaseURL, in.Email, in.Token)
	c.Retries = 1
	info, err := c.ServerInfo(ctx)
	if err != nil {
		return out, connectionError(err)
	}
	cloud := strings.EqualFold(info.DeploymentType, "cloud")
	switch {
	case cloud && in.Flavor != Cloud:
		return out, apperr.Invalid("flavor", "this is a Jira Cloud site: choose Jira Cloud")
	case !cloud && info.DeploymentType != "" && in.Flavor == Cloud:
		return out, apperr.Invalid("flavor", "this is Jira %s: choose Jira Data Center / Server", info.DeploymentType)
	}
	me, err := c.Myself(ctx)
	if err != nil {
		return out, connectionError(err)
	}
	out.ServerTitle, out.Version, out.DeploymentType = cmpOr(info.ServerTitle, "Jira"), info.Version, cmpOr(info.DeploymentType, "Server")
	out.User.DisplayName, out.User.Email = me.DisplayName, me.EmailAddress
	return out, nil
}

// connectionError explains a failed connection to the admin.
func connectionError(err error) error {
	var ae *APIError
	switch {
	case errors.As(err, &ae):
		return apperr.Unprocessable("jira_connection", "%s", ae.Error())
	case errors.Is(err, netguard.ErrBlocked):
		return apperr.Unprocessable("jira_connection", "the Jira address is on a private network; add its host to WFM_OUTBOUND_ALLOWED_HOSTS")
	}
	return apperr.Unprocessable("jira_connection", "could not reach Jira: %v", err)
}

func (s *Service) TestConnection(ctx context.Context, actor *authz.Actor, in ConnectionInput) (ConnectionTest, error) {
	if !actor.IsOrgAdmin() {
		return ConnectionTest{}, apperr.Forbidden("organisation admin role required")
	}
	return s.testConnection(ctx, in)
}

func siteOf(flavor, base string) string { return flavor + ":" + base }

// Create tests the connection and stores it (token sealed) as a new import.
func (s *Service) Create(ctx context.Context, actor *authz.Actor, in ConnectionInput) (dbq.JiraImport, error) {
	if !actor.IsOrgAdmin() {
		return dbq.JiraImport{}, apperr.Forbidden("organisation admin role required")
	}
	if err := in.validate(); err != nil {
		return dbq.JiraImport{}, err
	}
	test, err := s.testConnection(ctx, in)
	if err != nil {
		return dbq.JiraImport{}, err
	}
	sealed := s.Box.Seal([]byte(in.Token))
	email := ""
	if in.Flavor == Cloud {
		email = in.Email
	}
	imp, err := s.Q.CreateJiraImport(ctx, dbq.CreateJiraImportParams{
		Flavor: in.Flavor, BaseUrl: in.BaseURL, Site: siteOf(in.Flavor, in.BaseURL), Email: email, TokenEnc: &sealed,
		ServerTitle: test.ServerTitle, CreatedBy: actor.UserID(),
	})
	if err != nil {
		return imp, err
	}
	cfg, _ := json.Marshal(DefaultConfig())
	if imp, err = s.Q.UpdateJiraImportConfig(ctx, dbq.UpdateJiraImportConfigParams{ID: imp.ID, Config: cfg}); err != nil {
		return imp, err
	}
	tracker.Audit(ctx, s.Q, actor, "jira_import.create", "jira_import", imp.ID.String(), nil, map[string]any{"site": in.BaseURL, "flavor": in.Flavor})
	return imp, nil
}

func (s *Service) load(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.JiraImport, error) {
	if !actor.IsOrgAdmin() {
		return dbq.JiraImport{}, apperr.Forbidden("organisation admin role required")
	}
	imp, err := s.Q.GetJiraImport(ctx, id)
	if err != nil {
		return imp, apperr.NotFound("import")
	}
	return imp, nil
}

func (s *Service) client(imp dbq.JiraImport) (*Client, error) {
	if imp.TokenEnc == nil {
		return nil, apperr.Conflict("no_credentials", "the Jira credentials were deleted; enter a token again to continue")
	}
	tok, err := s.Box.Open(*imp.TokenEnc)
	if err != nil {
		return nil, fmt.Errorf("open jira token: %w", err)
	}
	return s.newClient(imp.Flavor, imp.BaseUrl, imp.Email, string(tok)), nil
}

// busy reports a scan or import in progress (and not stalled).
func (s *Service) busy(imp dbq.JiraImport) bool {
	if imp.Status != "scanning" && imp.Status != "running" {
		return false
	}
	return imp.HeartbeatAt == nil || s.Now().Sub(*imp.HeartbeatAt) < stallAfter
}

func (s *Service) UpdateConnection(ctx context.Context, actor *authz.Actor, id uuid.UUID, email, token string) (dbq.JiraImport, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return imp, err
	}
	if s.busy(imp) {
		return imp, apperr.Conflict("import_running", "wait for the running scan or import to finish")
	}
	in := ConnectionInput{Flavor: imp.Flavor, BaseURL: imp.BaseUrl, Email: cmpOr(strings.TrimSpace(email), imp.Email), Token: token}
	test, err := s.testConnection(ctx, in)
	if err != nil {
		return imp, err
	}
	sealed := s.Box.Seal([]byte(in.Token))
	if in.Flavor != Cloud {
		in.Email = ""
	}
	imp, err = s.Q.UpdateJiraImportConnection(ctx, dbq.UpdateJiraImportConnectionParams{ID: id, Email: in.Email, TokenEnc: &sealed, ServerTitle: test.ServerTitle})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "jira_import.credentials", "jira_import", id.String(), nil, nil)
	}
	return imp, err
}

// DeleteCredentials forgets the token; the import record and its mapping stay.
func (s *Service) DeleteCredentials(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.JiraImport, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return imp, err
	}
	if s.busy(imp) {
		return imp, apperr.Conflict("import_running", "wait for the running scan or import to finish")
	}
	imp, err = s.Q.ClearJiraImportCredentials(ctx, id)
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "jira_import.credentials_delete", "jira_import", id.String(), nil, nil)
	}
	return imp, err
}

// Delete removes the import record. The id mapping of the site stays, so importing the same
// site again later still updates instead of duplicating.
func (s *Service) Delete(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return err
	}
	if s.busy(imp) {
		return apperr.Conflict("import_running", "cancel the running scan or import first")
	}
	if err := s.Q.DeleteJiraImport(ctx, id); err != nil {
		return err
	}
	tracker.Audit(ctx, s.Q, actor, "jira_import.delete", "jira_import", id.String(), nil, map[string]any{"site": imp.BaseUrl})
	return nil
}

// ---------------------------------------------------------------------------
// Jira projects

type JiraProject struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	TeamManaged bool   `json:"teamManaged"`
	Lead        string `json:"lead,omitempty"`
	// Key here of the project an earlier import of this site created.
	ImportedAs string `json:"importedAs,omitempty"`
}

func (s *Service) JiraProjects(ctx context.Context, actor *authz.Actor, id uuid.UUID) ([]JiraProject, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	c, err := s.client(imp)
	if err != nil {
		return nil, err
	}
	list, err := c.Projects(ctx)
	if err != nil {
		return nil, connectionError(err)
	}
	mapped, err := s.Q.JiraMappedProjects(ctx, imp.Site)
	if err != nil {
		return nil, err
	}
	out := make([]JiraProject, 0, len(list))
	for _, p := range list {
		v := JiraProject{ID: p.ID, Key: p.Key, Name: p.Name, Type: p.ProjectTypeKey, TeamManaged: p.Style == "next-gen"}
		if p.Lead != nil {
			v.Lead = p.Lead.DisplayName
		}
		for _, m := range mapped {
			if m.JiraID == p.ID {
				v.ImportedAs = m.Key
			}
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b JiraProject) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

// ---------------------------------------------------------------------------
// Views

type UserRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"displayName"`
	Email       string    `json:"email"`
	Active      bool      `json:"active"`
}

// PersonView is a Jira person from the scan with the account it maps to by default.
type PersonView struct {
	ScanPerson
	// Existing account with the same email, or the account an earlier run mapped them to.
	Match *UserRef `json:"match,omitempty"`
	// The match is a placeholder from an earlier import that nobody has claimed yet.
	Placeholder bool `json:"placeholder"`
}

type View struct {
	ID             uuid.UUID    `json:"id"`
	Flavor         string       `json:"flavor"`
	BaseURL        string       `json:"baseUrl"`
	Email          string       `json:"email"`
	ServerTitle    string       `json:"serverTitle"`
	HasCredentials bool         `json:"hasCredentials"`
	Status         string       `json:"status"`
	Mode           *string      `json:"mode,omitempty"`
	Stalled        bool         `json:"stalled"`
	ScanStale      bool         `json:"scanStale"`
	Config         Config       `json:"config"`
	Targets        []Target     `json:"targets"`
	Scan           *Scan        `json:"scan,omitempty"`
	People         []PersonView `json:"people"`
	Progress       *Progress    `json:"progress,omitempty"`
	Error          *string      `json:"error,omitempty"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
	StartedAt      *time.Time   `json:"startedAt,omitempty"`
	FinishedAt     *time.Time   `json:"finishedAt,omitempty"`
}

func decodeConfig(imp dbq.JiraImport) Config {
	cfg := DefaultConfig()
	if len(imp.Config) > 0 {
		_ = json.Unmarshal(imp.Config, &cfg)
	}
	if cfg.Projects == nil {
		cfg.Projects = []ProjectTarget{}
	}
	return cfg
}

func decodeScan(imp dbq.JiraImport) *Scan {
	if len(imp.Scan) == 0 {
		return nil
	}
	var sc Scan
	if json.Unmarshal(imp.Scan, &sc) != nil {
		return nil
	}
	return &sc
}

func decodeProgress(raw []byte) *Progress {
	if len(raw) == 0 {
		return nil
	}
	var p Progress
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	if p.Counters == nil {
		p.Counters = map[string]int{}
	}
	if p.Problems == nil {
		p.Problems = []Problem{}
	}
	return &p
}

// scanStale reports a scan that does not cover the selected projects.
func scanStale(cfg Config, sc *Scan) bool {
	if sc == nil || !sc.Complete {
		return true
	}
	if len(sc.Projects) != len(cfg.Projects) {
		return true
	}
	for _, p := range cfg.Projects {
		if sc.project(p.JiraID) == nil {
			return true
		}
	}
	return false
}

func (s *Service) Get(ctx context.Context, actor *authz.Actor, id uuid.UUID) (View, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, imp, true)
}

// ViewOf is the full view of an import record.
func (s *Service) ViewOf(ctx context.Context, imp dbq.JiraImport) (View, error) {
	return s.view(ctx, imp, true)
}

func (s *Service) List(ctx context.Context, actor *authz.Actor) ([]View, error) {
	if !actor.IsOrgAdmin() {
		return nil, apperr.Forbidden("organisation admin role required")
	}
	list, err := s.Q.ListJiraImports(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(list))
	for _, imp := range list {
		v, err := s.view(ctx, imp, false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) view(ctx context.Context, imp dbq.JiraImport, full bool) (View, error) {
	cfg := decodeConfig(imp)
	sc := decodeScan(imp)
	v := View{
		ID: imp.ID, Flavor: imp.Flavor, BaseURL: imp.BaseUrl, Email: imp.Email, ServerTitle: imp.ServerTitle,
		HasCredentials: imp.TokenEnc != nil, Status: imp.Status, Mode: imp.Mode, Error: imp.Error,
		CreatedAt: imp.CreatedAt, UpdatedAt: imp.UpdatedAt, StartedAt: imp.StartedAt, FinishedAt: imp.FinishedAt,
		Progress: decodeProgress(imp.Progress), Config: cfg, Targets: []Target{}, People: []PersonView{},
	}
	v.Stalled = (imp.Status == "scanning" || imp.Status == "running") && !s.busy(imp)
	v.ScanStale = scanStale(cfg, sc)
	if !full {
		return v, nil
	}
	v.Scan = sc
	targets, err := s.targets(ctx, imp, cfg)
	if err != nil {
		return v, err
	}
	v.Targets = targets
	v.Config.ApplyDefaults(sc, targets)
	if sc != nil {
		if v.People, err = s.people(ctx, imp, sc.People); err != nil {
			return v, err
		}
	}
	return v, nil
}

// people pairs scanned Jira people with accounts here: mapped by an earlier run, else by email.
func (s *Service) people(ctx context.Context, imp dbq.JiraImport, list []ScanPerson) ([]PersonView, error) {
	mapped, err := s.Q.JiraMappedUsers(ctx, imp.Site)
	if err != nil {
		return nil, err
	}
	byJira := map[string]dbq.JiraMappedUsersRow{}
	for _, m := range mapped {
		byJira[m.JiraID] = m
	}
	var emails []string
	for _, p := range list {
		if p.Email != "" {
			emails = append(emails, p.Email)
		}
	}
	users, err := s.Q.UsersByEmails(ctx, emails)
	if err != nil {
		return nil, err
	}
	byEmail := map[string]dbq.User{}
	for _, u := range users {
		byEmail[u.Email] = u
	}
	out := make([]PersonView, 0, len(list))
	for _, p := range list {
		pv := PersonView{ScanPerson: p}
		if m, ok := byJira[p.ID]; ok {
			pv.Match = &UserRef{ID: m.ID, DisplayName: m.DisplayName, Email: m.Email, Active: m.Active}
			pv.Placeholder = m.Placeholder
		} else if u, ok := byEmail[p.Email]; ok {
			pv.Match = &UserRef{ID: u.ID, DisplayName: u.DisplayName, Email: u.Email, Active: u.Active}
			pv.Placeholder, _ = s.Q.IsUserPlaceholder(ctx, u.ID)
		}
		out = append(out, pv)
	}
	slices.SortStableFunc(out, func(a, b PersonView) int { return b.Count - a.Count })
	return out, nil
}

// targets lists, per selected project, the statuses and issue types it offers: those of the
// project an earlier run created, else of the chosen template.
func (s *Service) targets(ctx context.Context, imp dbq.JiraImport, cfg Config) ([]Target, error) {
	mapped, err := s.Q.JiraMappedProjects(ctx, imp.Site)
	if err != nil {
		return nil, err
	}
	out := make([]Target, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		var t Target
		found := false
		for _, m := range mapped {
			if m.JiraID == p.JiraID {
				if t, err = s.projectTarget(ctx, m.ID); err != nil {
					return nil, err
				}
				t.ExistingKey, found = m.Key, true
			}
		}
		if !found {
			t, _ = templateTarget(cmpOr(p.Template, "scrum"))
		}
		t.JiraID = p.JiraID
		out = append(out, t)
	}
	return out, nil
}

// projectTarget reads an existing project's issue types and their workflows' statuses.
func (s *Service) projectTarget(ctx context.Context, projectID uuid.UUID) (Target, error) {
	types, err := s.Q.ListIssueTypes(ctx, projectID)
	if err != nil {
		return Target{}, err
	}
	t := Target{}
	seen := map[string]bool{}
	slices.SortStableFunc(types, func(a, b dbq.ListIssueTypesRow) int { return absLevel(int(a.Level)) - absLevel(int(b.Level)) })
	for _, it := range types {
		// Imported issues are work: they keep their sprints and parents, which risks and
		// decisions never have.
		if it.Kind != tracker.KindWork {
			continue
		}
		def, _, err := s.workflowDef(ctx, it.WorkflowID)
		if err != nil {
			return t, err
		}
		tt := TargetType{Key: it.Key, Name: it.Name, Level: int(it.Level)}
		for _, st := range def.Statuses {
			tt.Statuses = append(tt.Statuses, st.Name)
			if !seen[strings.ToLower(st.Name)] {
				seen[strings.ToLower(st.Name)] = true
				t.Statuses = append(t.Statuses, TargetStatus{Name: st.Name, Category: st.Category})
			}
		}
		t.Types = append(t.Types, tt)
	}
	sortStatuses(t.Statuses)
	return t, nil
}

func (s *Service) workflowDef(ctx context.Context, workflowID uuid.UUID) (*workflow.Definition, dbq.WorkflowVersion, error) {
	wf, err := s.Q.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, dbq.WorkflowVersion{}, err
	}
	wv, err := s.Q.GetWorkflowVersion(ctx, dbq.GetWorkflowVersionParams{WorkflowID: workflowID, Version: wf.CurrentVersion})
	if err != nil {
		return nil, wv, err
	}
	var def workflow.Definition
	if err := json.Unmarshal(wv.Definition, &def); err != nil {
		return nil, wv, err
	}
	return &def, wv, nil
}

// ---------------------------------------------------------------------------
// Configuration

var projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

var templates = []string{"scrum", "kanban", "bug_tracking"}

// UpdateConfig saves the wizard's choices. Projects without a template get one from their Jira
// boards (scrum board → Scrum, otherwise Kanban).
func (s *Service) UpdateConfig(ctx context.Context, actor *authz.Actor, id uuid.UUID, cfg Config) (dbq.JiraImport, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return imp, err
	}
	if s.busy(imp) {
		return imp, apperr.Conflict("import_running", "wait for the running scan or import to finish")
	}
	if err := s.validateConfig(ctx, imp, &cfg); err != nil {
		return imp, err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return imp, err
	}
	return s.Q.UpdateJiraImportConfig(ctx, dbq.UpdateJiraImportConfigParams{ID: id, Config: raw})
}

func (s *Service) validateConfig(ctx context.Context, imp dbq.JiraImport, cfg *Config) error {
	if cfg.Projects == nil {
		cfg.Projects = []ProjectTarget{}
	}
	if cfg.Priorities == nil {
		cfg.Priorities = map[string]string{}
	}
	if cfg.People == nil {
		cfg.People = map[string]PersonMapping{}
	}
	mapped, err := s.Q.JiraMappedProjects(ctx, imp.Site)
	if err != nil {
		return err
	}
	keys := map[string]bool{}
	jiraIDs := map[string]bool{}
	var c *Client
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		field := fmt.Sprintf("projects[%d]", i)
		if p.JiraID == "" || p.JiraKey == "" {
			return apperr.Invalid(field+".jiraId", "a project needs its Jira id and key")
		}
		if jiraIDs[p.JiraID] {
			return apperr.Invalid(field+".jiraId", "%s is selected twice", p.JiraKey)
		}
		jiraIDs[p.JiraID] = true
		var existing *dbq.JiraMappedProjectsRow
		for k := range mapped {
			if mapped[k].JiraID == p.JiraID {
				existing = &mapped[k]
			}
		}
		if existing != nil {
			// The project exists from an earlier run: its key, name and template stay.
			p.Key, p.Name, p.Template = existing.Key, existing.Name, existing.Template
		}
		p.Key = strings.ToUpper(strings.TrimSpace(p.Key))
		if p.Key == "" {
			p.Key = defaultKey(p.JiraKey)
		}
		if !projectKeyPattern.MatchString(p.Key) {
			return apperr.Invalid(field+".key", "%s: project keys are 2-10 uppercase letters or digits, starting with a letter", p.Key)
		}
		if keys[p.Key] {
			return apperr.Invalid(field+".key", "the key %s is used for two projects", p.Key)
		}
		keys[p.Key] = true
		if existing == nil {
			if used, err := s.keyUsed(ctx, p.Key); err != nil {
				return err
			} else if used {
				return apperr.Invalid(field+".key", "the key %s is already used by a project here; choose another", p.Key)
			}
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			p.Name = cmpOr(strings.TrimSpace(p.JiraName), p.JiraKey)
		}
		if p.Template == "" {
			if c == nil {
				if c, err = s.client(imp); err != nil {
					return err
				}
			}
			p.Template = s.detectTemplate(ctx, c, p.JiraKey)
		}
		if !slices.Contains(templates, p.Template) {
			return apperr.Invalid(field+".template", "unknown template %q", p.Template)
		}
		if p.Statuses == nil {
			p.Statuses = map[string]string{}
		}
		if p.Types == nil {
			p.Types = map[string]string{}
		}
	}
	for jid, v := range cfg.Priorities {
		if !slices.Contains(priorityNames, v) {
			return apperr.Invalid("priorities."+jid, "unknown priority %q", v)
		}
	}
	for jid, pm := range cfg.People {
		switch pm.Action {
		case PersonAuto, PersonSkip:
		case PersonUser:
			if pm.UserID == nil {
				return apperr.Invalid("people."+jid, "choose the user")
			}
			if _, err := s.Q.GetUser(ctx, *pm.UserID); err != nil {
				return apperr.Invalid("people."+jid, "user not found")
			}
		case PersonPlaceholder:
			pm.Email = strings.ToLower(strings.TrimSpace(pm.Email))
			if pm.Email != "" && !strings.Contains(pm.Email, "@") {
				return apperr.Invalid("people."+jid, "%q is not an email address", pm.Email)
			}
			cfg.People[jid] = pm
		default:
			return apperr.Invalid("people."+jid, "unknown action %q", pm.Action)
		}
	}
	return nil
}

func (s *Service) keyUsed(ctx context.Context, key string) (bool, error) {
	if _, err := s.Q.GetProjectByKey(ctx, key); err == nil {
		return true, nil
	} else if !db.IsNoRows(err) {
		return false, err
	}
	if _, err := s.Q.GetProjectByAlias(ctx, key); err == nil {
		return true, nil
	} else if !db.IsNoRows(err) {
		return false, err
	}
	return false, nil
}

// detectTemplate: Scrum when the project has a scrum board, Bug tracking for service
// projects, else Kanban.
func (s *Service) detectTemplate(ctx context.Context, c *Client, key string) string {
	boards, err := c.Boards(ctx, key)
	if err != nil {
		return "scrum"
	}
	for _, b := range boards {
		if b.Type == "scrum" {
			return "scrum"
		}
	}
	if len(boards) == 0 {
		if p, err := c.Project(ctx, key); err == nil && p.ProjectTypeKey == "service_desk" {
			return "bug_tracking"
		}
	}
	return "kanban"
}

// ---------------------------------------------------------------------------
// Runs

func (s *Service) startRun(ctx context.Context, actor *authz.Actor, imp dbq.JiraImport, mode, status string, cur cursor, prog *Progress) (dbq.JiraImport, error) {
	// Two runs writing the same site's mapping at once would race; one at a time.
	if n, err := s.Q.BusyJiraImportsOfSite(ctx, dbq.BusyJiraImportsOfSiteParams{Site: imp.Site, ID: imp.ID, FreshAfter: ptrTime(s.Now().Add(-stallAfter))}); err != nil {
		return imp, err
	} else if n > 0 {
		return imp, apperr.Conflict("import_running", "another import of this Jira site is running; wait for it to finish")
	}
	runID := uuid.New()
	rawCur, _ := json.Marshal(cur)
	rawProg, _ := json.Marshal(prog)
	var out dbq.JiraImport
	err := db.Tx(ctx, s.Pool, func(tx pgxTx, q *dbq.Queries) error {
		var err error
		out, err = q.StartJiraImportRun(ctx, dbq.StartJiraImportRunParams{ID: imp.ID, Status: status, Mode: &mode, RunID: &runID, Cursor: rawCur, Progress: rawProg})
		if err != nil {
			return err
		}
		if s.Jobs == nil {
			return nil
		}
		_, err = s.Jobs.InsertTx(ctx, tx, jobs.JiraImport{ImportID: imp.ID, RunID: runID, Mode: mode, ActorID: *actor.UserID()}, nil)
		return err
	})
	return out, err
}

// Scan starts analysing the selected projects.
func (s *Service) Scan(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.JiraImport, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return imp, err
	}
	if s.busy(imp) {
		return imp, apperr.Conflict("import_running", "a scan or import is already running")
	}
	if _, err := s.client(imp); err != nil {
		return imp, err
	}
	if len(decodeConfig(imp).Projects) == 0 {
		return imp, apperr.Invalid("projects", "choose at least one project")
	}
	return s.startRun(ctx, actor, imp, "scan", "scanning", cursor{}, newProgress(scanPhases))
}

// Import starts (or resumes) the import. An interrupted, failed or cancelled import resumes
// from its last checkpoint; everything is idempotent, so a finished one simply re-syncs.
func (s *Service) Import(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.JiraImport, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return imp, err
	}
	if s.busy(imp) {
		return imp, apperr.Conflict("import_running", "a scan or import is already running")
	}
	dry, err := s.dryRun(ctx, imp)
	if err != nil {
		return imp, err
	}
	if !dry.Ready {
		e := apperr.Conflict("import_blocked", "fix the problems the dry run reports first")
		for _, p := range dry.Problems {
			if p.Severity == "error" {
				e.Fields = append(e.Fields, apperr.FieldError{Message: p.Message})
			}
		}
		return imp, e
	}
	cur, prog := cursor{}, newProgress(importPhases)
	resume := imp.Mode != nil && *imp.Mode == "import" && imp.Status != "succeeded"
	if resume {
		_ = json.Unmarshal(imp.Cursor, &cur)
		if p := decodeProgress(imp.Progress); p != nil {
			prog = p
		}
	}
	out, err := s.startRun(ctx, actor, imp, "import", "running", cur, prog)
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "jira_import.run", "jira_import", id.String(), nil, map[string]any{"resume": resume})
	}
	return out, err
}

// Cancel asks the running job to stop after its current step. A stalled run stops at once.
func (s *Service) Cancel(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.JiraImport, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return imp, err
	}
	if imp.Status != "scanning" && imp.Status != "running" {
		return imp, apperr.Conflict("not_running", "nothing is running")
	}
	if !s.busy(imp) && imp.RunID != nil {
		status := "cancelled"
		if imp.Status == "scanning" {
			status = "draft"
		}
		if err := s.Q.FinishJiraImportRun(ctx, dbq.FinishJiraImportRunParams{ID: id, RunID: imp.RunID, Status: status, Progress: imp.Progress, Cursor: imp.Cursor}); err != nil {
			return imp, err
		}
		return s.Q.GetJiraImport(ctx, id)
	}
	return s.Q.RequestJiraImportCancel(ctx, id)
}

func ptrTime(t time.Time) *time.Time { return &t }
