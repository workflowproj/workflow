package scm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// GitLab implements Provider for gitlab.com and self-managed GitLab, authenticated with a
// group or project access token.
type GitLab struct{}

func (GitLab) Name() string { return "gitlab" }

func (GitLab) Verify(r *http.Request, body []byte, secret string) (Delivery, error) {
	if secret == "" || !secure.Equal(r.Header.Get("X-Gitlab-Token"), secret) {
		return Delivery{}, ErrBadSignature
	}
	d := Delivery{Event: r.Header.Get("X-Gitlab-Event")}
	for _, h := range []string{"X-Gitlab-Event-UUID", "X-Gitlab-Webhook-UUID", "Idempotency-Key"} {
		if v := r.Header.Get(h); v != "" {
			d.ID = v
			break
		}
	}
	if d.ID == "" {
		d.ID = "sha256:" + secure.Hash(string(body)) // dedupe on content when no UUID is sent
	}
	if d.Event == "" {
		return d, errors.New("missing X-Gitlab-Event")
	}
	return d, nil
}

type glProject struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	DefaultBranch     string `json:"default_branch"`
}

func (p glProject) ref() RepoRef {
	return RepoRef{ExternalID: strconv.FormatInt(p.ID, 10), FullPath: p.PathWithNamespace, WebURL: p.WebURL, DefaultBranch: p.DefaultBranch}
}

type glMR struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	WebURL       string `json:"web_url"`
	State        string `json:"state"`
	Action       string `json:"action"`
	Draft        bool   `json:"draft"`
	WIP          bool   `json:"work_in_progress"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	UpdatedAt    string `json:"updated_at"`
	LastCommit   struct {
		ID string `json:"id"`
	} `json:"last_commit"`
	SHA    string `json:"sha"`
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
}

// GitLab timestamps come in two formats depending on the API surface.
func parseGitLabTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now()
}

func (m glMR) event(repo RepoRef, action string) PullRequestEvent {
	state := map[string]string{"opened": "open", "merged": "merged", "closed": "closed", "locked": "open"}[m.State]
	if state == "" {
		state = "open"
	}
	if state == "open" && (m.Draft || m.WIP) {
		state = "draft"
	}
	u := m.URL
	if u == "" {
		u = m.WebURL
	}
	sha := m.LastCommit.ID
	if sha == "" {
		sha = m.SHA
	}
	return PullRequestEvent{
		Repo: repo, Number: strconv.Itoa(m.IID), Title: m.Title, Body: m.Description, URL: u, State: state, Action: action,
		SourceBranch: m.SourceBranch, TargetBranch: m.TargetBranch, Author: m.Author.Username, HeadSHA: sha, UpdatedAt: parseGitLabTime(m.UpdatedAt),
	}
}

func (GitLab) Parse(eventType string, payload []byte) ([]any, error) {
	var base struct {
		ObjectKind string    `json:"object_kind"`
		Project    glProject `json:"project"`
	}
	if err := json.Unmarshal(payload, &base); err != nil {
		return nil, err
	}
	repo := base.Project.ref()
	switch base.ObjectKind {
	case "push":
		var p struct {
			Ref     string `json:"ref"`
			Before  string `json:"before"`
			After   string `json:"after"`
			Commits []struct {
				ID        string `json:"id"`
				Message   string `json:"message"`
				URL       string `json:"url"`
				Timestamp string `json:"timestamp"`
				Author    struct {
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"author"`
			} `json:"commits"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
		if !ok {
			return nil, nil
		}
		zero := strings.Repeat("0", 40)
		ev := PushEvent{Repo: repo, Branch: branch, Created: p.Before == zero, Deleted: p.After == zero}
		for _, c := range p.Commits {
			ev.Commits = append(ev.Commits, Commit{SHA: c.ID, Message: c.Message, URL: c.URL, AuthorName: c.Author.Name, AuthorEmail: strings.ToLower(c.Author.Email), Timestamp: parseGitLabTime(c.Timestamp)})
		}
		return []any{ev}, nil
	case "merge_request":
		var p struct {
			ObjectAttributes glMR `json:"object_attributes"`
			User             struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		mr := p.ObjectAttributes
		action := map[string]string{
			"open": "opened", "reopen": "reopened", "close": "closed", "merge": "merged", "update": "updated",
			"approved": "approved", "unapproved": "unapproved", "approval": "approved", "unapproval": "unapproved",
		}[mr.Action]
		switch action {
		case "":
			return nil, nil
		case "approved", "unapproved":
			state := "approved"
			if action == "unapproved" {
				state = "commented"
			}
			return []any{ReviewEvent{Repo: repo, Number: strconv.Itoa(mr.IID), State: state, Reviewer: p.User.Username}}, nil
		}
		return []any{mr.event(repo, action)}, nil
	case "pipeline":
		var p struct {
			ObjectAttributes struct {
				ID         int64  `json:"id"`
				Ref        string `json:"ref"`
				Tag        bool   `json:"tag"`
				SHA        string `json:"sha"`
				Status     string `json:"status"`
				Name       string `json:"name"`
				FinishedAt string `json:"finished_at"`
				CreatedAt  string `json:"created_at"`
			} `json:"object_attributes"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		a := p.ObjectAttributes
		if a.Tag {
			return nil, nil
		}
		status := map[string]string{
			"created": "pending", "waiting_for_resource": "pending", "preparing": "pending", "pending": "pending", "scheduled": "pending",
			"running": "running", "success": "success", "failed": "failed", "canceled": "canceled", "skipped": "canceled", "manual": "pending",
		}[a.Status]
		if status == "" {
			status = "running"
		}
		name := a.Name
		if name == "" {
			name = "Pipeline #" + strconv.FormatInt(a.ID, 10)
		}
		updated := parseGitLabTime(a.CreatedAt)
		if a.FinishedAt != "" {
			updated = parseGitLabTime(a.FinishedAt)
		}
		return []any{PipelineEvent{Repo: repo, ID: strconv.FormatInt(a.ID, 10), Name: name, Status: status,
			URL: fmt.Sprintf("%s/-/pipelines/%d", repo.WebURL, a.ID), Branch: a.Ref, SHA: a.SHA, CommitMessage: p.Commit.Message, UpdatedAt: updated}}, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// API client

