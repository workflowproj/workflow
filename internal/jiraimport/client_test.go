package jiraimport

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/jiraimport/jiratest"
)

func TestClientPaginationAndRateLimits(t *testing.T) {
	site := jiratest.NewCloud(time.Now())
	site.Throttle = 2
	srv := site.Server()
	defer srv.Close()
	var waits []time.Duration
	c := &Client{Flavor: Cloud, BaseURL: srv.URL, Email: site.Email, Token: site.Token, HTTP: srv.Client(),
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }}
	ctx := context.Background()

	// Two 429s with Retry-After: 2, then pages of 2 (the fake's page size) until the last.
	var keys []string
	next := ""
	for {
		page, err := c.Search(ctx, jqlProject("PLAT", 0), []string{"summary"}, "", next, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, is := range page.Issues {
			keys = append(keys, is.Key)
		}
		if page.Last {
			break
		}
		next = page.Next
	}
	if got := len(keys); got != 6 || keys[0] != "PLAT-1" || keys[5] != "PLAT-7" {
		t.Fatalf("keys %v", keys)
	}
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 3*time.Second {
		t.Fatalf("waits %v (Retry-After, then Retry-After plus backoff)", waits)
	}

	// Resuming after a key skips what was done.
	page, err := c.Search(ctx, jqlProject("PLAT", 5), []string{"summary"}, "", "", 50)
	if err != nil || len(page.Issues) == 0 || page.Issues[0].Key != "PLAT-6" {
		t.Fatalf("resume: %v %v", page.Issues, err)
	}

	// Paged changelog and comments are fetched completely.
	hist, err := c.Changelog(ctx, "30001")
	if err != nil || len(hist) != 6 {
		t.Fatalf("changelog: %d %v", len(hist), err)
	}
	comments, err := c.Comments(ctx, "30001")
	if err != nil || len(comments) != 3 {
		t.Fatalf("comments: %d %v", len(comments), err)
	}
	projects, err := c.Projects(ctx)
	if err != nil || len(projects) != 2 {
		t.Fatalf("projects: %v %v", projects, err)
	}

	// Persistent server errors give up after the retries.
	site.FailSearches(0, 100)
	c.Retries = 2
	_, err = c.Search(ctx, jqlProject("PLAT", 0), nil, "", "", 50)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusInternalServerError {
		t.Fatalf("error %v", err)
	}

	// Bad credentials are not retried.
	c.Token = "nope"
	_, err = c.Myself(ctx)
	if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized {
		t.Fatalf("auth error %v", err)
	}
}

func TestRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := http.Header{"Retry-After": {now.Add(30 * time.Second).Format(http.TimeFormat)}}
	if d := retryAfter(h, now); d != 30*time.Second {
		t.Fatalf("got %v", d)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://ACME.atlassian.net/":                         "https://acme.atlassian.net",
		"https://acme.atlassian.net/browse/PLAT-12":           "https://acme.atlassian.net",
		"https://acme.atlassian.net/jira/software/projects/X": "https://acme.atlassian.net",
		"https://jira.example.com/jira/secure/Dashboard.jspa": "https://jira.example.com/jira",
		"http://localhost:8080/jira":                          "http://localhost:8080/jira",
	} {
		got, err := normalizeBaseURL(in)
		if err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"acme.atlassian.net", "ftp://x", "https://user:pw@x.io"} {
		if _, err := normalizeBaseURL(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestDefaults(t *testing.T) {
	for in, want := range map[string]string{"PLAT": "PLAT", "AB_C": "ABC", "a": "AX", "1PROJ": "PROJ", "VERYLONGPROJECTKEY": "VERYLONGPR"} {
		if got := defaultKey(in); got != want {
			t.Errorf("defaultKey(%s) = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"Blocker": "highest", "Major": "high", "Medium": "medium", "Trivial": "lowest", "P3": "medium"} {
		if got := defaultPriority(in); got != want {
			t.Errorf("defaultPriority(%s) = %s, want %s", in, got, want)
		}
	}
	scrum, _ := templateTarget("scrum")
	if got := defaultStatus(scrum, "In Review", "in_progress"); got != "In Review" {
		t.Errorf("by name: %s", got)
	}
	if got := defaultStatus(scrum, "Waiting for QA", "in_progress"); got != "In Progress" {
		t.Errorf("by category: %s", got)
	}
	if got := defaultType(scrum, "Initiative", 1); got != "epic" {
		t.Errorf("epic by level: %s", got)
	}
	bugs, _ := templateTarget("bug_tracking")
	if got := defaultType(bugs, "Epic", 1); got != "task" {
		t.Errorf("epic without an epic type: %s", got)
	}
	sp := []Sprint{{ID: 1, State: "closed"}, {ID: 3, State: "future"}, {ID: 2, State: "active"}}
	if got := currentSprint(sp); got.ID != 2 {
		t.Errorf("current sprint %d", got.ID)
	}
	if got := currentSprint([]Sprint{{ID: 4, State: "closed"}, {ID: 9, State: "closed"}}); got.ID != 9 {
		t.Errorf("last closed sprint %d", got.ID)
	}
	labels, dropped := normalizeLabels([]string{"tech debt", "tech debt", " x ", ""})
	if dropped || len(labels) != 2 || labels[0] != "tech-debt" || labels[1] != "x" {
		t.Errorf("labels %v", labels)
	}
}
