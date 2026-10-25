package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/geoffo-dev/workflow-management/internal/app"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/identity/local"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// createAdmin makes someone an organisation admin with a local account and prints a temporary
// password: the way into an install without single sign-on. It reads the server's configuration
// (WFM_DATABASE_URL, WFM_SECRET_KEY, WFM_PUBLIC_URL) and talks to the database directly.
func createAdmin(args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
	email := fs.String("email", "", "email address of the admin (required)")
	name := fs.String("name", "", "display name, when the account is new")
	_ = fs.Parse(args)
	if *email == "" {
		return errors.New("create-admin needs --email")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, stop := signalContext()
	defer stop()
	pool, err := app.OpenWithRetry(ctx, cfg.AppDatabaseURL(), time.Minute, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	box, err := secure.NewBox(cfg.SecretKey)
	if err != nil {
		return err
	}
	// Insert-only job client: any email goes out through the running workers.
	jobs, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: log})
	if err != nil {
		return err
	}
	u, password, err := local.New(pool, cfg, box, jobs, log).BootstrapAdmin(ctx, *email, *name)
	if err != nil {
		return err
	}
	fmt.Printf("%s <%s> is an organisation admin.\n\nTemporary password: %s\n\nSign in at %s/login with it; you will choose your own password straight away.\n", u.DisplayName, u.Email, password, cfg.PublicURL)
	return nil
}
