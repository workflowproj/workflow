package scm

import (
	"bytes"
	"cmp"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/workflowproj/workflow/internal/secure"
)

// GitHub implements Provider for a GitHub App (github.com or GitHub Enterprise Server).
type GitHub struct{}

func (GitHub) Name() string { return "github" }

var ErrBadSignature = errors.New("webhook signature mismatch")

func (GitHub) Verify(r *http.Request, body []byte, secret string) (Delivery, error) {
	sig := r.Header.Get("X-Hub-Signature-256")
	want := "sha256=" + secure.HMACSHA256Hex(secret, body)
	if secret == "" || !secure.Equal(sig, want) {
		return Delivery{}, ErrBadSignature
	}
	d := Delivery{Event: r.Header.Get("X-GitHub-Event"), ID: r.Header.Get("X-GitHub-Delivery")}
	if d.Event == "" || d.ID == "" {
		return d, errors.New("missing X-GitHub-Event or X-GitHub-Delivery")
	}
	return d, nil
}

type ghRepo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghPR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	Merged    bool      `json:"merged"`
	MergedAt  *string   `json:"merged_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      ghUser    `json:"user"`
	Head      struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (r ghRepo) ref(installation int64) RepoRef {
	return RepoRef{ExternalID: strconv.FormatInt(r.ID, 10), FullPath: r.FullName, WebURL: r.HTMLURL, DefaultBranch: r.DefaultBranch, InstallationID: installation}
}

func (p ghPR) event(repo RepoRef, action string) PullRequestEvent {
	state := "open"
	switch {
	case p.Merged || p.MergedAt != nil:
		state = "merged"
	case p.State == "closed":
		state = "closed"
	case p.Draft:
		state = "draft"
	}
	return PullRequestEvent{
		Repo: repo, Number: strconv.Itoa(p.Number), Title: p.Title, Body: p.Body, URL: p.HTMLURL, State: state, Action: action,
		SourceBranch: p.Head.Ref, TargetBranch: p.Base.Ref, Author: p.User.Login, HeadSHA: p.Head.SHA, UpdatedAt: p.UpdatedAt,
	}
}

func (GitHub) Parse(eventType string, payload []byte) ([]any, error) {
	var base struct {
		Action       string `json:"action"`
		Repository   ghRepo `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(payload, &base); err != nil {
		return nil, err
	}
	repo := base.Repository.ref(base.Installation.ID)
	switch eventType {
	case "push":
		var p struct {
			Ref        string `json:"ref"`
			After      string `json:"after"`
			Created    bool   `json:"created"`
			Deleted    bool   `json:"deleted"`
			HeadCommit *struct {
				ID string `json:"id"`
			} `json:"head_commit"`
			Commits []struct {
				ID        string    `json:"id"`
				Message   string    `json:"message"`
				URL       string    `json:"url"`
				Distinct  bool      `json:"distinct"`
				Timestamp time.Time `json:"timestamp"`
				Author    struct {
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"author"`
			} `json:"commits"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if tag, isTag := strings.CutPrefix(p.Ref, "refs/tags/"); isTag {
			if !p.Created || p.Deleted {
				return nil, nil
			}
			// The head commit is what the tag points at (after is the tag object for annotated tags).
			sha := p.After
			if p.HeadCommit != nil && p.HeadCommit.ID != "" {
				sha = p.HeadCommit.ID
			}
			return []any{TagEvent{Repo: repo, Name: tag, SHA: sha}}, nil
		}
		branch, isBranch := strings.CutPrefix(p.Ref, "refs/heads/")
		if !isBranch {
			return nil, nil
		}
		ev := PushEvent{Repo: repo, Branch: branch, Created: p.Created, Deleted: p.Deleted}
		for _, c := range p.Commits {
			if !c.Distinct {
				continue // already seen on another branch
			}
			ev.Commits = append(ev.Commits, Commit{SHA: c.ID, Message: c.Message, URL: c.URL, AuthorName: c.Author.Name, AuthorEmail: strings.ToLower(c.Author.Email), Timestamp: c.Timestamp})
		}
		return []any{ev}, nil
	case "create", "delete":
		var p struct {
			Ref     string `json:"ref"`
			RefType string `json:"ref_type"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		switch {
		case p.RefType == "tag" && eventType == "create":
			return []any{TagEvent{Repo: repo, Name: p.Ref}}, nil
		case p.RefType != "branch":
			return nil, nil
		}
		return []any{BranchEvent{Repo: repo, Branch: p.Ref, Deleted: eventType == "delete"}}, nil
	case "pull_request":
		var p struct {
			PullRequest ghPR `json:"pull_request"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		action := map[string]string{
			"opened": "opened", "reopened": "reopened", "ready_for_review": "ready", "converted_to_draft": "draft",
			"edited": "edited", "synchronize": "updated", "closed": "closed",
		}[base.Action]
		if action == "" {
			return nil, nil
		}
		ev := p.PullRequest.event(repo, action)
		if action == "closed" && ev.State == "merged" {
			ev.Action = "merged"
		}
		return []any{ev}, nil
	case "pull_request_review":
		var p struct {
			Review struct {
				State string `json:"state"`
				User  ghUser `json:"user"`
			} `json:"review"`
			PullRequest ghPR `json:"pull_request"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if base.Action != "submitted" {
			return nil, nil
		}
		return []any{ReviewEvent{Repo: repo, Number: strconv.Itoa(p.PullRequest.Number), State: strings.ToLower(p.Review.State), Reviewer: p.Review.User.Login}}, nil
	case "workflow_run":
		var p struct {
			WorkflowRun struct {
				ID         int64     `json:"id"`
				Name       string    `json:"name"`
				Status     string    `json:"status"`
				Conclusion string    `json:"conclusion"`
				HTMLURL    string    `json:"html_url"`
				HeadBranch string    `json:"head_branch"`
				HeadSHA    string    `json:"head_sha"`
				UpdatedAt  time.Time `json:"updated_at"`
				HeadCommit struct {
					Message string `json:"message"`
				} `json:"head_commit"`
			} `json:"workflow_run"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		wr := p.WorkflowRun
		status := "running"
		switch {
		case wr.Status == "queued" || wr.Status == "requested" || wr.Status == "waiting" || wr.Status == "pending":
			status = "pending"
		case wr.Status == "completed" && wr.Conclusion == "success":
			status = "success"
		case wr.Status == "completed" && (wr.Conclusion == "cancelled" || wr.Conclusion == "skipped"):
			status = "canceled"
		case wr.Status == "completed":
			status = "failed"
		}
		return []any{PipelineEvent{Repo: repo, ID: strconv.FormatInt(wr.ID, 10), Name: wr.Name, Status: status, URL: wr.HTMLURL,
			Branch: wr.HeadBranch, SHA: wr.HeadSHA, CommitMessage: wr.HeadCommit.Message, UpdatedAt: wr.UpdatedAt}}, nil
	case "deployment_status":
		var p struct {
			DeploymentStatus struct {
				State          string    `json:"state"`
				Environment    string    `json:"environment"`
				EnvironmentURL string    `json:"environment_url"`
				TargetURL      string    `json:"target_url"`
				LogURL         string    `json:"log_url"`
				CreatedAt      time.Time `json:"created_at"`
			} `json:"deployment_status"`
			Deployment struct {
				ID          int64  `json:"id"`
				SHA         string `json:"sha"`
				Ref         string `json:"ref"`
				Environment string `json:"environment"`
				Creator     ghUser `json:"creator"`
			} `json:"deployment"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		st, dep := p.DeploymentStatus, p.Deployment
		// "inactive" means a newer deployment superseded this one: it is not an outcome.
		status := map[string]string{
			"queued": "pending", "pending": "pending", "waiting": "pending", "in_progress": "running",
			"success": "success", "failure": "failed", "error": "failed",
		}[st.State]
		if status == "" {
			return nil, nil
		}
		env := cmp.Or(st.Environment, dep.Environment)
		link := cmp.Or(st.LogURL, st.TargetURL, st.EnvironmentURL)
		if link == "" && repo.WebURL != "" {
			link = repo.WebURL + "/deployments"
		}
		return []any{DeploymentEvent{Repo: repo, ID: strconv.FormatInt(dep.ID, 10), Environment: env, Ref: dep.Ref, SHA: dep.SHA,
			Status: status, URL: link, Deployer: dep.Creator.Login, At: st.CreatedAt}}, nil
	case "installation", "installation_repositories":
		var p struct {
			Repositories        []ghRepo `json:"repositories"`
			RepositoriesAdded   []ghRepo `json:"repositories_added"`
			RepositoriesRemoved []ghRepo `json:"repositories_removed"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		ev := RepositoriesEvent{}
		added := append(p.RepositoriesAdded, p.Repositories...)
		if base.Action == "deleted" {
			for _, r := range p.Repositories {
				ev.Removed = append(ev.Removed, r.ref(base.Installation.ID))
			}
			return []any{ev}, nil
		}
		for _, r := range added {
			ev.Added = append(ev.Added, r.ref(base.Installation.ID))
		}
		for _, r := range p.RepositoriesRemoved {
			ev.Removed = append(ev.Removed, r.ref(base.Installation.ID))
		}
		return []any{ev}, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// API client (GitHub App authentication)

type githubClient struct {
	conn  Connection
	http  *http.Client
	cache *tokenCache
}

// tokenCache keeps a connection's installation tokens until shortly before they expire. Clients
// for the same connection share it, whichever HTTP client they use.
type tokenCache struct {
	privateKey string
	mu         sync.Mutex
	tokens     map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

var ghTokens sync.Map // connection id -> *tokenCache

func (GitHub) Client(_ context.Context, conn Connection, httpClient *http.Client) (Client, error) {
	if conn.AppID == 0 || conn.PrivateKey == "" {
		return nil, errors.New("the GitHub connection needs an App id and private key")
	}
	tc, ok := ghTokens.Load(conn.ID)
	if !ok || tc.(*tokenCache).privateKey != conn.PrivateKey {
		tc = &tokenCache{privateKey: conn.PrivateKey, tokens: map[int64]cachedToken{}}
		ghTokens.Store(conn.ID, tc)
	}
	return &githubClient{conn: conn, http: httpClient, cache: tc.(*tokenCache)}, nil
}

func (c *githubClient) appJWT() (string, error) {
	block, _ := pem.Decode([]byte(c.conn.PrivateKey))
	if block == nil {
		return "", errors.New("GitHub App private key is not PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k8, err8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err8 != nil {
			return "", fmt.Errorf("parse GitHub App private key: %w", err)
		}
		return jwt.NewWithClaims(jwt.SigningMethodRS256, c.claims()).SignedString(k8)
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, c.claims()).SignedString(key)
}

func (c *githubClient) claims() jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    strconv.FormatInt(c.conn.AppID, 10),
	}
}

func (c *githubClient) installationToken(ctx context.Context, installation int64) (string, error) {
	c.cache.mu.Lock()
	if t, ok := c.cache.tokens[installation]; ok && time.Until(t.expires) > 5*time.Minute {
		c.cache.mu.Unlock()
		return t.token, nil
	}
	c.cache.mu.Unlock()
	appToken, err := c.appJWT()
	if err != nil {
		return "", err
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := c.do(ctx, appToken, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installation), nil, &out); err != nil {
		return "", err
	}
	c.cache.mu.Lock()
	c.cache.tokens[installation] = cachedToken{out.Token, out.ExpiresAt}
	c.cache.mu.Unlock()
	return out.Token, nil
}

func (c *githubClient) do(ctx context.Context, token, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = strings.TrimRight(c.conn.APIURL, "/") + path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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
		return apiError("github", method, path, res.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *githubClient) repoCall(ctx context.Context, repo RepoRef, method, path string, body, out any) error {
	if repo.InstallationID == 0 {
		return errors.New("repository has no GitHub App installation")
	}
	tok, err := c.installationToken(ctx, repo.InstallationID)
	if err != nil {
		return err
	}
	return c.do(ctx, tok, method, path, body, out)
}

func (c *githubClient) ListRepositories(ctx context.Context) ([]RepoRef, error) {
	appToken, err := c.appJWT()
	if err != nil {
		return nil, err
	}
	var installs []struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, appToken, http.MethodGet, "/app/installations?per_page=100", nil, &installs); err != nil {
		return nil, err
	}
	var out []RepoRef
	for _, inst := range installs {
		tok, err := c.installationToken(ctx, inst.ID)
		if err != nil {
			return nil, err
		}
		for page := 1; page <= 50; page++ {
			var res struct {
				Repositories []ghRepo `json:"repositories"`
			}
			if err := c.do(ctx, tok, http.MethodGet, fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), nil, &res); err != nil {
				return nil, err
			}
			for _, r := range res.Repositories {
				out = append(out, r.ref(inst.ID))
			}
			if len(res.Repositories) < 100 {
				break
			}
		}
	}
	return out, nil
}

func repoPath(repo RepoRef) string {
	owner, name, _ := strings.Cut(repo.FullPath, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func (c *githubClient) ListPullRequests(ctx context.Context, repo RepoRef) ([]PullRequest, error) {
	var prs []ghPR
	if err := c.repoCall(ctx, repo, http.MethodGet, repoPath(repo)+"/pulls?state=all&sort=updated&direction=desc&per_page=50", nil, &prs); err != nil {
		return nil, err
	}
	out := make([]PullRequest, len(prs))
	for i, p := range prs {
		out[i] = p.event(repo, "reconciled")
	}
	return out, nil
}

func (c *githubClient) ListBranches(ctx context.Context, repo RepoRef) ([]string, error) {
	var bs []struct {
		Name string `json:"name"`
	}
	if err := c.repoCall(ctx, repo, http.MethodGet, repoPath(repo)+"/branches?per_page=100", nil, &bs); err != nil {
		return nil, err
	}
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
	}
	return out, nil
}

func (c *githubClient) Comment(ctx context.Context, repo RepoRef, number, body string) error {
	return c.repoCall(ctx, repo, http.MethodPost, repoPath(repo)+"/issues/"+url.PathEscape(number)+"/comments", map[string]string{"body": body}, nil)
}

func (c *githubClient) CreateBranch(ctx context.Context, repo RepoRef, name, from string) error {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.repoCall(ctx, repo, http.MethodGet, repoPath(repo)+"/git/ref/heads/"+url.PathEscape(from), nil, &ref); err != nil {
		return err
	}
	return c.repoCall(ctx, repo, http.MethodPost, repoPath(repo)+"/git/refs", map[string]string{"ref": "refs/heads/" + name, "sha": ref.Object.SHA}, nil)
}

func (c *githubClient) EnsureHook(context.Context, RepoRef, string, string) (string, error) {
	return "", nil // GitHub Apps deliver every installation's events to the app-level webhook
}

type ghCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

func (g ghCommit) commit() Commit {
	return Commit{SHA: g.SHA, Message: g.Commit.Message, URL: g.HTMLURL, AuthorName: g.Commit.Author.Name,
		AuthorEmail: strings.ToLower(g.Commit.Author.Email), Timestamp: g.Commit.Author.Date}
}

// CommitsBetween uses the compare API, which answers with up to 250 commits, oldest first.
func (c *githubClient) CommitsBetween(ctx context.Context, repo RepoRef, base, head string, limit int) ([]Commit, bool, error) {
	if base == "" {
		var one ghCommit
		if err := c.repoCall(ctx, repo, http.MethodGet, repoPath(repo)+"/commits/"+url.PathEscape(head), nil, &one); err != nil {
			return nil, false, err
		}
		return []Commit{one.commit()}, false, nil
	}
	var res struct {
		TotalCommits int        `json:"total_commits"`
		Commits      []ghCommit `json:"commits"`
	}
	if err := c.repoCall(ctx, repo, http.MethodGet, repoPath(repo)+"/compare/"+url.PathEscape(base)+"..."+url.PathEscape(head), nil, &res); err != nil {
		return nil, false, err
	}
	out := make([]Commit, len(res.Commits))
	for i, g := range res.Commits {
		out[i] = g.commit()
	}
	out, truncated := newest(out, limit)
	return out, truncated || res.TotalCommits > len(res.Commits), nil
}
