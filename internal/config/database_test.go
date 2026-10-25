package config

import (
	"strings"
	"testing"
)

func load(t *testing.T, vars map[string]string) (*Config, error) {
	t.Helper()
	base := map[string]string{"WFM_ENV": "development"}
	for k, v := range vars {
		base[k] = v
	}
	return LoadFrom(func(k string) string { return base[k] })
}

func TestDatabaseRouting(t *testing.T) {
	const direct = "postgres://wfm:s3cret@wfm-db-rw:5432/wfm?sslmode=require"
	cases := []struct {
		name              string
		vars              map[string]string
		pooled            bool
		appURL, directURL string
	}{
		{"no pooler", map[string]string{"WFM_DATABASE_URL": direct}, false, direct, direct},
		{
			"pooler host with port",
			map[string]string{"WFM_DATABASE_URL": direct, "WFM_DATABASE_POOLER_HOST": "wfm-db-pooler-rw:6432"},
			true, "postgres://wfm:s3cret@wfm-db-pooler-rw:6432/wfm?sslmode=require", direct,
		},
		{
			"pooler host without port keeps 5432",
			map[string]string{"WFM_DATABASE_URL": direct, "WFM_DATABASE_POOLER_HOST": "wfm-db-pooler-rw"},
			true, "postgres://wfm:s3cret@wfm-db-pooler-rw:5432/wfm?sslmode=require", direct,
		},
		{
			"url points at the pooler, direct url given",
			map[string]string{"WFM_DATABASE_URL": "postgres://wfm:s3cret@pgbouncer:6432/wfm", "WFM_DATABASE_DIRECT_URL": direct},
			true, "postgres://wfm:s3cret@pgbouncer:6432/wfm", direct,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := load(t, tc.vars)
			if err != nil {
				t.Fatal(err)
			}
			if c.DatabasePooled() != tc.pooled {
				t.Errorf("pooled = %v, want %v", c.DatabasePooled(), tc.pooled)
			}
			if got := c.AppDatabaseURL(); got != tc.appURL {
				t.Errorf("app url = %q, want %q", got, tc.appURL)
			}
			if got := c.DirectDatabaseURL(); got != tc.directURL {
				t.Errorf("direct url = %q, want %q", got, tc.directURL)
			}
		})
	}
}

func TestDatabaseRoutingErrors(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"both set", map[string]string{"WFM_DATABASE_POOLER_HOST": "p", "WFM_DATABASE_DIRECT_URL": "postgres://x/y"}, "only one of"},
		{"host with a path", map[string]string{"WFM_DATABASE_POOLER_HOST": "p:6432/wfm"}, "want host or host:port"},
		{"keyword dsn", map[string]string{"WFM_DATABASE_URL": "host=db user=wfm", "WFM_DATABASE_POOLER_HOST": "p"}, "URL form"},
		{"poll too fast", map[string]string{"WFM_WORKER_POLL_INTERVAL": "10ms"}, "WFM_WORKER_POLL_INTERVAL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.vars)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
