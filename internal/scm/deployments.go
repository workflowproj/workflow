package scm

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
)

/*
 * Tags and deployments.
 *
 * A new tag links the release named like it in each project connected to the repository (see
 * tracker.TagPushed). A deployment is stored as its statuses arrive; once it completes, a job asks
 * the provider for the commits since the previous successful deployment to the same environment
 * and associates the deployment with the issues those commits mention or are linked to. The
 * first deployment to an environment only has its head commit. Each issue then gets
 * deployment.completed (automation, outbound webhooks).
 */

// maxDeploymentCommits caps the commits read for one deployment.
const maxDeploymentCommits = 250

// applyTag links a new tag to the releases it names.
func (s *Service) applyTag(ctx context.Context, c dbq.ScmConnection, ev TagEvent) (int, error) {
	sc, err := s.scopeFor(ctx, c, ev.Repo)
	if err != nil || len(sc.projects) == 0 {
		return 0, err
	}
	n := 0
	for _, p := range sc.projects {
		ok, err := s.Tracker.TagPushed(ctx, scmActor, p, sc.repo, ev.Name, ev.SHA)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// applyDeployment records a deployment's status and, when it completed with an outcome not yet
// handled, queues the job that finds its issues.
func (s *Service) applyDeployment(ctx context.Context, c dbq.ScmConnection, ev DeploymentEvent) (int, error) {
	if ev.ID == "" || ev.Environment == "" || ev.Status == "" {
		return 0, nil
	}
	sc, err := s.scopeFor(ctx, c, ev.Repo)
	if err != nil || len(sc.projects) == 0 {
		return 0, err
	}
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	err = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		d, err := q.UpsertDeployment(ctx, dbq.UpsertDeploymentParams{
			RepositoryID: sc.repo.ID, ExternalID: ev.ID, Environment: ev.Environment, Ref: ev.Ref, Sha: ev.SHA,
			Status: ev.Status, Url: ev.URL, Deployer: ev.Deployer, StatusAt: at,
		})
		if err != nil {
			return err
		}
		if !completed(d.Status) || deref(d.NotifiedStatus) == d.Status || s.Jobs == nil {
			return nil
		}
		_, err = s.Jobs.InsertTx(ctx, tx, jobs.SCMDeployment{DeploymentID: d.ID}, nil)
		return err
	})
	return 1, err
}

func completed(status string) bool { return status == "success" || status == "failed" }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ResolveDeployment runs as a job: it works out the issues a completed deployment contains (once)
// and raises deployment.completed on each for the deployment's current outcome (once per outcome).
func (s *Service) ResolveDeployment(ctx context.Context, id uuid.UUID) error {
	d, err := s.Q.GetDeployment(ctx, id)
	if err != nil {
		return nil // gone with its repository
	}
	if !completed(d.Status) || d.ResolvedAt != nil && deref(d.NotifiedStatus) == d.Status {
		return nil
	}
	repo, err := s.Q.GetScmRepository(ctx, d.RepositoryID)
	if err != nil {
		return nil
	}
	c, err := s.Q.GetScmConnection(ctx, repo.ConnectionID)
	if err != nil {
		return nil
	}
	sc, err := s.scopeOf(ctx, c, repo)
	if err != nil {
		return err
	}
	var contents deploymentContents
	if d.ResolvedAt == nil {
		if contents, err = s.contentsOf(ctx, c, sc, d); err != nil {
			s.Log.Warn("deployment contents", "repository", repo.FullPath, "environment", d.Environment, "sha", d.Sha, "error", err)
			return err // River retries with backoff
		}
	}
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		d, err := q.LockDeployment(ctx, id)
		if err != nil {
			return err
		}
		if d.ResolvedAt == nil {
			for _, issueID := range contents.issues {
				if err := q.AddDeploymentIssue(ctx, dbq.AddDeploymentIssueParams{DeploymentID: d.ID, IssueID: issueID}); err != nil {
					return err
				}
			}
			if err := q.MarkDeploymentResolved(ctx, dbq.MarkDeploymentResolvedParams{ID: d.ID, BaseSha: contents.base, CommitCount: int32(contents.commits), Truncated: contents.truncated}); err != nil {
				return err
			}
		}
		if !completed(d.Status) || deref(d.NotifiedStatus) == d.Status {
			return nil
		}
		issueIDs, err := q.DeploymentIssueIDs(ctx, d.ID)
		if err != nil {
			return err
		}
		data := map[string]any{
			"environment": d.Environment, "status": d.Status, "url": d.Url, "ref": d.Ref, "sha": d.Sha,
			"deployer": d.Deployer, "deploymentId": d.ID, "repository": repo.FullPath, "provider": c.Provider,
		}
		for _, issueID := range issueIDs {
			i, err := q.GetIssue(ctx, issueID)
			if err != nil {
				continue
			}
			p, ok := sc.projects[i.ProjectID]
			if !ok {
				continue // the project was disconnected from the repository since
			}
			if err := s.Tracker.Emit(ctx, tx, scmActor, p, &i, "deployment.completed", maps(data)); err != nil {
				return err
			}
		}
		return q.SetDeploymentNotified(ctx, dbq.SetDeploymentNotifiedParams{ID: d.ID, Status: &d.Status})
	})
}

