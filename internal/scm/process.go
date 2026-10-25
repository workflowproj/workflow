package scm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/tracker"
)

// scope is what a repository may link to: the projects connected to it.
type scope struct {
	repo     dbq.ScmRepository
	conn     dbq.ScmConnection
	projects map[uuid.UUID]dbq.Project
	allowed  map[string]bool // project keys and their aliases
}

// ProcessEvent handles one stored webhook event. It is idempotent: dev links are upserted and
// smart commits only run the first time a commit is linked to an issue.
func (s *Service) ProcessEvent(ctx context.Context, eventID uuid.UUID) error {
	ev, err := s.Q.GetWebhookEvent(ctx, eventID)
	if err != nil {
		return err
	}
	if ev.Status == "processed" || ev.Status == "ignored" {
		return nil
	}
	c, err := s.Q.GetScmConnection(ctx, ev.ConnectionID)
	if err != nil {
		return err
	}
	provider := s.Providers[c.Provider]
	parsed, err := provider.Parse(ev.EventType, ev.Payload)
	if err != nil {
		msg := "unparseable payload: " + err.Error()
		return s.Q.FinishWebhookEvent(ctx, dbq.FinishWebhookEventParams{ID: ev.ID, Status: "failed", Error: &msg})
	}
	linked := 0
	for _, e := range parsed {
		n, err := s.apply(ctx, c, e, true)
		if err != nil {
			msg := err.Error()
			_ = s.Q.FinishWebhookEvent(ctx, dbq.FinishWebhookEventParams{ID: ev.ID, Status: "failed", Error: &msg})
			return err // River retries with backoff
		}
		linked += n
	}
	status := "processed"
	if linked == 0 {
		status = "ignored"
	}
	return s.Q.FinishWebhookEvent(ctx, dbq.FinishWebhookEventParams{ID: ev.ID, Status: status})
}

func (s *Service) scopeFor(ctx context.Context, c dbq.ScmConnection, ref RepoRef) (*scope, error) {
	var install *int64
	if ref.InstallationID != 0 {
		install = &ref.InstallationID
	}
	branch := ref.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	repo, err := s.Q.UpsertScmRepository(ctx, dbq.UpsertScmRepositoryParams{
		ConnectionID: c.ID, ExternalID: ref.ExternalID, FullPath: ref.FullPath, WebUrl: ref.WebURL, DefaultBranch: branch, InstallationID: install,
	})
	if err != nil {
		return nil, err
	}
	projects, err := s.Q.RepositoryProjects(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	sc := &scope{repo: repo, conn: c, projects: map[uuid.UUID]dbq.Project{}, allowed: map[string]bool{}}
	for _, p := range projects {
		sc.projects[p.ID] = p
		sc.allowed[p.Key] = true
		aliases, err := s.Q.ProjectKeyAliases(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range aliases {
			sc.allowed[a] = true
		}
	}
	return sc, nil
}

func (sc *scope) issue(ctx context.Context, s *Service, q *dbq.Queries, key string) (dbq.Issue, dbq.Project, bool) {
	i, err := s.Tracker.ResolveIssue(ctx, q, key)
	if err != nil {
		return i, dbq.Project{}, false
	}
	p, ok := sc.projects[i.ProjectID]
	return i, p, ok
}

var scmActor = &authz.Actor{Type: authz.ActorSCM, OrgRole: "member"}

// apply links one normalised event. live=false (reconcile) skips automation triggers.
func (s *Service) apply(ctx context.Context, c dbq.ScmConnection, e any, live bool) (int, error) {
	switch ev := e.(type) {
	case RepositoriesEvent:
		for _, r := range ev.Added {
			if _, err := s.scopeFor(ctx, c, r); err != nil {
				return 0, err
			}
		}
		return len(ev.Added), nil
	case PushEvent:
		return s.applyPush(ctx, c, ev, live)
	case BranchEvent:
		sc, err := s.scopeFor(ctx, c, ev.Repo)
		if err != nil || len(sc.projects) == 0 {
			return 0, err
		}
		if ev.Deleted {
			return 1, s.Q.UpdateDevLinksState(ctx, dbq.UpdateDevLinksStateParams{RepositoryID: sc.repo.ID, Kind: "branch", ExternalID: ev.Branch, State: "deleted", Data: []byte(`{}`)})
		}
		return s.linkBranch(ctx, sc, ev.Branch, live, time.Now())
	case PullRequestEvent:
		return s.applyPR(ctx, c, ev, live)
	case ReviewEvent:
		sc, err := s.scopeFor(ctx, c, ev.Repo)
		if err != nil || len(sc.projects) == 0 {
			return 0, err
		}
		links, err := s.Q.IssueDevLinksForRepo(ctx, dbq.IssueDevLinksForRepoParams{RepositoryID: sc.repo.ID, Kind: "pull_request", ExternalID: ev.Number})
		if err != nil {
			return 0, err
		}
		data, _ := json.Marshal(map[string]any{"reviewState": ev.State, "reviewer": ev.Reviewer})
		for _, l := range links {
			if err := s.Q.MergeDevLinkData(ctx, dbq.MergeDevLinkDataParams{ID: l.ID, Data: data}); err != nil {
				return 0, err
			}
			s.refresh(ctx, sc, l.IssueID)
		}
		return len(links), nil
	case PipelineEvent:
		return s.applyPipeline(ctx, c, ev, live)
	}
	return 0, nil
}

func (s *Service) refresh(ctx context.Context, sc *scope, issueID uuid.UUID) {
	_ = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		i, err := q.GetIssue(ctx, issueID)
		if err != nil {
			return err
		}
		p, ok := sc.projects[i.ProjectID]
		if !ok {
			return nil
		}
		return s.Tracker.Refresh(ctx, tx, scmActor, p, &i)
	})
}

