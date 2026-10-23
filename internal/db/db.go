// Package db owns the connection pool, migrations and transaction helpers.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies the application schema (goose) and River's job-queue schema.
// Migrations are written expand-then-contract so they can run before new pods roll out.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("application migrations: %w", err)
	}
	version, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river migrations: %w", err)
	}
	log.Info("database migrated", "schema_version", version, "river_versions_applied", len(res.Versions))
	return nil
}

// WaitForSchema blocks until migrations have created the application and job-queue tables, or
// ctx ends. Workers use it on first install, when they can start before the migration job.
func WaitForSchema(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	var lastLog time.Time
	for {
		var ready bool
		err := pool.QueryRow(ctx, `SELECT to_regclass('river_queue') IS NOT NULL AND to_regclass('issues') IS NOT NULL`).Scan(&ready)
		if err == nil && ready {
			return nil
		}
		if time.Since(lastLog) > 30*time.Second {
			log.Info("waiting for database migrations", "error", err)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Tx runs fn in a transaction, committing on success.
func Tx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx, q *dbq.Queries) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return fn(tx, dbq.New(tx))
	})
}

// IsUniqueViolation reports a Postgres unique_violation, optionally for a specific constraint.
func IsUniqueViolation(err error, constraint ...string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	if len(constraint) == 0 {
		return true
	}
	for _, c := range constraint {
		if pgErr.ConstraintName == c {
			return true
		}
	}
	return false
}

func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
