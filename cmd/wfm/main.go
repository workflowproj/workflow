// Command wfm is the workflow platform: one binary that runs as web, hooks or worker, applies
// database migrations, and acts as a CLI for workflows as code.
//
//	wfm serve --mode=web|hooks|worker|all
//	wfm migrate
//	wfm workflow apply -f workflow.yaml [--project KEY] [--map "Old=New"] [--dry-run]
//	wfm workflow validate -f workflow.yaml
//	wfm workflow export --id <uuid>
//	wfm version
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/app"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/telemetry"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wfm:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return serve(args)
	case "migrate":
		return migrate()
	case "workflow":
		return workflowCmd(args)
	case "version":
		fmt.Println(telemetry.Version)
		return nil
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}

const usage = `Usage:
  wfm serve [--mode=web|hooks|worker|all] [--migrate]
  wfm migrate
  wfm workflow apply -f FILE [--project KEY] [--map "Old status=New status"] [--dry-run]
  wfm workflow validate -f FILE
  wfm workflow export --id WORKFLOW_ID
  wfm version

The workflow commands talk to the API: set WFM_SERVER (e.g. https://wfm.example.com) and
WFM_TOKEN (a personal access token or service-account token with the write scope).`

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	mode := fs.String("mode", "", "web, hooks, worker or all (default: WFM_MODE or all)")
	doMigrate := fs.Bool("migrate", false, "apply database migrations before serving")
	_ = fs.Parse(args)
	if *mode != "" {
		_ = os.Setenv("WFM_MODE", *mode)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := telemetry.Logger(cfg.LogLevel, cfg.LogFormat)
	ctx, stop := signalContext()
	defer stop()

	shutdownTracing, err := telemetry.Tracing(ctx, string(cfg.Mode), log)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	pool, err := app.OpenWithRetry(ctx, cfg.DatabaseURL, 2*time.Minute, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	if *doMigrate {
		if err := app.Migrate(ctx, pool, log); err != nil {
			return err
		}
	}
	a, err := app.New(ctx, cfg, log, pool, app.Options{})
	if err != nil {
		return err
	}
	log.Info("starting", "version", telemetry.Version, "mode", cfg.Mode, "public_url", cfg.PublicURL)
	return a.Run(ctx)
}

// migrate only needs the database: it runs as a Kubernetes job (or Argo CD hook) that should
// not depend on the rest of the configuration being valid.
func migrate() error {
	url := os.Getenv("WFM_DATABASE_URL")
	if url == "" {
		url = config.DefaultDatabaseURL
	}
	log := telemetry.Logger(envOr("WFM_LOG_LEVEL", "info"), envOr("WFM_LOG_FORMAT", "json"))
	ctx, stop := signalContext()
	defer stop()
	// A fresh database (e.g. a new CloudNativePG cluster) can take minutes to accept connections.
	pool, err := app.OpenWithRetry(ctx, url, 5*time.Minute, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	return app.Migrate(ctx, pool, log)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ---------------------------------------------------------------------------
// Workflows as code

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func workflowCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("workflow needs a subcommand: apply, validate or export")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("workflow "+sub, flag.ExitOnError)
	file := fs.String("f", "", "workflow YAML file (- for stdin)")
	project := fs.String("project", "", "project key (omit for a shared workflow)")
	id := fs.String("id", "", "workflow id (export)")
	dryRun := fs.Bool("dry-run", false, "report what would happen without changing anything")
	var maps multiFlag
	fs.Var(&maps, "map", `status mapping for removed statuses, "Old=New" (repeatable)`)
	_ = fs.Parse(args)

	client, err := newAPIClient()
	if err != nil {
		return err
	}
	switch sub {
	case "apply", "validate":
		src, err := readSource(*file)
		if err != nil {
			return err
		}
		if sub == "validate" {
			var res struct {
				Valid  bool `json:"valid"`
				Errors []struct {
					Path    string `json:"path"`
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := client.call(http.MethodPost, "/workflows/validate", map[string]any{"yaml": src}, &res); err != nil {
				return err
			}
			if res.Valid {
				fmt.Println("valid")
				return nil
			}
			for _, e := range res.Errors {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", e.Path, e.Message)
			}
			return errors.New("workflow is invalid")
		}
		mapping := map[string]string{}
		for _, m := range maps {
			from, to, ok := strings.Cut(m, "=")
			if !ok {
				return fmt.Errorf("--map %q: expected Old=New", m)
			}
			mapping[strings.TrimSpace(from)] = strings.TrimSpace(to)
		}
		body := map[string]any{"yaml": src, "statusMapping": mapping, "dryRun": *dryRun}
		if *project != "" {
			body["projectKey"] = *project
		}
		var res struct {
			Action          string `json:"action"`
			WorkflowID      string `json:"workflowId"`
			Version         int    `json:"version"`
			MigrationQueued bool   `json:"migrationQueued"`
		}
		if err := client.call(http.MethodPut, "/workflows/apply", body, &res); err != nil {
			return err
		}
		prefix := ""
		if *dryRun {
			prefix = "(dry run) "
		}
		fmt.Printf("%sworkflow %s: %s, version %d", prefix, res.WorkflowID, res.Action, res.Version)
		if res.MigrationQueued {
			fmt.Print(" (issue migration queued)")
		}
		fmt.Println()
		return nil
	case "export":
		if *id == "" {
			return errors.New("--id is required")
		}
		var res struct {
			Version struct {
				YAML string `json:"yaml"`
			} `json:"version"`
		}
		if err := client.call(http.MethodGet, "/workflows/"+*id, nil, &res); err != nil {
			return err
		}
		fmt.Print(res.Version.YAML)
		return nil
	}
	return fmt.Errorf("unknown workflow subcommand %q", sub)
}

func readSource(file string) (string, error) {
	if file == "" {
		return "", errors.New("-f is required")
	}
	var b []byte
	var err error
	if file == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(file)
	}
	return string(b), err
}

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

func newAPIClient() (*apiClient, error) {
	server := strings.TrimRight(os.Getenv("WFM_SERVER"), "/")
	token := os.Getenv("WFM_TOKEN")
	if server == "" || token == "" {
		return nil, errors.New("set WFM_SERVER and WFM_TOKEN")
	}
	return &apiClient{base: server + "/api/v1", token: token, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (c *apiClient) call(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		var p struct {
			Detail string `json:"detail"`
			Errors []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(raw, &p)
		msg := p.Detail
		for _, e := range p.Errors {
			msg += fmt.Sprintf("\n  %s: %s", e.Field, e.Message)
		}
		if msg == "" {
			msg = string(raw)
		}
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, msg)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
