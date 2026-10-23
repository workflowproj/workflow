package scm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/secure"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

var webhooksReceived = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "wfm_scm_webhooks_received_total", Help: "Inbound SCM webhooks by provider, event and result.",
}, []string{"provider", "event", "result"})

type secrets struct {
	PrivateKey    string `json:"privateKey,omitempty"`
	Token         string `json:"token,omitempty"`
	WebhookSecret string `json:"webhookSecret"`
}

type Service struct {
	Pool    *pgxpool.Pool
	Q       *dbq.Queries
	Box     *secure.Box
	Cfg     *config.Config
	Tracker *tracker.Service
	Jobs    jobs.Enqueuer
	Log     *slog.Logger
	HTTP    *http.Client
	// Providers by name; tests may substitute fakes.
	Providers map[string]Provider
}

func NewService(pool *pgxpool.Pool, box *secure.Box, cfg *config.Config, tr *tracker.Service, enq jobs.Enqueuer, log *slog.Logger) *Service {
	return &Service{
		Pool: pool, Q: dbq.New(pool), Box: box, Cfg: cfg, Tracker: tr, Jobs: enq, Log: log,
		HTTP:      &http.Client{Timeout: 20 * time.Second},
		Providers: map[string]Provider{"github": GitHub{}, "gitlab": GitLab{}},
	}
}

func (s *Service) WebhookURL(c dbq.ScmConnection) string {
	return fmt.Sprintf("%s/hooks/%s/%s", s.Cfg.HooksPublicURL, c.Provider, c.ID)
}

func (s *Service) decrypt(c dbq.ScmConnection) (Connection, error) {
	raw, err := s.Box.Open(c.SecretsEnc)
	if err != nil {
		return Connection{}, fmt.Errorf("decrypt connection secrets: %w", err)
	}
	var sec secrets
	if err := json.Unmarshal(raw, &sec); err != nil {
		return Connection{}, err
	}
	out := Connection{ID: c.ID.String(), Provider: c.Provider, APIURL: c.ApiUrl, WebURL: c.WebUrl, PrivateKey: sec.PrivateKey, Token: sec.Token, WebhookSecret: sec.WebhookSecret}
	if c.AppID != nil {
		out.AppID = *c.AppID
	}
	return out, nil
}

func (s *Service) client(ctx context.Context, c dbq.ScmConnection) (Client, error) {
	conn, err := s.decrypt(c)
	if err != nil {
		return nil, err
	}
	p, ok := s.Providers[c.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %s", c.Provider)
	}
	return p.Client(ctx, conn, s.HTTP)
}

// ---------------------------------------------------------------------------
// Connections (org admins)

type ConnectionInput struct {
	Provider      string
	Name          string
	APIURL        string
	WebURL        string
	AppID         *int64
	AppSlug       *string
	PrivateKey    string
	Token         string
	WebhookSecret string
	ManageHooks   bool
	WriteBack     bool
}

func (s *Service) CreateConnection(ctx context.Context, actor *authz.Actor, in ConnectionInput) (dbq.ScmConnection, error) {
	if !actor.IsOrgAdmin() {
		return dbq.ScmConnection{}, apperr.Forbidden("only organisation admins can connect GitHub or GitLab")
	}
	switch in.Provider {
	case "github":
		if in.APIURL == "" {
			in.APIURL = "https://api.github.com"
		}
		if in.WebURL == "" {
			in.WebURL = "https://github.com"
		}
		if in.AppID == nil || in.PrivateKey == "" {
			return dbq.ScmConnection{}, apperr.Invalid("appId", "a GitHub App id and private key are required")
		}
	case "gitlab":
		if in.APIURL == "" {
			in.APIURL = "https://gitlab.com"
		}
		if in.WebURL == "" {
			in.WebURL = in.APIURL
		}
		if in.Token == "" {
			return dbq.ScmConnection{}, apperr.Invalid("token", "a GitLab group or project access token is required")
		}
	default:
		return dbq.ScmConnection{}, apperr.Invalid("provider", "provider must be github or gitlab")
	}
	for _, u := range []string{in.APIURL, in.WebURL} {
		if pu, err := url.Parse(u); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
			return dbq.ScmConnection{}, apperr.Invalid("apiUrl", "%q is not a valid URL", u)
		}
	}
	if in.WebhookSecret == "" {
		in.WebhookSecret = secure.Token("", 24)
	}
	raw, _ := json.Marshal(secrets{PrivateKey: in.PrivateKey, Token: in.Token, WebhookSecret: in.WebhookSecret})
	c, err := s.Q.CreateScmConnection(ctx, dbq.CreateScmConnectionParams{
		Provider: in.Provider, Name: strings.TrimSpace(in.Name), ApiUrl: strings.TrimRight(in.APIURL, "/"), WebUrl: strings.TrimRight(in.WebURL, "/"),
		AppID: in.AppID, AppSlug: in.AppSlug, SecretsEnc: s.Box.Seal(raw), ManageHooks: in.ManageHooks, WriteBack: in.WriteBack, CreatedBy: actor.UserID(),
	})
	if err != nil {
		return c, err
	}
	tracker.Audit(ctx, s.Q, actor, "scm.connection_create", "scm_connection", c.ID.String(), nil, map[string]any{"provider": c.Provider, "name": c.Name})
	if s.Jobs != nil {
		if _, err := s.Jobs.Insert(ctx, jobs.SCMReconcile{ConnectionID: &c.ID}, nil); err != nil {
			s.Log.Warn("could not queue initial repository discovery", "error", err)
		}
	}
	return c, nil
}

