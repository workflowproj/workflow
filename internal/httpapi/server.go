// Package httpapi implements the generated OpenAPI server interface and mounts the hand-written
// routes (OIDC redirects, server-sent events, inbound webhooks, SCIM).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oapi-codegen/nullable"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.yaml.in/yaml/v3"

	apispec "github.com/geoffo-dev/workflow-management/api"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/automation"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/events"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/mail"
	"github.com/geoffo-dev/workflow-management/internal/mcpserver"
	"github.com/geoffo-dev/workflow-management/internal/scm"
	"github.com/geoffo-dev/workflow-management/internal/secure"
	"github.com/geoffo-dev/workflow-management/internal/telemetry"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

type Server struct {
	Cfg        *config.Config
	Log        *slog.Logger
	Pool       *pgxpool.Pool
	Q          *dbq.Queries
	Tracker    *tracker.Service
	Auth       *identity.Auth
	Prov       *identity.Provisioner
	SCM        *scm.Service
	Automation *automation.Service
	Jobs       jobs.Enqueuer
	Hub        *events.Hub
	Box        *secure.Box
	Mailer     mail.Sender

	limitState limitState
}

var _ api.StrictServerInterface = (*Server)(nil)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wfm_http_requests_total", Help: "HTTP requests by route and status.",
	}, []string{"method", "route", "status"})
	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "wfm_http_request_duration_seconds", Help: "HTTP request latency.", Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
)

// ---------------------------------------------------------------------------
// Errors as RFC 9457 problem details

func WriteProblem(log *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	p := api.Problem{Status: http.StatusInternalServerError}
	if ae, ok := apperr.As(err); ok {
		p.Status = ae.Status
		p.Detail = &ae.Message
		code := ae.Code
		p.Code = &code
		p.Position = ae.Position
		if len(ae.Fields) > 0 {
			fe := make([]api.FieldError, len(ae.Fields))
			for i, f := range ae.Fields {
				field := f.Field
				fe[i] = api.FieldError{Message: f.Message}
				if field != "" {
					fe[i].Field = &field
				}
			}
			p.Errors = &fe
		}
	} else if db.IsUniqueViolation(err) {
		p.Status = http.StatusConflict
		msg := "that already exists"
		p.Detail = &msg
	} else if errors.Is(err, context.Canceled) {
		p.Status = 499
	} else {
		log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		msg := "an unexpected error occurred"
		p.Detail = &msg
	}
	p.Title = http.StatusText(p.Status)
	if p.Title == "" {
		p.Title = "Error"
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func badRequest(log *slog.Logger) func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		WriteProblem(log, w, r, apperr.BadRequest("%s", err.Error()))
	}
}

// ---------------------------------------------------------------------------
// Request context helpers

type httpKey struct{}

type httpPair struct {
	w http.ResponseWriter
	r *http.Request
}

// withHTTP gives strict handlers access to the writer (cookies) and request (client IP).
func withHTTP(f api.StrictHandlerFunc, _ string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		return f(context.WithValue(ctx, httpKey{}, httpPair{w, r}), w, r, req)
	}
}

func httpFrom(ctx context.Context) (http.ResponseWriter, *http.Request) {
	p, _ := ctx.Value(httpKey{}).(httpPair)
	return p.w, p.r
}

// actor returns the authenticated caller or a 401.
func actor(ctx context.Context) (*authz.Actor, error) {
	a := identity.ActorFrom(ctx)
	if a == nil {
		return nil, apperr.Unauthorized()
	}
	return a, nil
}

func orgAdmin(ctx context.Context) (*authz.Actor, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.IsOrgAdmin() {
		return nil, apperr.Forbidden("organisation admin role required")
	}
	return a, nil
}

func opt[T any](n nullable.Nullable[T]) tracker.Opt[T] {
	if !n.IsSpecified() {
		return tracker.Opt[T]{}
	}
	if n.IsNull() {
		return tracker.Null[T]()
	}
	return tracker.Some(n.MustGet())
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func limitOr(l *api.Limit, def int) int {
	if l == nil || *l <= 0 {
		return def
	}
	return min(*l, 500)
}

// ---------------------------------------------------------------------------
// Router

// WebHandler serves the UI, the API, OIDC redirects and live events.
func (s *Server) WebHandler(ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	strict := api.NewStrictHandlerWithOptions(s, []api.StrictMiddlewareFunc{withHTTP}, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  badRequest(s.Log),
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { WriteProblem(s.Log, w, r, err) },
	})
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux, ErrorHandlerFunc: badRequest(s.Log)})
	mux.HandleFunc("GET /api/v1/events", s.streamEvents)
	mux.HandleFunc("GET /api/v1/openapi.yaml", s.openAPI)
	mux.HandleFunc("GET /api/v1/dev/mailbox", s.devMailbox)
	mux.HandleFunc("GET /api/v1/openapi.json", s.openAPI)
	mux.Handle("/mcp", (&mcpserver.Server{API: s, Auth: s.Auth, PublicURL: s.Cfg.PublicURL, Version: telemetry.Version, Log: s.Log}).Handler())
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(s.Log, w, r, apperr.NotFound("route"))
	})
	mux.HandleFunc("GET /auth/{provider}/login", s.Auth.HandleLogin)
	mux.HandleFunc("GET /auth/{provider}/callback", s.Auth.HandleCallback)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	if ui != nil {
		mux.Handle("/", ui)
	}
	return s.common(s.clientAddress(s.authenticate(mux)))
}

