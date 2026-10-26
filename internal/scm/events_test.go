package scm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parseOne(t *testing.T, p Provider, event, payload string) any {
	t.Helper()
	evs, err := p.Parse(event, []byte(payload))
	if err != nil {
		t.Fatalf("%s: %v", event, err)
	}
	switch len(evs) {
	case 0:
		return nil
	case 1:
		return evs[0]
	}
	t.Fatalf("%s: %d events", event, len(evs))
	return nil
}

func TestGitHubTagEvents(t *testing.T) {
	gh := GitHub{}
	repo := RepoRef{ExternalID: "101", FullPath: "acme/shop", WebURL: "https://github.com/acme/shop", DefaultBranch: "main", InstallationID: 77}

	create := fixture(t, "github_create_tag.json")
	if got, want := parseOne(t, gh, "create", create), (TagEvent{Repo: repo, Name: "v3.9.0"}); !reflect.DeepEqual(got, want) {
		t.Errorf("create tag = %+v, want %+v", got, want)
	}
	if got := parseOne(t, gh, "delete", create); got != nil {
		t.Errorf("deleted tags are ignored, got %+v", got)
	}

	push := fixture(t, "github_push_tag.json")
	// The head commit, not "after" (an annotated tag's own object).
	if got, want := parseOne(t, gh, "push", push), (TagEvent{Repo: repo, Name: "v3.9.0", SHA: "e3f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1"}); !reflect.DeepEqual(got, want) {
		t.Errorf("push tag = %+v, want %+v", got, want)
	}
	deleted := strings.NewReplacer(`"created": true`, `"created": false`, `"deleted": false`, `"deleted": true`).Replace(push)
	if got := parseOne(t, gh, "push", deleted); got != nil {
		t.Errorf("a deleted tag's push is ignored, got %+v", got)
	}
}

func TestGitHubDeploymentStatus(t *testing.T) {
	gh := GitHub{}
	payload := fixture(t, "github_deployment_status.json")
	got, ok := parseOne(t, gh, "deployment_status", payload).(DeploymentEvent)
	if !ok {
		t.Fatalf("not a deployment event")
	}
	want := DeploymentEvent{
		Repo: RepoRef{ExternalID: "101", FullPath: "acme/shop", WebURL: "https://github.com/acme/shop", DefaultBranch: "main", InstallationID: 77},
		ID:   "145988746", Environment: "production", Ref: "v3.9.0", SHA: "e3f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1", Status: "success",
		URL: "https://github.com/acme/shop/actions/runs/6544321/job/17772123", Deployer: "ada", At: time.Date(2026, 10, 21, 10, 15, 42, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deployment = %+v\nwant %+v", got, want)
	}
	for state, status := range map[string]string{"queued": "pending", "pending": "pending", "in_progress": "running", "failure": "failed", "error": "failed", "inactive": ""} {
		ev := parseOne(t, gh, "deployment_status", strings.Replace(payload, `"state": "success"`, `"state": "`+state+`"`, 1))
		if status == "" {
			if ev != nil {
				t.Errorf("%s: superseded deployments are not an outcome, got %+v", state, ev)
			}
			continue
		}
		if d, _ := ev.(DeploymentEvent); d.Status != status {
			t.Errorf("%s: status %q, want %q", state, d.Status, status)
		}
	}
	// Without a log or target URL, the environment's URL.
	bare := strings.NewReplacer(`"target_url": "https://github.com/acme/shop/actions/runs/6544321",`, "",
		`"log_url": "https://github.com/acme/shop/actions/runs/6544321/job/17772123",`, "").Replace(payload)
	if d, _ := parseOne(t, gh, "deployment_status", bare).(DeploymentEvent); d.URL != "https://shop.example.com" {
		t.Errorf("url = %q", d.URL)
	}
}

func TestGitLabTagAndDeploymentEvents(t *testing.T) {
	gl := GitLab{}
	repo := RepoRef{ExternalID: "42", FullPath: "grp/app", WebURL: "https://gitlab.example.com/grp/app", DefaultBranch: "main"}

	tag := fixture(t, "gitlab_tag_push.json")
	if got, want := parseOne(t, gl, "Tag Push Hook", tag), (TagEvent{Repo: repo, Name: "v2.0.0", SHA: "da1560886d4f094c3e6c9ef40349f7d38b5d27d7"}); !reflect.DeepEqual(got, want) {
		t.Errorf("tag push = %+v, want %+v", got, want)
	}
	deleted := strings.Replace(tag, `"after": "82b3d5ae55f7080f1e6022629cdb57bfae7cccc7"`, `"after": "0000000000000000000000000000000000000000"`, 1)
	if got := parseOne(t, gl, "Tag Push Hook", deleted); got != nil {
		t.Errorf("deleted tags are ignored, got %+v", got)
	}

	dep := fixture(t, "gitlab_deployment.json")
	want := DeploymentEvent{
		Repo: repo, ID: "15", Environment: "staging", Ref: "main", SHA: "279484c09fbe69ededfced8c1bb6e6d24616b468", Status: "success",
		URL: "https://gitlab.example.com/grp/app/-/jobs/796", Deployer: "bob", At: time.Date(2026, 10, 21, 19, 50, 0, 0, time.UTC),
	}
	got, _ := parseOne(t, gl, "Deployment Hook", dep).(DeploymentEvent)
	got.At = got.At.UTC()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deployment = %+v\nwant %+v", got, want)
	}
	for state, status := range map[string]string{"created": "pending", "running": "running", "failed": "failed", "canceled": "canceled"} {
		ev := parseOne(t, gl, "Deployment Hook", strings.Replace(dep, `"status": "success"`, `"status": "`+state+`"`, 1))
		if d, _ := ev.(DeploymentEvent); d.Status != status {
			t.Errorf("%s: status %q, want %q", state, d.Status, status)
		}
	}
	// Only the short sha when the payload has no commit URL.
	short := strings.Replace(dep, `"commit_url": "https://gitlab.example.com/grp/app/-/commit/279484c09fbe69ededfced8c1bb6e6d24616b468",`, "", 1)
	if d, _ := parseOne(t, gl, "Deployment Hook", short).(DeploymentEvent); d.SHA != "279484c0" {
		t.Errorf("sha = %q", d.SHA)
	}
}