type ConnectionPatch struct {
	Name          *string
	PrivateKey    *string
	Token         *string
	WebhookSecret *string
	ManageHooks   *bool
	WriteBack     *bool
}

func (s *Service) UpdateConnection(ctx context.Context, actor *authz.Actor, id uuid.UUID, in ConnectionPatch) (dbq.ScmConnection, error) {
	if !actor.IsOrgAdmin() {
		return dbq.ScmConnection{}, apperr.Forbidden("only organisation admins can change SCM connections")
	}
	c, err := s.Q.GetScmConnection(ctx, id)
	if err != nil {
		return c, apperr.NotFound("connection")
	}
	conn, err := s.decrypt(c)
	if err != nil {
		return c, err
	}
	sec := secrets{PrivateKey: conn.PrivateKey, Token: conn.Token, WebhookSecret: conn.WebhookSecret}
	if in.PrivateKey != nil {
		sec.PrivateKey = *in.PrivateKey
	}
	if in.Token != nil {
		sec.Token = *in.Token
	}
	if in.WebhookSecret != nil && *in.WebhookSecret != "" {
		sec.WebhookSecret = *in.WebhookSecret
	}
	raw, _ := json.Marshal(sec)
	params := dbq.UpdateScmConnectionParams{ID: id, Name: c.Name, SecretsEnc: s.Box.Seal(raw), ManageHooks: c.ManageHooks, WriteBack: c.WriteBack}
	if in.Name != nil {
		params.Name = *in.Name
	}
	if in.ManageHooks != nil {
		params.ManageHooks = *in.ManageHooks
	}
	if in.WriteBack != nil {
		params.WriteBack = *in.WriteBack
	}
	out, err := s.Q.UpdateScmConnection(ctx, params)
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "scm.connection_update", "scm_connection", id.String(), nil, map[string]any{"name": out.Name})
	}
	return out, err
}

func (s *Service) DeleteConnection(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	if !actor.IsOrgAdmin() {
		return apperr.Forbidden("only organisation admins can remove SCM connections")
	}
	c, err := s.Q.GetScmConnection(ctx, id)
	if err != nil {
		return apperr.NotFound("connection")
	}
	if err := s.Q.DeleteScmConnection(ctx, id); err != nil {
		return err
	}
	tracker.Audit(ctx, s.Q, actor, "scm.connection_delete", "scm_connection", id.String(), nil, map[string]any{"name": c.Name})
	return nil
}