// HooksHandler serves the internet-facing endpoints: Git webhooks and SCIM.
func (s *Server) HooksHandler(scimHandler interface {
	Routes(*http.ServeMux, string)
}) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hooks/{provider}/{connection}", s.SCM.HandleWebhook)
	mux.HandleFunc("POST /hooks/chat/{provider}", s.Automation.HandleChatCommand) // Slack / Mattermost /wfm
	if scimHandler != nil {
		scimHandler.Routes(mux, "/scim/v2")
	}
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	return s.common(s.clientAddress(mux))
}

// openAPI publishes the API description (YAML, or JSON for tools that want it).
func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	if !strings.HasSuffix(r.URL.Path, ".json") {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(apispec.OpenAPI)
		return
	}
	specJSON.once.Do(func() {
		var doc any
		if specJSON.err = yaml.Unmarshal(apispec.OpenAPI, &doc); specJSON.err == nil {
			specJSON.body, specJSON.err = json.Marshal(doc)
		}
	})
	if specJSON.err != nil {
		WriteProblem(s.Log, w, r, specJSON.err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(specJSON.body)
}

var specJSON struct {
	once sync.Once
	body []byte
	err  error
}

// devMailbox lists the emails held in memory when no SMTP server is configured, in
// development only (?to=address filters), so tests and developers can follow invite and reset
// links.
func (s *Server) devMailbox(w http.ResponseWriter, r *http.Request) {
	box, ok := s.Mailer.(*mail.Memory)
	if !ok || s.Cfg.Production {
		WriteProblem(s.Log, w, r, apperr.NotFound("route"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(box.Messages(r.URL.Query().Get("to")))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Pool.Ping(ctx); err != nil {
		http.Error(w, `{"status":"database unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// route collapses ids so metrics stay low-cardinality.
func route(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if _, err := uuid.Parse(p); err == nil {
			parts[i] = ":id"
		} else if issueKeyLike(p) {
			parts[i] = ":key"
		}
	}
	r := strings.Join(parts, "/")
	if !strings.HasPrefix(r, "/api/") && !strings.HasPrefix(r, "/hooks/") && !strings.HasPrefix(r, "/scim/") && !strings.HasPrefix(r, "/auth/") {
		return "/ui"
	}
	return r
}

func issueKeyLike(s string) bool {
	i := strings.LastIndexByte(s, '-')
	if i <= 0 {
		return false
	}
	_, err := strconv.Atoi(s[i+1:])
	return err == nil && strings.ToUpper(s[:i]) == s[:i]
}

// common adds recovery, security headers, metrics and access logs.
func (s *Server) common(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error("panic", "error", v, "stack", string(debug.Stack()))
				WriteProblem(s.Log, rec, r, errors.New("panic"))
			}
			rt := route(r.URL.Path)
			httpRequests.WithLabelValues(r.Method, rt, strconv.Itoa(rec.status)).Inc()
			if rt != "/api/v1/events" {
				httpDuration.WithLabelValues(r.Method, rt).Observe(time.Since(start).Seconds())
			}
			if rt != "/ui" && r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
				s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
			}
		}()
		h := rec.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(rec, r)
	})
}

var safeMethods = map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodOptions: true}

// authenticate resolves the caller and enforces CSRF and token-scope rules for /api.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		a, err := s.Auth.Authenticate(r)
		if err != nil {
			WriteProblem(s.Log, w, r, err)
			return
		}
		// Cookie sessions must send a header a cross-site form cannot (CSRF); tokens are exempt.
		if !safeMethods[r.Method] && (a == nil || a.TokenID == nil) && r.Header.Get("X-Requested-With") == "" {
			WriteProblem(s.Log, w, r, apperr.Forbidden("missing X-Requested-With header"))
			return
		}
		if err := identity.CheckScope(a, r.Method, r.URL.Path); err != nil {
			WriteProblem(s.Log, w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(identity.WithActor(r.Context(), a)))
	})
}