// maps copies event data, since emitting adds the issue's key and summary to it.
func maps(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	return out
}

type deploymentContents struct {
	issues    []uuid.UUID
	base      *string
	commits   int
	truncated bool
}

// contentsOf finds the issues a deployment brought to its environment: those mentioned by, or
// already linked to, the commits since the previous successful deployment there (only the head
// commit for the first one). A comparison the provider cannot make (the base was rewritten away)
// falls back to the head commit.
func (s *Service) contentsOf(ctx context.Context, c dbq.ScmConnection, sc *scope, d dbq.Deployment) (deploymentContents, error) {
	var out deploymentContents
	if d.Sha == "" || len(sc.projects) == 0 {
		return out, nil
	}
	prev, err := s.Q.PreviousSuccessfulDeployment(ctx, dbq.PreviousSuccessfulDeploymentParams{RepositoryID: d.RepositoryID, Environment: d.Environment, ID: d.ID, Before: d.StatusAt})
	switch {
	case db.IsNoRows(err):
	case err != nil:
		return out, err
	default:
		out.base = &prev.Sha
	}
	if out.base != nil && *out.base == d.Sha {
		return out, nil // a redeploy of what is already there brings nothing new
	}
	cl, err := s.client(ctx, c)
	if err != nil {
		return out, err
	}
	ref := repoRef(sc.repo)
	commits, truncated, err := cl.CommitsBetween(ctx, ref, deref(out.base), d.Sha, maxDeploymentCommits)
	if err != nil && out.base != nil && IsAPIStatus(err, http.StatusNotFound, http.StatusUnprocessableEntity) {
		s.Log.Info("deployment compare failed; using the head commit", "repository", sc.repo.FullPath, "base", *out.base, "head", d.Sha, "error", err)
		commits, _, err = cl.CommitsBetween(ctx, ref, "", d.Sha, 1)
		truncated = true
	}
	if err != nil {
		return out, err
	}
	out.commits, out.truncated = len(commits), truncated
	shas := []string{d.Sha}
	var messages []string
	for _, cm := range commits {
		shas = append(shas, cm.SHA)
		messages = append(messages, cm.Message)
	}
	linked, err := s.Q.IssuesLinkedToCommits(ctx, dbq.IssuesLinkedToCommitsParams{RepositoryID: sc.repo.ID, Shas: shas})
	if err != nil {
		return out, err
	}
	for _, id := range linked {
		if i, err := s.Q.GetIssue(ctx, id); err == nil {
			if _, ok := sc.projects[i.ProjectID]; ok && !slices.Contains(out.issues, id) {
				out.issues = append(out.issues, id)
			}
		}
	}
	for _, key := range ExtractKeys(sc.allowed, messages...) {
		if i, _, ok := sc.issue(ctx, s, s.Q, key); ok && !slices.Contains(out.issues, i.ID) {
			out.issues = append(out.issues, i.ID)
		}
	}
	return out, nil
}