// upsertLink writes a dev link and, when trigger returns an event name, emits that automation
// trigger in the same transaction. trigger sees the upserted row (e.g. to fire only on insert).
func (s *Service) upsertLink(ctx context.Context, sc *scope, key string, params dbq.UpsertDevLinkParams, trigger func(dbq.UpsertDevLinkRow) string, triggerData map[string]any) (dbq.UpsertDevLinkRow, bool, error) {
	var row dbq.UpsertDevLinkRow
	found := false
	err := db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		i, p, ok := sc.issue(ctx, s, q, key)
		if !ok {
			return nil
		}
		found = true
		params.IssueID = i.ID
		params.RepositoryID = sc.repo.ID
		if params.Data == nil {
			params.Data = []byte(`{}`)
		}
		var err error
		if row, err = q.UpsertDevLink(ctx, params); err != nil {
			return err
		}
		if event := triggerName(trigger, row); event != "" {
			if triggerData == nil {
				triggerData = map[string]any{}
			}
			triggerData["repository"] = sc.repo.FullPath
			triggerData["provider"] = sc.conn.Provider
			return s.Tracker.Emit(ctx, tx, scmActor, p, &i, event, triggerData)
		}
		return s.Tracker.Refresh(ctx, tx, scmActor, p, &i)
	})
	return row, found, err
}

func triggerName(fn func(dbq.UpsertDevLinkRow) string, row dbq.UpsertDevLinkRow) string {
	if fn == nil {
		return ""
	}
	return fn(row)
}

// onInsert fires an event only the first time a link is created (re-deliveries stay quiet).
func onInsert(live bool, event string) func(dbq.UpsertDevLinkRow) string {
	return func(row dbq.UpsertDevLinkRow) string {
		if live && row.Inserted {
			return event
		}
		return ""
	}
}

func always(event string) func(dbq.UpsertDevLinkRow) string {
	return func(dbq.UpsertDevLinkRow) string { return event }
}

