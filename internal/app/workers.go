package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/riverqueue/river"

	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
)

var (
	jobRuns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wfm_jobs_total", Help: "Background jobs by kind and outcome.",
	}, []string{"kind", "outcome"})
	jobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "wfm_job_duration_seconds", Help: "Background job duration.", Buckets: prometheus.DefBuckets,
	}, []string{"kind"})
)

func instrument[T river.JobArgs](ctx context.Context, job *river.Job[T], fn func(context.Context, T) error) error {
	start := time.Now()
	err := fn(ctx, job.Args)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	kind := job.Args.Kind()
	jobRuns.WithLabelValues(kind, outcome).Inc()
	jobDuration.WithLabelValues(kind).Observe(time.Since(start).Seconds())
	return err
}

// worker adapts a function into a River worker.
type worker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
	fn      func(context.Context, T) error
	timeout time.Duration
}

func (w *worker[T]) Work(ctx context.Context, job *river.Job[T]) error {
	return instrument(ctx, job, w.fn)
}

func (w *worker[T]) Timeout(*river.Job[T]) time.Duration {
	if w.timeout > 0 {
		return w.timeout
	}
	return time.Minute
}

func add[T river.JobArgs](ws *river.Workers, timeout time.Duration, fn func(context.Context, T) error) {
	river.AddWorker(ws, &worker[T]{fn: fn, timeout: timeout})
}

func (a *App) workers() *river.Workers {
	ws := river.NewWorkers()
	add(ws, time.Minute, a.Automation.HandleDomainEvent)
	add(ws, 2*time.Minute, func(ctx context.Context, j jobs.ProcessWebhook) error { return a.SCM.ProcessEvent(ctx, j.EventID) })
	add(ws, time.Minute, func(ctx context.Context, j jobs.SCMWriteBack) error { return a.SCM.WriteBack(ctx, j.DevLinkID) })
	add(ws, 15*time.Minute, func(ctx context.Context, j jobs.SCMReconcile) error { return a.SCM.Reconcile(ctx, j.ConnectionID) })
	add(ws, time.Minute, func(ctx context.Context, j jobs.SCMEnsureHook) error { return a.SCM.EnsureHook(ctx, j.RepositoryID) })
	add(ws, 30*time.Minute, func(ctx context.Context, j jobs.WorkflowMigrate) error {
		n, err := a.Tracker.MigrateWorkflowIssues(ctx, j.WorkflowID, j.Mapping)
		a.Log.Info("workflow migration", "workflow", j.WorkflowID, "issues", n, "error", err)
		return err
	})
	add(ws, time.Minute, a.Automation.HandlePostFunction)
	add(ws, time.Minute, func(ctx context.Context, j jobs.DeliverWebhook) error { return a.Automation.Deliver(ctx, j.DeliveryID) })
	add(ws, 30*time.Minute, func(ctx context.Context, j jobs.DirectorySync) error {
		if a.Syncer == nil {
			return nil
		}
		stats, err := a.Syncer.Run(ctx, j.Trigger)
		a.Log.Info("directory sync", "trigger", j.Trigger, "stats", stats, "error", err)
		return err
	})
	add(ws, 5*time.Minute, func(ctx context.Context, _ jobs.AutomationSchedule) error { return a.Automation.RunScheduled(ctx) })
	add(ws, 10*time.Minute, func(ctx context.Context, _ jobs.Maintenance) error { return a.maintenance(ctx) })
	return ws
}

func (a *App) periodicJobs() []*river.PeriodicJob {
	every := func(d time.Duration, args river.JobArgs) *river.PeriodicJob {
		return river.NewPeriodicJob(river.PeriodicInterval(d), func() (river.JobArgs, *river.InsertOpts) { return args, nil }, &river.PeriodicJobOpts{RunOnStart: true})
	}
	out := []*river.PeriodicJob{
		every(time.Minute, jobs.AutomationSchedule{}),
		every(time.Hour, jobs.Maintenance{}),
	}
	if a.Cfg.SCMReconcileInterval > 0 {
		out = append(out, every(a.Cfg.SCMReconcileInterval, jobs.SCMReconcile{}))
	}
	if a.Cfg.GoogleDirectory.Enabled && a.Cfg.GoogleDirectory.Interval > 0 {
		out = append(out, every(a.Cfg.GoogleDirectory.Interval, jobs.DirectorySync{Trigger: "scheduled"}))
	}
	return out
}

// maintenance purges expired sessions, anonymises users deleted upstream after the retention
// period and prunes processed webhook payloads.
func (a *App) maintenance(ctx context.Context) error {
	q := dbq.New(a.Pool)
	if n, err := q.DeleteExpiredSessions(ctx); err != nil {
		return err
	} else if n > 0 {
		a.Log.Info("expired sessions removed", "count", n)
	}
	if a.Cfg.DeletedUserRetention > 0 {
		ids, err := q.UsersToAnonymize(ctx, ptr(time.Now().Add(-a.Cfg.DeletedUserRetention)))
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := q.AnonymizeUser(ctx, id); err != nil {
				return err
			}
		}
		if len(ids) > 0 {
			a.Log.Info("users anonymised after retention", "count", len(ids))
		}
	}
	_, err := q.PruneWebhookEvents(ctx, time.Now().AddDate(0, 0, -30))
	return err
}

func ptr[T any](v T) *T { return &v }

var _ pgx.Tx