// A comparison longer than GitHub's 250 commits is read from its last pages, so the newest commits
// are the ones kept.
func TestGitHubCommitsBetweenKeepsTheNewest(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	const total = 320
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/7/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "t", "expires_at": time.Now().Add(time.Hour)})
		case "/repos/acme/shop/compare/aaa...bbb":
			from, to := 1, 250 // unpaged: the first 250
			if page, _ := strconv.Atoi(r.URL.Query().Get("page")); page > 0 {
				from, to = (page-1)*100+1, min(page*100, total)
			}
			var cs []map[string]any
			for n := from; n <= to; n++ {
				cs = append(cs, map[string]any{"sha": fmt.Sprintf("c%03d", n), "commit": map[string]any{"message": fmt.Sprintf("commit %d", n)}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_commits": total, "commits": cs})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cl, err := GitHub{}.Client(t.Context(), Connection{ID: "paging-test", APIURL: srv.URL, AppID: 1, PrivateKey: pemKey}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	commits, truncated, err := cl.CommitsBetween(t.Context(), RepoRef{FullPath: "acme/shop", InstallationID: 7}, "aaa", "bbb", 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 250 || commits[0].SHA != "c071" || commits[249].SHA != "c320" || !truncated {
		t.Fatalf("got %d commits %s..%s, truncated %v", len(commits), commits[0].SHA, commits[len(commits)-1].SHA, truncated)
	}
	// A short range needs one call.
	if commits, truncated, err := cl.CommitsBetween(t.Context(), RepoRef{FullPath: "acme/shop", InstallationID: 7}, "aaa", "bbb", 400); err != nil || len(commits) != total || truncated {
		t.Fatalf("limit 400: %d commits, truncated %v, %v", len(commits), truncated, err)
	}
}

func TestNewestCommits(t *testing.T) {
	var cs []Commit
	for _, sha := range []string{"a", "b", "c", "d"} {
		cs = append(cs, Commit{SHA: sha})
	}
	got, truncated := newest(cs, 2)
	if len(got) != 2 || got[0].SHA != "c" || got[1].SHA != "d" || !truncated {
		t.Errorf("newest(4, 2) = %v %v", got, truncated)
	}
	if got, truncated := newest(cs, 10); len(got) != 4 || truncated {
		t.Errorf("newest(4, 10) = %v %v", got, truncated)
	}
}
