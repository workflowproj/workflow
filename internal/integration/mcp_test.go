package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpSession connects to /mcp the way an AI assistant does, with an access token.
func (e *Env) mcpSession(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: e.Srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call runs a tool and returns its text, failing the test on a protocol error.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func mustCall(t *testing.T, cs *mcp.ClientSession, tool string, args any) string {
	t.Helper()
	out, isErr := call(t, cs, tool, args)
	if isErr {
		t.Fatalf("%s failed: %s", tool, out)
	}
	return out
}

func wantContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("expected %q in:\n%s", w, got)
		}
	}
}

func TestMCPServer(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("AI", "scrum")
	ada.must("POST", "/api/v1/projects/AI/members", obj{"userId": bob.id(), "roleKey": "member"}, 201, nil)
	token := func(c *Client, scopes ...string) string {
		var tok obj
		c.must("POST", "/api/v1/me/tokens", obj{"name": "assistant", "scopes": scopes, "expiresInDays": 7}, 201, &tok)
		return str(tok, "token")
	}
	cs := e.mcpSession(t, token(ada, "read", "write"))

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Name == "search_issues" && !tool.Annotations.ReadOnlyHint {
			t.Fatal("read tools are marked read-only")
		}
	}
	for _, want := range []string{"search_issues", "get_issue", "create_issue", "update_issue", "transition_issue", "add_comment", "get_metrics", "get_sprint_report"} {
		if !slices.Contains(names, want) {
			t.Fatalf("missing tool %s in %v", want, names)
		}
	}

	wantContains(t, mustCall(t, cs, "whoami", obj{}), "Ada <ada@example.com>", "read, write")
	wantContains(t, mustCall(t, cs, "list_projects", obj{}), "AI — ")
	wantContains(t, mustCall(t, cs, "get_project", obj{"key": "AI"}), "story — Story", "Sprints:", "Boards:")

	// Create, update, transition, comment, link.
	created := mustCall(t, cs, "create_issue", obj{
		"projectKey": "AI", "type": "story", "summary": "Let assistants file bugs", "description": "From **MCP**.",
		"priority": "high", "assignee": "bob@example.com", "storyPoints": 3, "labels": []string{"ai"},
	})
	wantContains(t, created, "Created AI-1: Let assistants file bugs", "/browse/AI-1")
	issue := ada.issue("AI-1")
	eq(t, str(issue, "assignee", "email"), "bob@example.com", "assignee resolved by email")
	eq(t, str(issue, "description"), "From **MCP**.", "markdown description")

	mustCall(t, cs, "update_issue", obj{"key": "AI-1", "dueDate": "2026-12-01", "sprint": "AI Sprint 1", "clear": []string{"labels"}})
	issue = ada.issue("AI-1")
	eq(t, str(issue, "dueDate"), "2026-12-01", "due date")
	eq(t, len(issue["labels"].([]any)), 0, "labels cleared")
	if str(issue, "sprintId") == "" {
		t.Fatal("sprint set by name")
	}
	out, isErr := call(t, cs, "transition_issue", obj{"key": "AI-1", "transition": "Teleport"})
	if !isErr || !strings.Contains(out, "start-work") {
		t.Fatalf("unknown transitions list the available ones: %s", out)
	}
	wantContains(t, mustCall(t, cs, "transition_issue", obj{"key": "AI-1", "transition": "In Progress", "comment": "Picked up by the assistant"}), "AI-1 is now In Progress")
	mustCall(t, cs, "add_comment", obj{"key": "AI-1", "body": "Looks good"})
	mustCall(t, cs, "create_issue", obj{"projectKey": "AI", "type": "task", "summary": "Follow-up"})
	mustCall(t, cs, "link_issues", obj{"key": "AI-1", "type": "blocks", "targetKey": "AI-2"})

	detail := mustCall(t, cs, "get_issue", obj{"key": "AI-1"})
	wantContains(t, detail, "AI-1: Let assistants file bugs", "Status: In Progress", "Points: 3", "Due: 2026-12-01",
		"Transitions available:", "blocks AI-2", "Picked up by the assistant", "Looks good", "Timing:", "From **MCP**.")
	wantContains(t, mustCall(t, cs, "search_issues", obj{"jql": "project = AI ORDER BY key"}), "2 issues match", "AI-1 [Story · In Progress", "AI-2 [Task")
	_, isErr = call(t, cs, "search_issues", obj{"jql": "status =="})
	if !isErr {
		t.Fatal("JQL errors are tool errors")
	}
	wantContains(t, mustCall(t, cs, "get_metrics", obj{"kind": "breakdown", "projects": []string{"AI"}, "by": "status"}), "2 issues", "In Progress: 1 issues")
	wantContains(t, mustCall(t, cs, "get_board", obj{"projectKey": "AI"}), "No active sprint")
	wantContains(t, mustCall(t, cs, "find_users", obj{"query": "bob"}), "Bob <bob@example.com>")

	// The history records the token's owner, as in the UI.
	var hist []obj
	ada.get("/api/v1/issues/AI-1/history", &hist)
	eq(t, str(hist[len(hist)-1], "actor", "displayName"), "Ada", "changes are made as the token owner")

	// Permissions are the owner's: Bob cannot see a private project.
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	bobCS := e.mcpSession(t, token(bob, "read", "write"))
	out, isErr = call(t, bobCS, "get_project", obj{"key": "SEC"})
	if !isErr || !strings.Contains(out, "not found") {
		t.Fatalf("private project: %s", out)
	}

	// A read-only token cannot change anything.
	readCS := e.mcpSession(t, token(ada, "read"))
	mustCall(t, readCS, "search_issues", obj{"jql": ""})
	out, isErr = call(t, readCS, "add_comment", obj{"key": "AI-1", "body": "nope"})
	if !isErr || !strings.Contains(out, "read-only") {
		t.Fatalf("read-only token: %s", out)
	}

	// No token, a bad token or a browser session: 401.
	for _, hdr := range [][]string{nil, {"Authorization", "Bearer wfm_pat_nope"}} {
		r := ada.do("POST", "/mcp", json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), hdr...)
		if r.Status != http.StatusUnauthorized || r.Header.Get("WWW-Authenticate") == "" {
			t.Fatalf("unauthenticated /mcp: %d %s", r.Status, r.Body)
		}
	}

	// Behind a sidecar or local reverse proxy, requests reach the server from loopback with the
	// public host name; the bearer token is what protects /mcp.
	r := ada.do("POST", "/mcp", json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"proxy","version":"1"}}}`),
		"Host", "wfm.example.com", "Authorization", "Bearer "+token(ada, "read"), "Accept", "application/json, text/event-stream")
	eq(t, r.Status, http.StatusOK, "proxied host name: "+string(r.Body))

	// The OpenAPI description is published.
	var spec obj
	ada.get("/api/v1/openapi.json", &spec)
	eq(t, str(spec, "info", "title") != "", true, "openapi.json")
	r = ada.do("GET", "/api/v1/openapi.yaml", nil)
	eq(t, strings.HasPrefix(string(r.Body), "openapi:"), true, "openapi.yaml")
}
