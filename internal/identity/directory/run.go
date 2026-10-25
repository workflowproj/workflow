package directory

import (
	"context"
	"encoding/json"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

// Runner is a directory sync: Google Workspace (Syncer) or Keycloak (KeycloakSyncer). The app,
// the directory_sync job and the admin "Run now" button use whichever one is configured.
type Runner interface {
	// Run performs one sync and records it in directory_sync_runs and the audit log.
	Run(ctx context.Context, trigger string) (Stats, error)
	// Source is the directory's name in directory_sync_runs.source ("google", "keycloak").
	Source() string
}

// Stats counts what one run found and changed, e.g. usersCreated, groupsDeleted.
type Stats map[string]int

// recordRun wraps one sync in a directory_sync_runs row and a directory.sync audit event.
func recordRun(ctx context.Context, q *dbq.Queries, source, trigger string, sync func(context.Context) (Stats, error)) (Stats, error) {
	run, err := q.CreateDirectorySyncRun(ctx, dbq.CreateDirectorySyncRunParams{Trigger: trigger, Source: source})
	if err != nil {
		return nil, err
	}
	stats, runErr := sync(ctx)
	status := "succeeded"
	var errMsg *string
	if runErr != nil {
		status = "failed"
		m := runErr.Error()
		errMsg = &m
	}
	b, _ := json.Marshal(stats)
	done := context.WithoutCancel(ctx)
	if err := q.FinishDirectorySyncRun(done, dbq.FinishDirectorySyncRunParams{ID: run.ID, Status: status, Stats: b, Error: errMsg}); err != nil {
		return stats, err
	}
	data := map[string]any{"trigger": trigger, "status": status, "source": source}
	for k, v := range stats {
		data[k] = v
	}
	tracker.Audit(done, q, authz.SystemActor(), "directory.sync", "directory", run.ID.String(), nil, data)
	return stats, runErr
}
