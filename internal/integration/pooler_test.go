package integration

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/events"
)

// TestPooledDatabase runs the app as it runs behind a transaction-mode pooler: live events
// arrive over a dedicated direct LISTEN session rather than a pooled connection, and the app
// still works end to end. Both ways of configuring it are covered.
func TestPooledDatabase(t *testing.T) {
	// Options run after the harness has put the test database's URL in the environment, so
	// the "pooler" here is the same server: what is under test is the routing.
	cases := []struct {
		name string
		opt  envOption
	}{
		{"pooler host", func(m map[string]string) {
			u, _ := url.Parse(m["WFM_DATABASE_URL"])
			m["WFM_DATABASE_POOLER_HOST"] = u.Host
		}},
		{"direct url", func(m map[string]string) { m["WFM_DATABASE_DIRECT_URL"] = m["WFM_DATABASE_URL"] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.opt)
			if !e.Cfg.DatabasePooled() {
				t.Fatal("expected pooled mode")
			}
			if e.App.Hub.ListenURL == "" {
				t.Fatal("hub should listen on a direct connection when pooled")
			}

			project := uuid.New()
			ch, unsubscribe := e.App.Hub.Subscribe(project)
			defer unsubscribe()

			// The listener connects asynchronously; publish until an event arrives.
			deadline := time.After(10 * time.Second)
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for received := false; !received; {
				err := db.Tx(context.Background(), e.Pool, func(tx pgx.Tx, _ *dbq.Queries) error {
					return events.Publish(context.Background(), tx, events.Event{Type: "issue.updated", ProjectID: project})
				})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case ev := <-ch:
					if ev.ProjectID != project || ev.Type != "issue.updated" {
						t.Fatalf("unexpected event %+v", ev)
					}
					received = true
				case <-tick.C:
				case <-deadline:
					t.Fatal("no live event arrived over the direct listener")
				}
			}
			// The listener holds its own session, not one of the app pool's connections.
			if n := e.Pool.Stat().AcquiredConns(); n != 0 {
				t.Errorf("pool has %d connections held while idle; the listener should not use the pool", n)
			}

			// And the app works normally through the "pooler" address.
			admin := e.login("admin@example.com", "Admin")
			var p obj
			admin.must("POST", "/api/v1/projects", obj{"key": "POOL", "name": "Pooled", "template": "kanban"}, 201, &p)
			var issue obj
			admin.must("POST", "/api/v1/issues", obj{"projectKey": "POOL", "typeKey": "task", "summary": "Through the pooler"}, 201, &issue)
			eq(t, str(issue, "key"), "POOL-1", "issue created through the pooled connection")
		})
	}
}
