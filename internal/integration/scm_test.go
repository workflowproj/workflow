package integration

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeSCM records API calls and answers from canned routes ("METHOD /path" → JSON body).
type fakeSCM struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string]any
	calls  []fakeCall
}

type fakeCall struct {
	Method, Path, Auth string
	Body               map[string]any
}

func newFakeSCM(t *testing.T, routes map[string]any) *fakeSCM {
	f := &fakeSCM{routes: routes}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.Header.Get("PRIVATE-TOKEN")
		}
		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{Method: r.Method, Path: r.URL.Path, Auth: auth, Body: body})
		resp, ok := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if !ok {
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id": 1}`))
				return
			}
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSCM) find(method, path string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func rsaPEM(t *testing.T) string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// deliver posts a webhook to the hooks endpoint.
func (e *Env) deliver(path string, payload any, headers ...string) Response {
	e.t.Helper()
	raw, _ := json.Marshal(payload)
	c := e.anon()
	c.csrf = false
	return c.do("POST", path, raw, append([]string{"Content-Type", "application/json"}, headers...)...)
}

func githubHeaders(secret, event, delivery string, payload any) []string {
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	return []string{"X-GitHub-Event", event, "X-GitHub-Delivery", delivery, "X-Hub-Signature-256", "sha256=" + hex.EncodeToString(mac.Sum(nil))}
}

func devLinks(c *Client, key string) map[string][]obj {
	out := map[string][]obj{}
	for _, d := range c.issue(key)["development"].([]any) {
		m := d.(obj)
		out[str(m, "kind")] = append(out[str(m, "kind")], m)
	}
	return out
}

func TestGitHubIntegration(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	member := e.login("mel@example.com", "Mel")
	admin.createProject("SHOP", "scrum")
	admin.createProject("OTHER", "kanban")

	repoJSON := obj{"id": 101, "full_name": "acme/shop", "html_url": "https://github.com/acme/shop", "default_branch": "main"}
	gh := newFakeSCM(t, map[string]any{
		"POST /app/installations/77/access_tokens": obj{"token": "ghs_installation", "expires_at": time.Now().Add(time.Hour)},
		"GET /app/installations":                   []obj{{"id": 77}},
		"GET /installation/repositories":           obj{"repositories": []obj{repoJSON}},
		"GET /repos/acme/shop/pulls":               []obj{},
		"GET /repos/acme/shop/branches":            []obj{{"name": "main"}, {"name": "feature/SHOP-3-reconciled"}},
		"GET /repos/acme/shop/git/ref/heads/main":  obj{"object": obj{"sha": "basesha"}},
	})

	member.must("POST", "/api/v1/admin/scm-connections", obj{"provider": "github", "name": "x"}, 403, nil)
	var conn obj
	admin.must("POST", "/api/v1/admin/scm-connections", obj{
		"provider": "github", "name": "GitHub", "apiUrl": gh.URL, "webUrl": "https://github.com",
		"appId": 4242, "appSlug": "wfm-test", "privateKey": rsaPEM(t), "webhookSecret": "s3cret",
	}, 201, &conn)
	eq(t, str(conn, "webhookSecret"), "s3cret", "secret returned once on create")
	hook := strings.TrimPrefix(str(conn, "webhookUrl"), e.Cfg.HooksPublicURL)
	eq(t, hook, "/hooks/github/"+str(conn, "id"), "webhook path")

	// Signatures are verified before anything is stored.
	install := obj{"action": "created", "installation": obj{"id": 77}, "repositories": []obj{{"id": 101, "full_name": "acme/shop"}}}
	eq(t, e.deliver(hook, install, githubHeaders("wrong", "installation", "d-0", install)...).Status, 401, "bad signature")
	eq(t, e.deliver("/hooks/github/"+uuid.NewString(), install, githubHeaders("s3cret", "installation", "d-0", install)...).Status, 404, "unknown connection")
	eq(t, e.deliver(hook, obj{"zen": "hi"}, githubHeaders("s3cret", "ping", "d-ping", obj{"zen": "hi"})...).Status, 200, "ping")
	eq(t, e.deliver(hook, install, githubHeaders("s3cret", "installation", "d-1", install)...).Status, 202, "installation")
	e.DrainJobs()
	var repos []obj
	admin.get("/api/v1/scm/repositories", &repos)
	eq(t, len(repos), 1, "repository discovered from installation")
	repoID := str(repos[0], "id")

	admin.createIssue("SHOP", "story", "Checkout button", nil)
	other := admin.createIssue("OTHER", "task", "Unrelated", nil)

	// Before the repository is connected to SHOP, keys in it are not linked.
	push := func(sha, msg, email string) obj {
		return obj{
			"ref": "refs/heads/SHOP-1-checkout", "installation": obj{"id": 77}, "repository": repoJSON,
			"commits": []obj{{"id": sha, "message": msg, "url": "https://github.com/acme/shop/commit/" + sha, "distinct": true,
				"timestamp": time.Now().Format(time.RFC3339), "author": obj{"name": "Ada", "email": email}}},
		}
	}
	early := push("aaa111", "SHOP-1 early commit", "ada@example.com")
	e.deliver(hook, early, githubHeaders("s3cret", "push", "d-2", early)...)
	e.DrainJobs()
	var events []obj
	admin.get("/api/v1/admin/scm-connections/"+str(conn, "id")+"/events", &events)
	eq(t, str(events[0], "status"), "ignored", "unconnected repository ignored")
	eq(t, len(devLinks(admin, "SHOP-1")), 0, "no links yet")

	admin.must("POST", "/api/v1/projects/SHOP/repositories", obj{"repositoryId": repoID}, 201, nil)

	// A push with smart commits: transitions go through the workflow engine as the mapped user.
	// OTHER-1 is mentioned too, but OTHER is not connected to this repository.
	p := push("bbb222", "SHOP-1 #start-work #comment Picked this up\n\nAlso touches OTHER-1", "Ada@Example.com")
	h := githubHeaders("s3cret", "push", "d-3", p)
	eq(t, e.deliver(hook, p, h...).Status, 202, "push accepted")
	eq(t, e.deliver(hook, p, h...).Status, 202, "redelivery accepted")
	eq(t, e.JobsOfKind("scm_webhook"), 3, "duplicate delivery not queued again") // d-1, d-2, d-3
	e.DrainJobs()

	got := admin.issue("SHOP-1")
	eq(t, str(got, "status", "name"), "In Progress", "smart commit transition")
	eq(t, str(got, "assignee", "email"), "ada@example.com", "transition post-function ran as the committer")
	var comments []obj
	admin.get("/api/v1/issues/SHOP-1/comments", &comments)
	eq(t, len(comments), 1, "smart commit comment")
	if !strings.Contains(str(comments[0], "body"), "Picked this up") || str(comments[0], "author", "email") != "ada@example.com" {
		t.Fatalf("comment = %v", comments[0])
	}
	// Comments render Markdown without HTML, so the commit is named in Markdown.
	eq(t, str(comments[0], "body"), "Picked this up\n\nVia commit [bbb222](https://github.com/acme/shop/commit/bbb222)", "smart commit comment body")
	links := devLinks(admin, "SHOP-1")
	eq(t, len(links["branch"]), 1, "branch link")
	eq(t, len(links["commit"]), 1, "commit link")
	eq(t, len(devLinks(admin, str(other, "key"))), 0, "keys of unconnected projects are not linked")

	// The same commit pushed to another branch (new delivery) does not repeat the smart commit.
	p2 := push("bbb222", "SHOP-1 #start-work #comment Picked this up", "ada@example.com")
	p2["ref"] = "refs/heads/main"
	e.deliver(hook, p2, githubHeaders("s3cret", "push", "d-4", p2)...)
	e.DrainJobs()
	admin.get("/api/v1/issues/SHOP-1/comments", &comments)
	eq(t, len(comments), 1, "smart commit ran once")

	// Unknown committers are linked but cannot change issues.
	p3 := push("ccc333", "SHOP-1 #done", "stranger@elsewhere.dev")
	e.deliver(hook, p3, githubHeaders("s3cret", "push", "d-5", p3)...)
	e.DrainJobs()
	eq(t, str(admin.issue("SHOP-1"), "status", "name"), "In Progress", "stranger cannot transition")
	eq(t, len(devLinks(admin, "SHOP-1")["commit"]), 2, "stranger commit still linked")

	// An automation rule moves issues to review when a PR opens; write-back comments on the PR.
	admin.must("POST", "/api/v1/projects/SHOP/automation-rules", obj{
		"name": "PR opened → In Review", "trigger": obj{"type": "pr.opened"},
		"actions": []obj{{"type": "transition", "status": "In Review"}},
	}, 201, nil)
	admin.must("POST", "/api/v1/projects/SHOP/automation-rules", obj{
		"name": "PR merged → Done", "trigger": obj{"type": "pr.merged"},
		"actions": []obj{{"type": "transition", "status": "Done"}, {"type": "comment", "body": "Merged in {{event.number}}"}},
	}, 201, nil)
	pr := func(action string, merged bool) obj {
		state := "open"
		if merged {
			state = "closed"
		}
		return obj{"action": action, "installation": obj{"id": 77}, "repository": repoJSON, "pull_request": obj{
			"number": 7, "title": "SHOP-1: checkout button", "body": "", "html_url": "https://github.com/acme/shop/pull/7",
			"state": state, "merged": merged, "user": obj{"login": "ada"}, "updated_at": time.Now().Format(time.RFC3339),
			"head": obj{"ref": "SHOP-1-checkout", "sha": "bbb222"}, "base": obj{"ref": "main"},
		}}
	}
	opened := pr("opened", false)
	e.deliver(hook, opened, githubHeaders("s3cret", "pull_request", "d-6", opened)...)
	e.DrainJobs()
	eq(t, str(admin.issue("SHOP-1"), "status", "name"), "In Review", "automation on pr.opened")
	wb := gh.find("POST", "/repos/acme/shop/issues/7/comments")
	eq(t, len(wb), 1, "write-back comment")
	if !strings.Contains(wb[0].Body["body"].(string), "SHOP-1") || wb[0].Auth != "Bearer ghs_installation" {
		t.Fatalf("write-back = %+v", wb[0])
	}
	eq(t, str(devLinks(admin, "SHOP-1")["pull_request"][0], "state"), "open", "PR link state")

	review := obj{"action": "submitted", "installation": obj{"id": 77}, "repository": repoJSON,
		"review": obj{"state": "APPROVED", "user": obj{"login": "bob"}}, "pull_request": obj{"number": 7}}
	e.deliver(hook, review, githubHeaders("s3cret", "pull_request_review", "d-7", review)...)
	run := obj{"action": "completed", "installation": obj{"id": 77}, "repository": repoJSON, "workflow_run": obj{
		"id": 555, "name": "CI", "status": "completed", "conclusion": "success", "html_url": "https://github.com/acme/shop/actions/runs/555",
		"head_branch": "SHOP-1-checkout", "head_sha": "bbb222", "updated_at": time.Now().Format(time.RFC3339)}}
	e.deliver(hook, run, githubHeaders("s3cret", "workflow_run", "d-8", run)...)
	merged := pr("closed", true)
	e.deliver(hook, merged, githubHeaders("s3cret", "pull_request", "d-9", merged)...)
	e.DrainJobs()

	got = admin.issue("SHOP-1")
	eq(t, str(got, "status", "name"), "Done", "automation on pr.merged")
	eq(t, str(got, "resolution"), "Done", "resolution set by done category")
	links = devLinks(admin, "SHOP-1")
	eq(t, str(links["pull_request"][0], "state"), "merged", "PR merged")
	eq(t, str(links["pull_request"][0]["data"].(obj), "reviewState"), "approved", "review state")
	eq(t, str(links["pipeline"][0], "state"), "success", "pipeline state")
	eq(t, len(gh.find("POST", "/repos/acme/shop/issues/7/comments")), 1, "write-back only once")
	admin.get("/api/v1/issues/SHOP-1/comments", &comments)
	eq(t, str(comments[len(comments)-1], "body"), "Merged in 7", "automation comment with template")

	// Reconcile picks up branches whose webhooks were missed (without firing automation).
	admin.createIssue("SHOP", "story", "Second", nil)
	admin.createIssue("SHOP", "story", "Third", nil)
	admin.must("POST", "/api/v1/admin/scm-connections/"+str(conn, "id")+"/sync", nil, 202, nil)
	e.DrainJobs()
	eq(t, len(devLinks(admin, "SHOP-3")["branch"]), 1, "reconciled branch")

	// Create a branch from the issue.
	var br obj
	admin.must("POST", "/api/v1/issues/SHOP-2/branches", obj{"repositoryId": repoID}, 201, &br)
	refs := gh.find("POST", "/repos/acme/shop/git/refs")
	eq(t, len(refs), 1, "branch created via API")
	eq(t, refs[0].Body["sha"], any("basesha"), "branch from default branch head")
	if !strings.HasPrefix(refs[0].Body["ref"].(string), "refs/heads/SHOP-2-") {
		t.Fatalf("branch ref = %v", refs[0].Body["ref"])
	}
	eq(t, str(br, "kind"), "branch", "branch link returned")
	member.must("POST", "/api/v1/issues/SHOP-2/branches", obj{"repositoryId": repoID}, 201, nil) // members can create branches

	// Reprocess a stored event.
	admin.get("/api/v1/admin/scm-connections/"+str(conn, "id")+"/events", &events)
	admin.must("POST", "/api/v1/admin/webhook-events/"+str(events[0], "id")+"/reprocess", nil, 202, nil)
}

func TestGitLabIntegration(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	admin.createProject("APP", "kanban")
	admin.createIssue("APP", "task", "Login page", nil)

	project := obj{"id": 42, "path_with_namespace": "grp/app", "web_url": "https://gitlab.example.com/grp/app", "default_branch": "main"}
	gl := newFakeSCM(t, map[string]any{
		"GET /api/v4/projects":          []obj{project},
		"GET /api/v4/projects/42/hooks": []obj{},
	})
	var conn obj
	admin.must("POST", "/api/v1/admin/scm-connections", obj{
		"provider": "gitlab", "name": "GitLab", "apiUrl": gl.URL, "webUrl": "https://gitlab.example.com",
		"token": "glpat-test", "webhookSecret": "gl-secret", "manageHooks": true,
	}, 201, &conn)
	var generated obj
	admin.must("POST", "/api/v1/admin/scm-connections", obj{"provider": "gitlab", "name": "Generated secret", "apiUrl": gl.URL, "token": "x"}, 201, &generated)
	if len(str(generated, "webhookSecret")) < 20 {
		t.Fatalf("generated webhook secret not returned: %v", generated)
	}
	var conns []obj
	admin.get("/api/v1/admin/scm-connections", &conns)
	for _, c := range conns {
		if _, ok := c["webhookSecret"]; ok {
			t.Fatal("webhook secret listed after creation")
		}
	}
	admin.must("DELETE", "/api/v1/admin/scm-connections/"+str(generated, "id"), nil, 204, nil)
	admin.must("POST", "/api/v1/admin/scm-connections/"+str(conn, "id")+"/sync", nil, 202, nil)
	e.DrainJobs()
	var repos []obj
	admin.get("/api/v1/scm/repositories?q=app", &repos)
	eq(t, len(repos), 1, "repository discovered by reconcile")

	// Linking with manageHooks creates the project hook through the API.
	admin.must("POST", "/api/v1/projects/APP/repositories", obj{"repositoryId": repos[0]["id"]}, 201, nil)
	e.DrainJobs()
	hooks := gl.find("POST", "/api/v4/projects/42/hooks")
	eq(t, len(hooks), 1, "hook created")
	eq(t, hooks[0].Body["token"], any("gl-secret"), "hook secret")
	eq(t, hooks[0].Auth, "glpat-test", "token auth")
	if !strings.HasSuffix(hooks[0].Body["url"].(string), "/hooks/gitlab/"+str(conn, "id")) {
		t.Fatalf("hook url = %v", hooks[0].Body["url"])
	}

	hook := "/hooks/gitlab/" + str(conn, "id")
	glh := func(event, id string) []string {
		return []string{"X-Gitlab-Token", "gl-secret", "X-Gitlab-Event", event, "X-Gitlab-Event-UUID", id}
	}
	push := obj{"object_kind": "push", "ref": "refs/heads/APP-1-login", "before": strings.Repeat("0", 40), "after": "abc", "project": project,
		"commits": []obj{{"id": "abc", "message": "APP-1 #comment Form done", "url": "https://gitlab.example.com/grp/app/-/commit/abc",
			"timestamp": "2026-01-02T10:00:00Z", "author": obj{"name": "Bob", "email": "bob@example.com"}}}}
	eq(t, e.deliver(hook, push, "X-Gitlab-Token", "nope", "X-Gitlab-Event", "Push Hook").Status, 401, "bad token")
	eq(t, e.deliver(hook, push, glh("Push Hook", "u-1")...).Status, 202, "push")
	mr := obj{"object_kind": "merge_request", "project": project, "user": obj{"username": "bob"}, "object_attributes": obj{
		"iid": 5, "title": "Login page", "description": "Closes APP-1", "url": "https://gitlab.example.com/grp/app/-/merge_requests/5",
		"state": "opened", "action": "open", "source_branch": "APP-1-login", "target_branch": "main", "updated_at": "2026-01-02 10:05:00 UTC",
		"last_commit": obj{"id": "abc"}, "author": obj{"username": "bob"}}}
	e.deliver(hook, mr, glh("Merge Request Hook", "u-2")...)
	pipe := obj{"object_kind": "pipeline", "project": project, "object_attributes": obj{"id": 900, "ref": "APP-1-login", "sha": "abc",
		"status": "failed", "created_at": "2026-01-02 10:06:00 UTC", "finished_at": "2026-01-02 10:09:00 UTC"}, "commit": obj{"message": "APP-1"}}
	e.deliver(hook, pipe, glh("Pipeline Hook", "u-3")...)
	e.DrainJobs()

	var comments []obj
	admin.get("/api/v1/issues/APP-1/comments", &comments)
	eq(t, len(comments), 1, "smart commit comment")
	eq(t, str(comments[0], "author", "id"), bob.id(), "comment by mapped user")
	eq(t, str(comments[0], "body"), "Form done\n\nVia commit [abc](https://gitlab.example.com/grp/app/-/commit/abc)", "smart commit comment body")
	links := devLinks(admin, "APP-1")
	eq(t, len(links["branch"]), 1, "branch")
	eq(t, len(links["commit"]), 1, "commit")
	eq(t, str(links["pull_request"][0], "url"), "https://gitlab.example.com/grp/app/-/merge_requests/5", "MR link")
	eq(t, str(links["pipeline"][0], "state"), "failed", "pipeline")
	eq(t, len(gl.find("POST", "/api/v4/projects/42/merge_requests/5/notes")), 1, "MR note write-back")

	// Content-hash dedupe when GitLab sends no event UUID.
	eq(t, e.deliver(hook, pipe, "X-Gitlab-Token", "gl-secret", "X-Gitlab-Event", "Pipeline Hook").Status, 202, "no uuid")
	eq(t, e.deliver(hook, pipe, "X-Gitlab-Token", "gl-secret", "X-Gitlab-Event", "Pipeline Hook").Status, 202, "no uuid again")
	eq(t, e.JobsOfKind("scm_webhook"), 4, "content dedupe")

	// The issue view summarises development state for board cards.
	var board []obj
	admin.get("/api/v1/projects/APP/boards", &board)
	var data obj
	admin.get("/api/v1/boards/"+str(board[0], "id")+"/data", &data)
	card := data["columns"].([]any)[0].(obj)["issues"].([]any)[0].(obj)
	if card["dev"] == nil {
		t.Fatalf("board card lacks dev summary: %v", card)
	}
}

// A pull/merge request whose title and description never mention the issue is still linked when
// its source branch carries the key, in any case and with any separator.
func TestPullRequestsLinkByBranchName(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("SHOP", "kanban")
	for i := 1; i <= 3; i++ {
		admin.createIssue("SHOP", "task", fmt.Sprintf("Task %d", i), nil)
	}
	repoJSON := obj{"id": 101, "full_name": "acme/shop", "html_url": "https://github.com/acme/shop", "default_branch": "main"}
	gh := newFakeSCM(t, map[string]any{
		"POST /app/installations/77/access_tokens": obj{"token": "ghs_installation", "expires_at": time.Now().Add(time.Hour)},
	})
	var conn obj
	admin.must("POST", "/api/v1/admin/scm-connections", obj{
		"provider": "github", "name": "GitHub", "apiUrl": gh.URL, "appId": 4242, "privateKey": rsaPEM(t), "webhookSecret": "s3cret",
	}, 201, &conn)
	hook := "/hooks/github/" + str(conn, "id")
	install := obj{"action": "created", "installation": obj{"id": 77}, "repositories": []obj{{"id": 101, "full_name": "acme/shop"}}}
	e.deliver(hook, install, githubHeaders("s3cret", "installation", "i-1", install)...)
	e.DrainJobs()
	var repos []obj
	admin.get("/api/v1/scm/repositories", &repos)
	admin.must("POST", "/api/v1/projects/SHOP/repositories", obj{"repositoryId": repos[0]["id"]}, 201, nil)

	branches := map[string]string{"SHOP-1": "feature/shop-1-add-cart", "SHOP-2": "SHOP-2_fix_totals", "SHOP-3": "users/ada/Shop-3/try"}
	n := 0
	for key, branch := range branches {
		n++
		pr := obj{"action": "opened", "installation": obj{"id": 77}, "repository": repoJSON, "pull_request": obj{
			"number": n, "title": "Improve the checkout", "body": "No issue key here", "html_url": fmt.Sprintf("https://github.com/acme/shop/pull/%d", n),
			"state": "open", "user": obj{"login": "ada"}, "updated_at": time.Now().Format(time.RFC3339),
			"head": obj{"ref": branch, "sha": "abc"}, "base": obj{"ref": "main"},
		}}
		e.deliver(hook, pr, githubHeaders("s3cret", "pull_request", fmt.Sprintf("pr-%d", n), pr)...)
		e.DrainJobs()
		prs := devLinks(admin, key)["pull_request"]
		if len(prs) != 1 || str(prs[0], "data", "sourceBranch") != branch {
			t.Fatalf("%s: PR from branch %q not linked: %v", key, branch, prs)
		}
	}
	// Each PR links only to its own issue.
	for key := range branches {
		eq(t, len(devLinks(admin, key)["pull_request"]), 1, key+" PR count")
	}
}