// LinkRepository connects a repository to a project (project admins). With manage_hooks on a
// GitLab connection, a project webhook is created through the API.
func (s *Service) LinkRepository(ctx context.Context, actor *authz.Actor, projectRef string, repoID uuid.UUID) (dbq.ScmRepository, error) {
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectRef, authz.ProjectAdmin)
	if err != nil {
		return dbq.ScmRepository{}, err
	}
	repo, err := s.Q.GetScmRepository(ctx, repoID)
	if err != nil {
		return repo, apperr.NotFound("repository")
	}
	err = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.LinkProjectRepository(ctx, dbq.LinkProjectRepositoryParams{ProjectID: p.ID, RepositoryID: repo.ID}); err != nil {
			return err
		}
		c, err := q.GetScmConnection(ctx, repo.ConnectionID)
		if err != nil {
			return err
		}
		if c.ManageHooks && c.Provider == "gitlab" && s.Jobs != nil {
			if _, err := s.Jobs.InsertTx(ctx, tx, jobs.SCMEnsureHook{RepositoryID: repo.ID}, nil); err != nil {
				return err
			}
		}
		tracker.Audit(ctx, q, actor, "scm.repository_link", "repository", repo.ID.String(), &p.ID, map[string]any{"repository": repo.FullPath})
		return nil
	})
	return repo, err
}

func (s *Service) UnlinkRepository(ctx context.Context, actor *authz.Actor, projectRef string, repoID uuid.UUID) error {
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectRef, authz.ProjectAdmin)
	if err != nil {
		return err
	}
	return s.Q.UnlinkProjectRepository(ctx, dbq.UnlinkProjectRepositoryParams{ProjectID: p.ID, RepositoryID: repoID})
}

// EnsureHook runs as a job: create or update the GitLab project hook for a repository.
func (s *Service) EnsureHook(ctx context.Context, repoID uuid.UUID) error {
	repo, err := s.Q.GetScmRepository(ctx, repoID)
	if err != nil {
		return err
	}
	c, err := s.Q.GetScmConnection(ctx, repo.ConnectionID)
	if err != nil {
		return err
	}
	conn, err := s.decrypt(c)
	if err != nil {
		return err
	}
	cl, err := s.client(ctx, c)
	if err != nil {
		return err
	}
	hookID, err := cl.EnsureHook(ctx, repoRef(repo), s.WebhookURL(c), conn.WebhookSecret)
	if err != nil {
		return err
	}
	if hookID != "" {
		return s.Q.SetRepositoryHook(ctx, dbq.SetRepositoryHookParams{ID: repo.ID, HookID: &hookID})
	}
	return nil
}

func repoRef(r dbq.ScmRepository) RepoRef {
	ref := RepoRef{ExternalID: r.ExternalID, FullPath: r.FullPath, WebURL: r.WebUrl, DefaultBranch: r.DefaultBranch}
	if r.InstallationID != nil {
		ref.InstallationID = *r.InstallationID
	}
	return ref
}

// ---------------------------------------------------------------------------
// Inbound webhooks (hooks mode): verify, store, enqueue. Processing happens in the worker.

const maxWebhookBytes = 25 << 20 // GitHub caps payloads at 25 MB