func (s *Service) linkBranch(ctx context.Context, sc *scope, branch string, live bool, at time.Time) (int, error) {
	n := 0
	for _, key := range ExtractKeys(sc.allowed, branch) {
		_, found, err := s.upsertLink(ctx, sc, key, dbq.UpsertDevLinkParams{
			Kind: "branch", ExternalID: branch, Title: branch, Url: branchURL(sc.conn.Provider, sc.repo, branch), State: "active", EventAt: at,
		}, onInsert(live, "branch.created"), map[string]any{"branch": branch})
		if err != nil {
			return n, err
		}
		if found {
			n++
		}
	}
	return n, nil
}

func (s *Service) applyPush(ctx context.Context, c dbq.ScmConnection, ev PushEvent, live bool) (int, error) {
	sc, err := s.scopeFor(ctx, c, ev.Repo)
	if err != nil || len(sc.projects) == 0 {
		return 0, err
	}
	if ev.Deleted {
		return 1, s.Q.UpdateDevLinksState(ctx, dbq.UpdateDevLinksStateParams{RepositoryID: sc.repo.ID, Kind: "branch", ExternalID: ev.Branch, State: "deleted", Data: []byte(`{}`)})
	}
	n, err := s.linkBranch(ctx, sc, ev.Branch, live, time.Now())
	if err != nil {
		return n, err
	}
	for _, cm := range ev.Commits {
		keys := ExtractKeys(sc.allowed, cm.Message)
		data, _ := json.Marshal(map[string]any{"branch": ev.Branch, "sha": cm.SHA})
		for _, key := range keys {
			at := cm.Timestamp
			if at.IsZero() {
				at = time.Now()
			}
			row, found, err := s.upsertLink(ctx, sc, key, dbq.UpsertDevLinkParams{
				Kind: "commit", ExternalID: cm.SHA, Title: firstLine(cm.Message), Url: cm.URL,
				AuthorName: cm.AuthorName, AuthorEmail: cm.AuthorEmail, Data: data, EventAt: at,
			}, onInsert(live, "commit.pushed"), map[string]any{"sha": cm.SHA, "branch": ev.Branch, "message": firstLine(cm.Message)})
			if err != nil {
				return n, err
			}
			if !found {
				continue
			}
			n++
			if row.Inserted && live {
				s.runSmartCommits(ctx, sc, key, cm)
			}
		}
	}
	return n, nil
}

// runSmartCommits executes "#comment" and "#transition" commands as the committer, mapped by
// verified email. Unknown committers still get linked but cannot change issues. Transitions go
// through the workflow engine, so conditions and validators apply.
func (s *Service) runSmartCommits(ctx context.Context, sc *scope, key string, cm Commit) {
	cmds := ParseSmartCommits(sc.allowed, cm.Message)
	if len(cmds) == 0 {
		return
	}
	user, err := s.Q.GetUserByEmail(ctx, cm.AuthorEmail)
	if err != nil || !user.Active || user.DeletedAt != nil {
		s.Log.Info("smart commit ignored: committer is not a known active user", "email", cm.AuthorEmail, "sha", cm.SHA)
		return
	}
	role, err := s.Tracker.EffectiveOrgRole(ctx, user)
	if err != nil {
		return
	}
	actor := &authz.Actor{User: &user, Type: authz.ActorSCM, OrgRole: role}
	for _, cmd := range cmds {
		if !containsKey(cmd.Keys, key) {
			continue
		}
		issue, _, ok := sc.issue(ctx, s, s.Q, key)
		if !ok {
			continue
		}
		switch cmd.Command {
		case "comment":
			if _, err := s.Tracker.AddComment(ctx, actor, issue.ID.String(), cmd.Arg+fmt.Sprintf("\n\n<sub>via commit %s</sub>", shortSHA(cm.SHA))); err != nil {
				s.Log.Warn("smart commit comment failed", "issue", key, "error", err)
			}
		default:
			ran, err := s.Tracker.TransitionByName(ctx, actor, issue.ID, cmd.Command)
			if err != nil {
				s.Log.Warn("smart commit transition refused", "issue", key, "command", cmd.Command, "error", err)
			} else if !ran {
				s.Log.Info("smart commit transition not available", "issue", key, "command", cmd.Command)
			}
		}
	}
}

