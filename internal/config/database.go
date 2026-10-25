package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Database connection routing.
//
// Behind a transaction-mode connection pooler (PgBouncer, e.g. a CloudNativePG Pooler) every
// app query can go through the pooler, but LISTEN cannot: a listener needs its own session.
// Two ways to configure it:
//
//   - WFM_DATABASE_POOLER_HOST=host[:port]: WFM_DATABASE_URL stays the direct address (the
//     same Secret the migration job and KEDA use) and queries are sent to the pooler instead,
//     with the same credentials and database.
//   - WFM_DATABASE_DIRECT_URL: WFM_DATABASE_URL already points at the pooler, and this is the
//     direct address for listeners and migrations.
//
// With neither set, everything uses WFM_DATABASE_URL, as before.

// DatabasePooled reports whether queries go through a transaction-mode pooler.
func (c *Config) DatabasePooled() bool {
	return c.DatabasePoolerHost != "" || c.DatabaseDirectURL != ""
}

// AppDatabaseURL is the address for the application's connection pool.
func (c *Config) AppDatabaseURL() string {
	if c.DatabasePoolerHost == "" {
		return c.DatabaseURL
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return c.DatabaseURL // validated at load; unreachable
	}
	u.Host = c.DatabasePoolerHost
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		u.Host = net.JoinHostPort(c.DatabasePoolerHost, "5432")
	}
	return u.String()
}

// DirectDatabaseURL is the address for sessions a pooler cannot serve: LISTEN for live events,
// and schema migrations.
func (c *Config) DirectDatabaseURL() string {
	if c.DatabaseDirectURL != "" {
		return c.DatabaseDirectURL
	}
	return c.DatabaseURL
}

func validateDatabase(c *Config) []error {
	var errs []error
	if c.DatabasePoolerHost != "" && c.DatabaseDirectURL != "" {
		errs = append(errs, fmt.Errorf("set only one of WFM_DATABASE_POOLER_HOST and WFM_DATABASE_DIRECT_URL"))
	}
	if h := c.DatabasePoolerHost; h != "" {
		if strings.ContainsAny(h, "/@?# ") {
			errs = append(errs, fmt.Errorf("WFM_DATABASE_POOLER_HOST: want host or host:port, got %q", h))
		}
		u, err := url.Parse(c.DatabaseURL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			errs = append(errs, fmt.Errorf("WFM_DATABASE_POOLER_HOST needs WFM_DATABASE_URL in URL form (postgres://...)"))
		}
	}
	if c.WorkerPollInterval < 100*time.Millisecond {
		errs = append(errs, fmt.Errorf("WFM_WORKER_POLL_INTERVAL: must be at least 100ms, got %s", c.WorkerPollInterval))
	}
	return errs
}