type gitlabClient struct {
	conn Connection
	http *http.Client
}

func (GitLab) Client(_ context.Context, conn Connection, httpClient *http.Client) (Client, error) {
	if conn.Token == "" {
		return nil, errors.New("the GitLab connection needs an access token")
	}
	return &gitlabClient{conn: conn, http: httpClient}, nil
}

func (c *gitlabClient) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.conn.APIURL, "/")+"/api/v4"+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.conn.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("gitlab %s %s: HTTP %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func projectPath(repo RepoRef) string { return "/projects/" + url.PathEscape(repo.ExternalID) }

func (c *gitlabClient) ListRepositories(ctx context.Context) ([]RepoRef, error) {
	var out []RepoRef
	for page := 1; page <= 50; page++ {
		var ps []glProject
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/projects?membership=true&simple=true&archived=false&per_page=100&page=%d", page), nil, &ps); err != nil {
			return nil, err
		}
		for _, p := range ps {
			out = append(out, p.ref())
		}
		if len(ps) < 100 {
			break
		}
	}
	return out, nil
}

func (c *gitlabClient) ListPullRequests(ctx context.Context, repo RepoRef) ([]PullRequest, error) {
	var mrs []glMR
	if err := c.do(ctx, http.MethodGet, projectPath(repo)+"/merge_requests?state=all&order_by=updated_at&sort=desc&per_page=50", nil, &mrs); err != nil {
		return nil, err
	}
	out := make([]PullRequest, len(mrs))
	for i, m := range mrs {
		out[i] = m.event(repo, "reconciled")
	}
	return out, nil
}

func (c *gitlabClient) ListBranches(ctx context.Context, repo RepoRef) ([]string, error) {
	var bs []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, projectPath(repo)+"/repository/branches?per_page=100", nil, &bs); err != nil {
		return nil, err
	}
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
	}
	return out, nil
}

func (c *gitlabClient) Comment(ctx context.Context, repo RepoRef, number, body string) error {
	return c.do(ctx, http.MethodPost, projectPath(repo)+"/merge_requests/"+url.PathEscape(number)+"/notes", map[string]string{"body": body}, nil)
}

func (c *gitlabClient) CreateBranch(ctx context.Context, repo RepoRef, name, from string) error {
	q := url.Values{"branch": {name}, "ref": {from}}
	return c.do(ctx, http.MethodPost, projectPath(repo)+"/repository/branches?"+q.Encode(), nil, nil)
}

// EnsureHook creates a project webhook through the API (group hooks need Premium).
func (c *gitlabClient) EnsureHook(ctx context.Context, repo RepoRef, hookURL, secret string) (string, error) {
	var hooks []struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodGet, projectPath(repo)+"/hooks", nil, &hooks); err != nil {
		return "", err
	}
	body := map[string]any{
		"url": hookURL, "token": secret, "push_events": true, "merge_requests_events": true,
		"pipeline_events": true, "tag_push_events": false, "enable_ssl_verification": true,
	}
	for _, h := range hooks {
		if h.URL == hookURL {
			id := strconv.FormatInt(h.ID, 10)
			return id, c.do(ctx, http.MethodPut, projectPath(repo)+"/hooks/"+id, body, nil)
		}
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, projectPath(repo)+"/hooks", body, &created); err != nil {
		return "", err
	}
	return strconv.FormatInt(created.ID, 10), nil
}
