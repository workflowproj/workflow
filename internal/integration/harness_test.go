// Package integration runs the whole application against a real PostgreSQL: set
// TEST_DATABASE_URL to an existing server, or let Testcontainers start postgres:17.
// Each test gets its own database cloned from a migrated template.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/geoffo-dev/workflow-management/internal/app"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
)

var (
	baseURL    string // admin connection URL
	templateDB = "wfm_it_template"
	setupOnce  sync.Once
	setupErr   error
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

func setup(t *testing.T) {
	t.Helper()
	setupOnce.Do(func() {
		ctx := context.Background()
		baseURL = os.Getenv("TEST_DATABASE_URL")
		if baseURL == "" {
			c, err := tcpostgres.Run(ctx, "postgres:17-alpine",
				tcpostgres.WithDatabase("wfm"), tcpostgres.WithUsername("wfm"), tcpostgres.WithPassword("wfm"),
				testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(90*time.Second)))
			if err != nil {
				setupErr = fmt.Errorf("start postgres container (or set TEST_DATABASE_URL): %w", err)
				return
			}
			baseURL, setupErr = c.ConnectionString(ctx, "sslmode=disable")
			if setupErr != nil {
				return
			}
		}
		admin, err := pgx.Connect(ctx, baseURL)
		if err != nil {
			setupErr = err
			return
		}
		defer admin.Close(ctx)
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+templateDB+" WITH (FORCE)")
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+templateDB); err != nil {
			setupErr = err
			return
		}
		pool, err := pgxpool.New(ctx, withDB(baseURL, templateDB))
		if err != nil {
			setupErr = err
			return
		}
		setupErr = db.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
		pool.Close()
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
}

func withDB(raw, name string) string {
	u, _ := url.Parse(raw)
	u.Path = "/" + name
	return u.String()
}

// freshDB clones the migrated template into a new database for one test.
func freshDB(t *testing.T) string {
	setup(t)
	ctx := context.Background()
	name := "wfm_it_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDB)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), baseURL)
		if err == nil {
			_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			c.Close(context.Background())
		}
	})
	return withDB(baseURL, name)
}

// Env is a running application on its own database.
type Env struct {
	t      *testing.T
	App    *app.App
	Srv    *httptest.Server
	Pool   *pgxpool.Pool
	Cfg    *config.Config
	Logs   *bytes.Buffer
	cancel context.CancelFunc
}

type envOption func(map[string]string)

func withEnv(k, v string) envOption { return func(m map[string]string) { m[k] = v } }

