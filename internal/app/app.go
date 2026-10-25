// Package app wires the modules together and runs the configured mode(s).
//
//	web    UI, REST API, OIDC sign-in and live events (internal ingress)
//	hooks  GitHub/GitLab webhooks and SCIM (the only internet-facing mode)
//	worker background jobs: webhook processing, automation, directory sync, write-back
//	all    everything in one process (development, single-node installs)
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/geoffo-dev/workflow-management/internal/automation"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/events"
	"github.com/geoffo-dev/workflow-management/internal/httpapi"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/identity/directory"
	"github.com/geoffo-dev/workflow-management/internal/identity/local"
	"github.com/geoffo-dev/workflow-management/internal/identity/scim"
	"github.com/geoffo-dev/workflow-management/internal/jiraimport"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/mail"
	"github.com/geoffo-dev/workflow-management/internal/notify"
	"github.com/geoffo-dev/workflow-management/internal/oauth"
	"github.com/geoffo-dev/workflow-management/internal/scm"
	"github.com/geoffo-dev/workflow-management/internal/secure"
	"github.com/geoffo-dev/workflow-management/internal/servicedesk"
	"github.com/geoffo-dev/workflow-management/internal/storage"
	"github.com/geoffo-dev/workflow-management/internal/telemetry"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
	"github.com/geoffo-dev/workflow-management/internal/webui"
	"github.com/geoffo-dev/workflow-management/internal/workflow"
)

type App struct {
	Cfg        *config.Config
	Log        *slog.Logger
	Pool       *pgxpool.Pool
	River      *river.Client[pgx.Tx]
	Box        *secure.Box
	Tracker    *tracker.Service
	Prov       *identity.Provisioner
	Auth       *identity.Auth
	SCM        *scm.Service
	Automation *automation.Service
	Syncer     *directory.Syncer
	Hub        *events.Hub
	// Mailer sends email (an in-memory *mail.Memory without WFM_SMTP_HOST).
	Mailer mail.Sender
	// Notify emails notifications (immediately or in digests) per people's settings.
	Notify *notify.Service
	// Local handles local accounts (email and password sign-in).
	Local  *local.Service
	Server *httpapi.Server
	Scim   *scim.Handler
	// OAuth signs AI assistants in to /mcp.
	OAuth *oauth.Server
	// JiraImport imports projects from Jira Cloud or Data Center.
	JiraImport *jiraimport.Service
	// ServiceDesk runs service projects: the customer portal, queues, SLAs and approvals.
	ServiceDesk *servicedesk.Service
}

// Options let tests substitute collaborators.
type Options struct {
	// Workers forces the River client to run workers regardless of mode.
	Workers *bool
	// OIDCHTTPClient is used for OIDC discovery and token exchange.
	OIDCHTTPClient *http.Client
}