func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	provider, ok := s.Providers[providerName]
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(r.PathValue("connection"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.Q.GetScmConnection(r.Context(), id)
	if err != nil || c.Provider != providerName {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	conn, err := s.decrypt(c)
	if err != nil {
		s.Log.Error("webhook: cannot decrypt connection", "connection", c.ID, "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	d, err := provider.Verify(r, body, conn.WebhookSecret)
	if err != nil {
		webhooksReceived.WithLabelValues(providerName, "unknown", "rejected").Inc()
		s.Log.Warn("webhook rejected", "provider", providerName, "connection", c.ID, "error", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	if d.Event == "ping" {
		webhooksReceived.WithLabelValues(providerName, d.Event, "ok").Inc()
		w.WriteHeader(http.StatusOK)
		return
	}
	payload := body
	// GitHub can be configured to send form-encoded payloads.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		form, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, "bad form payload", http.StatusBadRequest)
			return
		}
		payload = []byte(form.Get("payload"))
	}
	if !json.Valid(payload) {
		http.Error(w, "payload is not JSON", http.StatusBadRequest)
		return
	}
	result := "stored"
	err = db.Tx(r.Context(), s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		eventID, err := q.InsertWebhookEvent(r.Context(), dbq.InsertWebhookEventParams{
			ConnectionID: c.ID, Provider: providerName, EventType: d.Event, DeliveryID: d.ID, Payload: payload,
		})
		if db.IsNoRows(err) {
			result = "duplicate" // redelivery of something already stored
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.TouchScmConnection(r.Context(), c.ID); err != nil {
			return err
		}
		if s.Jobs == nil {
			return nil
		}
		_, err = s.Jobs.InsertTx(r.Context(), tx, jobs.ProcessWebhook{EventID: eventID}, nil)
		return err
	})
	if err != nil {
		webhooksReceived.WithLabelValues(providerName, d.Event, "error").Inc()
		s.Log.Error("webhook store failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	webhooksReceived.WithLabelValues(providerName, d.Event, result).Inc()
	w.WriteHeader(http.StatusAccepted)
}

// Reprocess re-queues a stored event (admin action after fixing configuration).
func (s *Service) Reprocess(ctx context.Context, actor *authz.Actor, eventID uuid.UUID) error {
	if !actor.IsOrgAdmin() {
		return apperr.Forbidden("only organisation admins can reprocess webhooks")
	}
	if _, err := s.Q.GetWebhookEvent(ctx, eventID); err != nil {
		return apperr.NotFound("webhook event")
	}
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.ResetWebhookEvent(ctx, eventID); err != nil {
			return err
		}
		_, err := s.Jobs.InsertTx(ctx, tx, jobs.ProcessWebhook{EventID: eventID}, nil)
		return err
	})
}

// ---------------------------------------------------------------------------
// Create branch (issue view action)

func (s *Service) CreateBranch(ctx context.Context, actor *authz.Actor, issueRef string, repoID uuid.UUID, name, from string) (dbq.UpsertDevLinkRow, error) {
	issue, p, access, err := s.Tracker.IssueFor(ctx, actor, issueRef, authz.ProjectBrowse)
	if err != nil {
		return dbq.UpsertDevLinkRow{}, err
	}
	if !access.Has(authz.IssueEdit) && !access.Has(authz.IssueEditOthers) {
		return dbq.UpsertDevLinkRow{}, apperr.Forbidden("you cannot create branches for %s", p.Key)
	}
	repo, err := s.Q.GetScmRepository(ctx, repoID)
	if err != nil {
		return dbq.UpsertDevLinkRow{}, apperr.NotFound("repository")
	}
	linked, err := s.Q.RepositoryProjects(ctx, repo.ID)
	if err != nil {
		return dbq.UpsertDevLinkRow{}, err
	}
	ok := false
	for _, lp := range linked {
		ok = ok || lp.ID == p.ID
	}
	if !ok {
		return dbq.UpsertDevLinkRow{}, apperr.Invalid("repositoryId", "%s is not connected to %s", repo.FullPath, p.Key)
	}
	key := fmt.Sprintf("%s-%d", p.Key, issue.Number)
	if name == "" {
		name = tracker.SuggestedBranch(key, issue.Summary)
	}
	if !strings.Contains(strings.ToUpper(name), key) {
		return dbq.UpsertDevLinkRow{}, apperr.Invalid("name", "the branch name must contain %s so it links back", key)
	}
	if from == "" {
		from = repo.DefaultBranch
	}
	c, err := s.Q.GetScmConnection(ctx, repo.ConnectionID)
	if err != nil {
		return dbq.UpsertDevLinkRow{}, err
	}
	cl, err := s.client(ctx, c)
	if err != nil {
		return dbq.UpsertDevLinkRow{}, err
	}
	if err := cl.CreateBranch(ctx, repoRef(repo), name, from); err != nil {
		return dbq.UpsertDevLinkRow{}, apperr.New(http.StatusBadGateway, "scm_error", "%s refused to create the branch: %v", c.Provider, err)
	}
	link, err := s.Q.UpsertDevLink(ctx, dbq.UpsertDevLinkParams{
		IssueID: issue.ID, RepositoryID: repo.ID, Kind: "branch", ExternalID: name, Title: name,
		Url: branchURL(c.Provider, repo, name), State: "active", Data: []byte(`{}`), EventAt: time.Now(),
	})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "scm.branch_create", "issue", key, &p.ID, map[string]any{"branch": name, "repository": repo.FullPath})
	}
	return link, err
}

func branchURL(provider string, repo dbq.ScmRepository, branch string) string {
	if provider == "gitlab" {
		return repo.WebUrl + "/-/tree/" + url.PathEscape(branch)
	}
	return repo.WebUrl + "/tree/" + url.PathEscape(branch)
}

