// Package scm connects GitHub and GitLab. Both sit behind the Provider interface, so Gitea or
// Bitbucket can follow. Webhooks are verified and stored on receipt (hooks mode), then
// processed asynchronously and idempotently by the worker; a reconcile job fills gaps from
// missed deliveries using the provider APIs.
package scm

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Connection is a decrypted SCM connection (GitHub App or GitLab instance).
type Connection struct {
	ID            string
	Provider      string
	APIURL        string
	WebURL        string
	AppID         int64
	PrivateKey    string
	Token         string
	WebhookSecret string
}

type RepoRef struct {
	ExternalID     string
	FullPath       string
	WebURL         string
	DefaultBranch  string
	InstallationID int64
}

type Commit struct {
	SHA         string
	Message     string
	URL         string
	AuthorName  string
	AuthorEmail string
	Timestamp   time.Time
}

// Normalised events produced by providers.
type (
	PushEvent struct {
		Repo    RepoRef
		Branch  string
		Created bool
		Deleted bool
		Commits []Commit
	}
	BranchEvent struct {
		Repo    RepoRef
		Branch  string
		Deleted bool
	}
	PullRequestEvent struct {
		Repo         RepoRef
		Number       string
		Title        string
		Body         string
		URL          string
		State        string // open | draft | merged | closed
		Action       string // opened | reopened | closed | merged | ready | edited | updated | draft
		SourceBranch string
		TargetBranch string
		Author       string
		HeadSHA      string
		UpdatedAt    time.Time
	}
	ReviewEvent struct {
		Repo     RepoRef
		Number   string
		State    string // approved | changes_requested | commented
		Reviewer string
	}
	PipelineEvent struct {
		Repo          RepoRef
		ID            string
		Name          string
		Status        string // pending | running | success | failed | canceled
		URL           string
		Branch        string
		SHA           string
		CommitMessage string
		UpdatedAt     time.Time
	}
	// RepositoriesEvent reports repositories added to or removed from an installation.
	RepositoriesEvent struct {
		Added   []RepoRef
		Removed []RepoRef
	}
)

// Delivery is the identity of an inbound webhook.
type Delivery struct {
	Event string
	ID    string
}

// Provider is implemented by each SCM.
type Provider interface {
	Name() string
	// Verify authenticates an inbound webhook and returns its event type and delivery id.
	Verify(r *http.Request, body []byte, secret string) (Delivery, error)
	// Parse turns a stored payload into normalised events (nil for events we ignore).
	Parse(eventType string, payload []byte) ([]any, error)
	// Client returns an API client for write-back, reconcile and branch creation.
	Client(ctx context.Context, conn Connection, httpClient *http.Client) (Client, error)
}

// PullRequest is a PR/MR as listed by the reconcile job.
type PullRequest = PullRequestEvent

type Client interface {
	ListRepositories(ctx context.Context) ([]RepoRef, error)
	ListPullRequests(ctx context.Context, repo RepoRef) ([]PullRequest, error)
	ListBranches(ctx context.Context, repo RepoRef) ([]string, error)
	Comment(ctx context.Context, repo RepoRef, number, body string) error
	CreateBranch(ctx context.Context, repo RepoRef, name, from string) error
	// EnsureHook creates a project webhook (GitLab Free); returns the hook id. GitHub Apps use the app-level hook.
	EnsureHook(ctx context.Context, repo RepoRef, url, secret string) (string, error)
}

// keyPattern matches a candidate key; the character after it is checked separately (RE2 has no
// lookahead), so keys can be followed by "_" or "/" as in branch names like PLAT-12_login.
var keyPattern = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])([A-Za-z][A-Za-z0-9]{1,9})-([1-9][0-9]{0,8})`)

// ExtractKeys finds issue keys such as PLAT-12 in branch names, commit messages and titles,
// keeping only keys whose project is in allowed (upper-case project keys). Matching ignores case
// and separators, so feature/plat-12-login, PLAT-12_fix and "Fix PLAT-12: …" all name PLAT-12.
func ExtractKeys(allowed map[string]bool, texts ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range texts {
		for _, m := range keyPattern.FindAllStringSubmatchIndex(t, -1) {
			if end := m[1]; end < len(t) && isAlphaNum(t[end]) {
				continue // e.g. PLAT-12abc is not a key
			}
			project := strings.ToUpper(t[m[2]:m[3]])
			if !allowed[project] {
				continue
			}
			key := project + "-" + t[m[4]:m[5]]
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out
}

func isAlphaNum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// SmartCommand is one "#command" from a commit message, attached to issue keys.
type SmartCommand struct {
	Keys    []string
	Command string // "comment" or a transition / status name slug
	Arg     string
}

var commandPattern = regexp.MustCompile(`#([A-Za-z][A-Za-z0-9_-]*)`)

// ParseSmartCommits reads smart commits: "PLAT-12 PLAT-13 #in-review #comment ready for eyes".
// Commands apply to the keys on the same line; #comment takes the rest of the line.
func ParseSmartCommits(allowed map[string]bool, message string) []SmartCommand {
	var out []SmartCommand
	for _, line := range strings.Split(message, "\n") {
		keys := ExtractKeys(allowed, line)
		if len(keys) == 0 {
			continue
		}
		locs := commandPattern.FindAllStringSubmatchIndex(line, -1)
		for _, loc := range locs {
			cmd := strings.ToLower(line[loc[2]:loc[3]])
			if cmd == "time" {
				continue // time tracking is out of scope for v1
			}
			if cmd == "comment" {
				arg := strings.TrimSpace(line[loc[1]:])
				if arg != "" {
					out = append(out, SmartCommand{Keys: keys, Command: "comment", Arg: arg})
				}
				break // the comment consumes the rest of the line
			}
			out = append(out, SmartCommand{Keys: keys, Command: cmd})
		}
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