// OpenWithRetry opens the pool, retrying until the database accepts connections or `wait`
// elapses. In Kubernetes a pod often starts before its database (or its network policy) is ready.
func OpenWithRetry(ctx context.Context, databaseURL string, wait time.Duration, log *slog.Logger) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(wait)
	backoff := time.Second
	for {
		pool, err := OpenURL(ctx, databaseURL)
		if err == nil || time.Now().After(deadline) {
			return pool, err
		}
		log.Warn("database not ready; retrying", "error", err, "in", backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}

func Open(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	return OpenURL(ctx, cfg.AppDatabaseURL())
}

func OpenURL(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pc.ConnConfig.Tracer = otelpgx.NewTracer()
	pc.MaxConns = max(pc.MaxConns, 10)
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

func New(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, opts Options) (*App, error) {
	box, err := secure.NewBox(cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	eval, err := workflow.NewEvaluator()
	if err != nil {
		return nil, err
	}
	store, err := storage.New(ctx, cfg.S3, pool)
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Log: log, Pool: pool, Box: box, Mailer: mail.New(cfg.SMTP, log.With("module", "mail"))}

	runWorkers := cfg.RunsWorker()
	if opts.Workers != nil {
		runWorkers = *opts.Workers
	}
	// The River client is created first (insert-only), then services, then workers are attached.
	// Behind a transaction-mode pooler River cannot LISTEN, so it polls for new jobs and for
	// leadership changes instead.
	rcfg := &river.Config{
		Logger: log.With("component", "river"), JobTimeout: 5 * time.Minute,
		PollOnly: cfg.DatabasePooled(), FetchPollInterval: cfg.WorkerPollInterval,
	}
	if runWorkers {
		rcfg.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: cfg.WorkerConcurrency},
			jobs.QueueSCM:      {MaxWorkers: cfg.WorkerConcurrency},
			jobs.QueueOutbound: {MaxWorkers: max(cfg.WorkerConcurrency/2, 2)},
		}
		rcfg.Workers = river.NewWorkers()
	}

	// Services need an enqueuer; the client is built below once workers exist, so use a late binding.
	enq := &lateEnqueuer{}
	a.Tracker = tracker.New(pool, eval, enq, store, cfg, log.With("module", "tracker"))
	a.Prov = identity.NewProvisioner(pool, cfg, log.With("module", "identity"))
	a.Auth = &identity.Auth{Cfg: cfg, Q: dbq.New(pool), Prov: a.Prov, Box: box, Log: log.With("module", "auth"), Track: a.Tracker, HTTPClient: opts.OIDCHTTPClient}
	a.SCM = scm.NewService(pool, box, cfg, a.Tracker, enq, log.With("module", "scm"))
	a.Automation = automation.New(pool, a.Tracker, eval, enq, box, cfg, log.With("module", "automation"))
	a.Notify = notify.New(pool, enq, a.Mailer, box, cfg, log.With("module", "notify"))
	a.ServiceDesk = servicedesk.New(pool, a.Tracker, a.Notify, nil, enq, cfg, log.With("module", "service-desk"))
	a.Tracker.AfterCreateProject, a.Tracker.OnEvent, a.Tracker.SLACalendars = a.ServiceDesk.ProjectCreated, a.ServiceDesk.OnEvent, a.ServiceDesk.Calendars
	if cfg.GoogleDirectory.Enabled {
		a.Syncer = directory.NewSyncer(pool, cfg.GoogleDirectory, a.Prov, log.With("module", "directory"))
	}
	a.Hub = events.NewHub(pool, log.With("module", "events"))
	if cfg.DatabasePooled() {
		a.Hub.ListenURL = cfg.DirectDatabaseURL()
	}
	a.Scim = &scim.Handler{Pool: pool, Q: dbq.New(pool), Prov: a.Prov, BaseURL: cfg.HooksPublicURL + "/scim/v2", Log: log.With("module", "scim")}
	a.JiraImport = jiraimport.New(pool, box, cfg, a.Tracker, enq, log.With("module", "jira-import"))

	if runWorkers {
		rcfg.Workers = a.workers()
		rcfg.PeriodicJobs = a.periodicJobs()
	}
	client, err := river.NewClient(riverpgxv5.New(pool), rcfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	a.River = client
	enq.Enqueuer = client

	a.OAuth = oauth.New(cfg, pool, dbq.New(pool), box, a.Auth, a.Tracker, log.With("module", "oauth"))
	a.Local = local.New(pool, cfg, box, enq, log.With("module", "local-accounts"))
	a.ServiceDesk.Local = a.Local
	a.Server = &httpapi.Server{
		Cfg: cfg, Log: log.With("module", "http"), Pool: pool, Q: dbq.New(pool), Tracker: a.Tracker, Auth: a.Auth, Prov: a.Prov,
		SCM: a.SCM, Automation: a.Automation, Jobs: enq, Hub: a.Hub, Box: box, Mailer: a.Mailer, OAuth: a.OAuth,
		Notify: a.Notify, Local: a.Local, JiraImport: a.JiraImport, ServiceDesk: a.ServiceDesk,
	}
	return a, nil
}

// lateEnqueuer lets services be constructed before the River client that depends on them.
type lateEnqueuer struct{ jobs.Enqueuer }

// Handler returns the HTTP handler for the configured mode.
func (a *App) Handler() http.Handler {
	var h http.Handler
	switch a.Cfg.Mode {
	case config.ModeWeb:
		h = a.Server.WebHandler(webui.Handler())
	case config.ModeHooks:
		h = a.Server.HooksHandler(a.Scim)
	default:
		web := a.Server.WebHandler(webui.Handler())
		hooks := a.Server.HooksHandler(a.Scim)
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/hooks/") || strings.HasPrefix(r.URL.Path, "/scim/") {
				hooks.ServeHTTP(w, r)
				return
			}
			web.ServeHTTP(w, r)
		})
	}
	return otelhttp.NewHandler(h, "wfm", otelhttp.WithFilter(func(r *http.Request) bool {
		return r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && !strings.HasPrefix(r.URL.Path, "/assets/")
	}))
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errc := make(chan error, 4)

	metrics := telemetry.MetricsServer(a.Cfg.MetricsAddr, a.Pool.Ping)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := metrics.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("metrics server: %w", err)
		}
	}()

	var srv *http.Server
	if a.Cfg.Mode != config.ModeWorker {
		srv = &http.Server{Addr: a.Cfg.HTTPAddr, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Log.Info("http listening", "addr", a.Cfg.HTTPAddr, "mode", a.Cfg.Mode)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- fmt.Errorf("http server: %w", err)
			}
		}()
	}
	if a.Cfg.RunsWeb() {
		wg.Add(1)
		go func() { defer wg.Done(); a.Hub.Run(ctx) }()
	}
	var workerStarted atomic.Bool
	if a.Cfg.RunsWorker() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// On a first install the migration job may still be running; wait for its tables
			// rather than crash-looping.
			if err := db.WaitForSchema(ctx, a.Pool, a.Log); err != nil {
				return // shutting down
			}
			if err := a.River.Start(ctx); err != nil {
				errc <- fmt.Errorf("start worker: %w", err)
				return
			}
			workerStarted.Store(true)
			a.Log.Info("worker started", "concurrency", a.Cfg.WorkerConcurrency)
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}
	a.Log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if srv != nil {
		a.Hub.Close() // end SSE streams so Shutdown does not wait for them
		_ = srv.Shutdown(shutdownCtx)
	}
	if workerStarted.Load() {
		if err := a.River.Stop(shutdownCtx); err != nil {
			a.Log.Warn("worker stop", "error", err)
		}
	}
	_ = metrics.Shutdown(shutdownCtx)
	wg.Wait()
	return runErr
}

// Migrate applies database migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	return db.Migrate(ctx, pool, log)
}
