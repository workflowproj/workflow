package scm

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/workflowproj/workflow/internal/apperr"
)

func TestRefSHA(t *testing.T) {
	cases := []struct {
		name, raw, sha string
		err            error
	}{
		{"one ref", `{"ref":"refs/heads/main","object":{"sha":"abc"}}`, "abc", nil},
		{"one ref without its name", `{"object":{"sha":"abc"}}`, "abc", nil},
		{"another ref", `{"ref":"refs/heads/main-old","object":{"sha":"abc"}}`, "", ErrNoBaseBranch},
		{"refs starting with the name", `[{"ref":"refs/heads/main-old","object":{"sha":"old"}},{"ref":"refs/heads/main-2","object":{"sha":"two"}}]`, "", ErrNoBaseBranch},
		{"an exact match among them", `[{"ref":"refs/heads/main-old","object":{"sha":"old"}},{"ref":"refs/heads/main","object":{"sha":"abc"}}]`, "abc", nil},
		{"no refs", `[]`, "", ErrNoBaseBranch},
	}
	for _, c := range cases {
		sha, err := refSHA([]byte(c.raw), "refs/heads/main")
		if sha != c.sha || !errors.Is(err, c.err) {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, sha, err, c.sha, c.err)
		}
	}
	if _, err := refSHA([]byte(`"nonsense"`), "refs/heads/main"); err == nil {
		t.Error("an answer that is no ref: want an error")
	}
	if got := refPath("release/1.0 rc"); got != "release/1.0%20rc" {
		t.Errorf("refPath = %q", got)
	}
}

// TestBranchFailure checks the messages people get when a branch cannot be created: plain words,
// the provider's status and what to check, never the provider's raw answer or a Go error.
func TestBranchFailure(t *testing.T) {
	api := func(status int, path, body string) error {
		return &APIError{Provider: "github", Method: "POST", Path: path, Status: status, Body: body}
	}
	cases := []struct {
		provider string
		err      error
		status   int
		want     []string
	}{
		{"github", fmt.Errorf("%w: %w", ErrNoBaseBranch, api(404, "/repos/acme/shop/git/ref/heads/develop", `{"message":"Not Found"}`)), 400, []string{`acme/shop has no branch "develop"`}},
		{"github", api(403, "/repos/acme/shop/git/refs", `{"message":"Resource not accessible by integration"}`), 502, []string{"403 Forbidden", "Contents: Read & write"}},
		{"gitlab", api(403, "/projects/9/repository/branches", `{"message":"403 Forbidden"}`), 502, []string{"GitLab refused", "api scope", "Developer role"}},
		{"github", api(422, "/repos/acme/shop/git/refs", `{"message":"Reference already exists"}`), 502, []string{"422 Unprocessable Entity", `"SHOP-1-x" may already exist`}},
		{"github", api(404, "/repos/acme/shop/git/refs", `{"message":"Not Found"}`), 502, []string{"could not find acme/shop (404 Not Found)"}},
		{"github", api(401, "/app/installations/7/access_tokens", `{"message":"A JSON web token could not be decoded"}`), 502, []string{"credentials (401 Unauthorized)", "App id and private key"}},
		{"github", api(503, "/repos/acme/shop/git/refs", `upstream`), 502, []string{"answered 503 Service Unavailable"}},
		{"github", errors.New("json: cannot unmarshal array into Go value of type struct { Object struct { SHA string } }"), 502, []string{"did not answer as expected"}},
	}
	for _, c := range cases {
		err := branchFailure(c.provider, "acme/shop", "SHOP-1-x", "develop", c.err)
		ae, ok := apperr.As(err)
		if !ok || ae.Status != c.status {
			t.Errorf("%v: got %v, want status %d", c.err, err, c.status)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(ae.Message, w) {
				t.Errorf("%v: message %q lacks %q", c.err, ae.Message, w)
			}
		}
		for _, leak := range []string{"json:", "Go value", "message", "/repos/", "/app/"} {
			if strings.Contains(ae.Message, leak) {
				t.Errorf("message %q shows %q", ae.Message, leak)
			}
		}
	}
}