func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (s *Service) applyPR(ctx context.Context, c dbq.ScmConnection, ev PullRequestEvent, live bool) (int, error) {
	sc, err := s.scopeFor(ctx, c, ev.Repo)
	if err != nil || len(sc.projects) == 0 {
		return 0, err
	}
	data, _ := json.Marshal(map[string]any{"number": ev.Number, "sourceBranch": ev.SourceBranch, "targetBranch": ev.TargetBranch, "headSha": ev.HeadSHA})
	// Keep every issue's copy of this PR current, even ones whose key left the title.
	if err := s.Q.UpdateDevLinksState(ctx, dbq.UpdateDevLinksStateParams{RepositoryID: sc.repo.ID, Kind: "pull_request", ExternalID: ev.Number, State: ev.State, Data: data}); err != nil {
		return 0, err
	}
	trigger := ""
	if live {
		switch ev.Action {
		case "opened", "reopened", "ready":
			if ev.State == "open" {
				trigger = "pr.opened"
			}
		case "merged":
			trigger = "pr.merged"
		case "closed":
			trigger = "pr.closed"
		}
	}
	at := ev.UpdatedAt
	if at.IsZero() {
		at = time.Now()
	}
	n := 0
	for _, key := range ExtractKeys(sc.allowed, ev.Title, ev.SourceBranch, ev.Body) {
		row, found, err := s.upsertLink(ctx, sc, key, dbq.UpsertDevLinkParams{
			Kind: "pull_request", ExternalID: ev.Number, Title: ev.Title, Url: ev.URL, State: ev.State, AuthorName: ev.Author, Data: data, EventAt: at,
		}, always(trigger), map[string]any{"number": ev.Number, "title": ev.Title, "url": ev.URL, "state": ev.State, "branch": ev.SourceBranch})
		if err != nil {
			return n, err
		}
		if !found {
			continue
		}
		n++
		if row.Inserted && live && sc.conn.WriteBack && s.Jobs != nil {
			if _, err := s.Jobs.Insert(ctx, jobs.SCMWriteBack{DevLinkID: row.ID, IssueID: row.IssueID}, nil); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

func (s *Service) applyPipeline(ctx context.Context, c dbq.ScmConnection, ev PipelineEvent, live bool) (int, error) {
	sc, err := s.scopeFor(ctx, c, ev.Repo)
	if err != nil || len(sc.projects) == 0 {
		return 0, err
	}
	trigger := ""
	if live && (ev.Status == "success" || ev.Status == "failed") {
		trigger = "pipeline.completed"
	}
	data, _ := json.Marshal(map[string]any{"branch": ev.Branch, "sha": ev.SHA})
	at := ev.UpdatedAt
	if at.IsZero() {
		at = time.Now()
	}
	n := 0
	for _, key := range ExtractKeys(sc.allowed, ev.Branch, ev.CommitMessage) {
		_, found, err := s.upsertLink(ctx, sc, key, dbq.UpsertDevLinkParams{
			Kind: "pipeline", ExternalID: ev.ID, Title: ev.Name, Url: ev.URL, State: ev.Status, Data: data, EventAt: at,
		}, always(trigger), map[string]any{"status": ev.Status, "pipeline": ev.Name, "url": ev.URL, "branch": ev.Branch})
		if err != nil {
			return n, err
		}
		if found {
			n++
		}
	}
	return n, nil
}

// WriteBack comments on a PR/MR the first time it is linked to an issue.
func (s *Service) WriteBack(ctx context.Context, devLinkID uuid.UUID) error {
	link, err := s.Q.GetDevLink(ctx, devLinkID)
	if err != nil {
		return nil // the link (or issue) is gone
	}
	var data map[string]any
	_ = json.Unmarshal(link.Data, &data)
	if data["writeBack"] == true {
		return nil
	}
	repo, err := s.Q.GetScmRepository(ctx, link.RepositoryID)
	if err != nil {
		return err
	}
	c, err := s.Q.GetScmConnection(ctx, repo.ConnectionID)
	if err != nil || !c.WriteBack {
		return err
	}
	issue, err := s.Q.GetIssue(ctx, link.IssueID)
	if err != nil {
		return nil
	}
	p, err := s.Q.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return err
	}
	sts, err := s.Q.GetStatusesByIDs(ctx, []uuid.UUID{issue.StatusID})
	if err != nil || len(sts) == 0 {
		return err
	}
	key := tracker.IssueKey(p, issue)
	body := fmt.Sprintf("Linked to **[%s](%s/browse/%s): %s** · status **%s**", key, s.Cfg.PublicURL, key, escapeMarkdown(issue.Summary), sts[0].Name)
	cl, err := s.client(ctx, c)
	if err != nil {
		return err
	}
	if err := cl.Comment(ctx, repoRef(repo), link.ExternalID, body); err != nil {
		return err
	}
	marker, _ := json.Marshal(map[string]any{"writeBack": true})
	return s.Q.MergeDevLinkData(ctx, dbq.MergeDevLinkDataParams{ID: link.ID, Data: marker})
}

func escapeMarkdown(s string) string {
	return strings.NewReplacer("[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`").Replace(s)
}

// Reconcile discovers repositories and refreshes branch and PR/MR links from the provider APIs,
// filling gaps left by missed deliveries. It never fires automation.
func (s *Service) Reconcile(ctx context.Context, connectionID *uuid.UUID) error {
	conns, err := s.Q.ListScmConnections(ctx)
	if err != nil {
		return err
	}
	var errs []string
	for _, row := range conns {
		if connectionID != nil && row.ID != *connectionID {
			continue
		}
		c := dbq.ScmConnection{ID: row.ID, Provider: row.Provider, Name: row.Name, ApiUrl: row.ApiUrl, WebUrl: row.WebUrl, AppID: row.AppID,
			AppSlug: row.AppSlug, SecretsEnc: row.SecretsEnc, ManageHooks: row.ManageHooks, WriteBack: row.WriteBack}
		if err := s.reconcileConnection(ctx, c); err != nil {
			s.Log.Warn("reconcile failed", "connection", c.Name, "error", err)
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("reconcile: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Service) reconcileConnection(ctx context.Context, c dbq.ScmConnection) error {
	cl, err := s.client(ctx, c)
	if err != nil {
		return err
	}
	repos, err := cl.ListRepositories(ctx)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if _, err := s.scopeFor(ctx, c, r); err != nil {
			return err
		}
	}
	linked, err := s.Q.LinkedRepositories(ctx, c.ID)
	if err != nil {
		return err
	}
	for _, repo := range linked {
		ref := repoRef(repo)
		prs, err := cl.ListPullRequests(ctx, ref)
		if err != nil {
			return fmt.Errorf("%s pull requests: %w", repo.FullPath, err)
		}
		for _, pr := range prs {
			if _, err := s.applyPR(ctx, c, pr, false); err != nil {
				return err
			}
		}
		branches, err := cl.ListBranches(ctx, ref)
		if err != nil {
			return fmt.Errorf("%s branches: %w", repo.FullPath, err)
		}
		sc, err := s.scopeFor(ctx, c, ref)
		if err != nil {
			return err
		}
		for _, b := range branches {
			if _, err := s.linkBranch(ctx, sc, b, false, time.Now()); err != nil {
				return err
			}
		}
	}
	return nil
}