func newEnv(t *testing.T, opts ...envOption) *Env {
	t.Helper()
	dsn := freshDB(t)
	srv := httptest.NewUnstartedServer(nil)
	public := "http://" + srv.Listener.Addr().String()
	vars := map[string]string{
		"WFM_ENV": "development", "WFM_DEV_LOGIN": "true", "WFM_DATABASE_URL": dsn,
		"WFM_PUBLIC_URL": public, "WFM_SECRET_KEY": "integration-test-secret-key-0123456789",
		"WFM_WEBHOOK_ALLOW_PRIVATE": "true", "WFM_LOG_LEVEL": "debug",
	}
	for _, o := range opts {
		o(vars)
	}
	cfg, err := config.LoadFrom(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool, err := app.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(&lockedWriter{w: logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	noWorkers := false
	a, err := app.New(ctx, cfg, log, pool, app.Options{Workers: &noWorkers, OIDCHTTPClient: http.DefaultClient})
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = a.Handler()
	srv.Start()
	go a.Hub.Run(ctx)
	e := &Env{t: t, App: a, Srv: srv, Pool: pool, Cfg: cfg, Logs: logs, cancel: cancel}
	t.Cleanup(func() {
		srv.Close()
		cancel()
		pool.Close()
		if t.Failed() {
			t.Logf("server logs:\n%s", tail(logs.String(), 6000))
		}
	})
	return e
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// DrainJobs runs queued background jobs synchronously until none are left.
func (e *Env) DrainJobs() {
	e.t.Helper()
	ctx := context.Background()
	for round := 0; round < 50; round++ {
		rows, err := e.Pool.Query(ctx, `SELECT id, kind, args FROM river_job WHERE state = 'available' ORDER BY id`)
		if err != nil {
			e.t.Fatal(err)
		}
		type job struct {
			id   int64
			kind string
			args []byte
		}
		var batch []job
		for rows.Next() {
			var j job
			if err := rows.Scan(&j.id, &j.kind, &j.args); err != nil {
				e.t.Fatal(err)
			}
			batch = append(batch, j)
		}
		rows.Close()
		if len(batch) == 0 {
			return
		}
		for _, j := range batch {
			state := "completed"
			if err := e.App.RunJob(ctx, j.kind, j.args); err != nil {
				e.t.Logf("job %s (%d) failed: %v", j.kind, j.id, err)
				state = "discarded"
			}
			if _, err := e.Pool.Exec(ctx, `UPDATE river_job SET state = $2, finalized_at = now() WHERE id = $1`, j.id, state); err != nil {
				e.t.Fatal(err)
			}
		}
	}
	e.t.Fatal("jobs kept enqueuing more jobs (automation loop?)")
}

// JobsOfKind counts queued jobs of a kind.
func (e *Env) JobsOfKind(kind string) int {
	var n int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE kind = $1`, kind).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------------------
// HTTP client

type Client struct {
	e     *Env
	http  *http.Client
	token string
	csrf  bool
	Me    map[string]any
}

func (e *Env) anon() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{e: e, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, csrf: true}
}

// login signs in through the development login (creating the user if needed).
func (e *Env) login(email, name string) *Client {
	e.t.Helper()
	c := e.anon()
	var me map[string]any
	c.must("POST", "/api/v1/auth/dev-login", map[string]any{"email": email, "displayName": name}, 200, &me)
	c.Me = me
	return c
}

func (e *Env) withToken(token string) *Client {
	return &Client{e: e, http: &http.Client{}, token: token}
}

func (c *Client) id() string { return c.Me["id"].(string) }

type Response struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r Response) JSON() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body, &m)
	return m
}

func (c *Client) do(method, path string, body any, hdr ...string) Response {
	c.e.t.Helper()
	var rdr io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rdr = bytes.NewReader(b)
	case string:
		rdr = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.e.t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
		ct = "application/json"
	}
	req, err := http.NewRequest(method, c.e.Srv.URL+path, rdr)
	if err != nil {
		c.e.t.Fatal(err)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if c.csrf {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			req.Host = hdr[i+1] // the client ignores a Host header
			continue
		}
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return Response{Status: res.StatusCode, Body: raw, Header: res.Header}
}

// must performs a request, asserts the status and decodes the body into out (if non-nil).
func (c *Client) must(method, path string, body any, want int, out any) Response {
	c.e.t.Helper()
	r := c.do(method, path, body)
	if r.Status != want {
		c.e.t.Fatalf("%s %s: status %d, want %d\n%s", method, path, r.Status, want, r.Body)
	}
	if out != nil && len(r.Body) > 0 {
		if err := json.Unmarshal(r.Body, out); err != nil {
			c.e.t.Fatalf("%s %s: decode: %v\n%s", method, path, err, r.Body)
		}
	}
	return r
}

func (c *Client) get(path string, out any) Response { return c.must("GET", path, nil, 200, out) }

// ---------------------------------------------------------------------------
// Small JSON helpers

type obj = map[string]any

func str(m obj, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

func keys(list []obj) []string {
	out := make([]string, len(list))
	for i, m := range list {
		out[i] = m["key"].(string)
	}
	return out
}

func eq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func sameStrings(t *testing.T, got, want []string, msg string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// statusID finds a status id by name.
func (c *Client) statusID(name string) string {
	var sts []obj
	c.get("/api/v1/statuses", &sts)
	for _, s := range sts {
		if strings.EqualFold(s["name"].(string), name) {
			return s["id"].(string)
		}
	}
	c.e.t.Fatalf("status %q not found", name)
	return ""
}

func (c *Client) createProject(key, template string) obj {
	var p obj
	c.must("POST", "/api/v1/projects", obj{"key": key, "name": key + " project", "template": template}, 201, &p)
	return p
}

func (c *Client) createIssue(project, typeKey, summary string, extra obj) obj {
	c.e.t.Helper()
	body := obj{"projectKey": project, "typeKey": typeKey, "summary": summary}
	for k, v := range extra {
		body[k] = v
	}
	var i obj
	c.must("POST", "/api/v1/issues", body, 201, &i)
	return i
}

func (c *Client) issue(key string) obj {
	var i obj
	c.get("/api/v1/issues/"+key, &i)
	return i
}

func (c *Client) transition(key, transitionID string, want int) Response {
	return c.must("POST", "/api/v1/issues/"+key+"/transitions", obj{"transitionId": transitionID}, want, nil)
}

func (c *Client) search(jql string) []string {
	c.e.t.Helper()
	var res struct {
		Issues []obj `json:"issues"`
	}
	c.get("/api/v1/search?jql="+url.QueryEscape(jql)+"&limit=200", &res)
	return keys(res.Issues)
}

func (e *Env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.Pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}
