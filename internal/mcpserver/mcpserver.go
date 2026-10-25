// Package mcpserver exposes the tracker to AI assistants over the Model Context Protocol
// (streamable HTTP at /mcp). Tools call the same handlers as the REST API, as the person whose
// access token authenticated the request, so permissions, workflow rules, history and
// automation apply exactly as they do in the UI.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/oauth"
)

const instructions = `Workflow is the team's issue tracker (projects, epics, stories, tasks, bugs, sub-tasks, sprints, releases).
Issues are identified by keys such as PLAT-12. Find work with search_issues (a JQL-like query language), read one with get_issue (it lists the
transitions available right now), and learn a project's issue types, boards and sprints with get_project.
Status changes go through the project's workflow: use transition_issue with one of the transitions get_issue lists.
Everything you do is recorded in the issue history as the person who owns the access token.`

// Server builds an MCP server per request, bound to the authenticated actor.
type Server struct {
	API       api.StrictServerInterface
	Auth      *identity.Auth
	PublicURL string
	Version   string
	Log       *slog.Logger
	// OAuth verifies access tokens issued to connected apps and names the protected resource
	// metadata advertised on 401s; nil (or disabled) leaves /mcp to personal access tokens.
	OAuth *oauth.Server
}

// Handler serves /mcp. It accepts only bearer tokens (OAuth access tokens issued for /mcp,
// personal access tokens or service-account tokens): a browser cookie never reaches the tools,
// so a web page cannot drive them. Failures follow RFC 6750 and the MCP authorization spec: 401
// with the protected resource metadata URL so clients can start the OAuth flow, and 403
// insufficient_scope when a read-only connected app calls a write tool (step-up).
func (s *Server) Handler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s.server(identity.ActorFrom(r.Context()), connectedApp(r))
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, Logger: s.Log,
		// The SDK's DNS-rebinding guard rejects requests that reach a loopback address with
		// another Host, which is every request behind a service-mesh sidecar, a local reverse
		// proxy or kubectl port-forward. It protects unauthenticated local servers; /mcp needs
		// a bearer token, which a rebinding web page cannot send.
		DisableLocalhostProtection: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			s.challenge(w, http.StatusUnauthorized, "", "send an access token: Authorization: Bearer <token>")
			return
		}
		var a *authz.Actor
		var err error
		if strings.HasPrefix(token, oauth.AccessTokenPrefix) {
			if s.OAuth.Enabled() {
				a, err = s.OAuth.Authenticate(r.Context(), token)
			}
		} else {
			a, err = s.Auth.Authenticate(r)
		}
		if err != nil || a == nil || a.TokenID == nil {
			s.challenge(w, http.StatusUnauthorized, "invalid_token", "invalid or expired access token")
			return
		}
		// Customers of a service desk only use its portal.
		if a.IsCustomer() {
			s.challenge(w, http.StatusForbidden, "access_denied", "customers use the help centre portal; assistants are for agents")
			return
		}
		// Changes are recorded as "<person> via <token name>", e.g. "Ada via Claude".
		if a.Via == "" {
			a.Via = a.TokenName
			if a.Via == "" {
				a.Via = "AI assistant"
			}
		}
		// A connected app allowed only to read is refused write tools at the HTTP level, so the
		// client can ask the person for more access (step-up authorisation).
		if connectedApp(r) && !oauth.CanWrite(a) && r.Method == http.MethodPost {
			if tool := writeToolCall(r); tool != "" {
				s.challenge(w, http.StatusForbidden, "insufficient_scope", fmt.Sprintf("%s may only read: %s changes issues; allow changes to use it", a.Via, tool))
				return
			}
		}
		h.ServeHTTP(w, r.WithContext(identity.WithActor(r.Context(), a)))
	})
}

// connectedApp reports a request authenticated with an OAuth access token, i.e. from an app the
// person connected in the browser (rather than with a personal access token).
func connectedApp(r *http.Request) bool {
	t, ok := bearerToken(r)
	return ok && strings.HasPrefix(t, oauth.AccessTokenPrefix)
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "Bearer ") {
		return "", false
	}
	t := strings.TrimSpace(h[7:])
	return t, t != ""
}

// challenge answers 401 or 403 with an RFC 6750 WWW-Authenticate challenge that points at the
// protected resource metadata (RFC 9728 §5.1) and names the scopes to ask for.
func (s *Server) challenge(w http.ResponseWriter, status int, code, msg string) {
	params := []string{`realm="workflow"`}
	if code != "" {
		params = append(params, fmt.Sprintf("error=%q", code), fmt.Sprintf("error_description=%q", msg))
	}
	if s.OAuth.Enabled() {
		params = append(params, fmt.Sprintf("resource_metadata=%q", s.OAuth.ResourceMetadataURL()), fmt.Sprintf("scope=%q", strings.Join(oauth.Scopes, " ")))
	}
	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(params, ", "))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if code == "" {
		code = "unauthorized"
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": msg, "message": msg})
}

// writeToolCall returns the name of a write tool the JSON-RPC request (or batch) calls, if any.
// The body is restored for the MCP handler.
func writeToolCall(r *http.Request) string {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	type call struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	var calls []call
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		_ = json.Unmarshal(trimmed, &calls)
	} else {
		var c call
		if json.Unmarshal(trimmed, &c) == nil {
			calls = append(calls, c)
		}
	}
	for _, c := range calls {
		if c.Method == "tools/call" && slices.Contains(writeTools, c.Params.Name) {
			return c.Params.Name
		}
	}
	return ""
}

func (s *Server) server(a *authz.Actor, connected bool) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "workflow", Title: "Workflow", Version: s.Version}, &mcp.ServerOptions{Instructions: instructions})
	t := &tools{api: s.API, actor: a, publicURL: strings.TrimRight(s.PublicURL, "/"), connected: connected}
	// A connected app allowed only to read sees only the read tools. Personal access tokens see
	// every tool and get a tool error if their scope does not allow it.
	t.readOnly = connected && !oauth.CanWrite(a)
	t.register(srv)
	return srv
}

// tools holds what every tool needs; handlers run with the actor in their context.
type tools struct {
	api       api.StrictServerInterface
	actor     *authz.Actor
	publicURL string
	// connected marks an app connected with OAuth (actor.Via names it); readOnly leaves the
	// write tools out of the server.
	connected, readOnly bool
}

// ctx puts the actor where the API handlers look for it, after checking the token's scope
// allows the operation (read-only tokens can only use read tools).
func (t *tools) ctx(ctx context.Context, write bool) (context.Context, error) {
	method := "GET"
	if write {
		method = "POST"
	}
	if err := identity.CheckScope(t.actor, method, "/api/v1/mcp"); err != nil {
		if t.connected {
			return ctx, fmt.Errorf("%s was allowed to read only; reconnect it and allow changes to create or update issues", t.actor.Via)
		}
		return ctx, errors.New("this access token is read-only; create one with the write scope to change issues")
	}
	return identity.WithActor(ctx, t.actor), nil
}

// as unwraps a typed API response, turning API errors into readable tool errors.
func as[T any](resp any, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, toolError(err)
	}
	if v, ok := resp.(T); ok {
		return v, nil
	}
	return zero, fmt.Errorf("unexpected response %T", resp)
}

// toolError explains an API error in one line, with field errors.
func toolError(err error) error {
	var e *apperr.Error
	if !errors.As(err, &e) {
		return err
	}
	msg := e.Message
	var details []string
	for _, f := range e.Fields {
		if f.Field != "" {
			details = append(details, f.Field+": "+f.Message)
		} else if f.Message != msg {
			details = append(details, f.Message)
		}
	}
	if len(details) > 0 {
		msg += " (" + strings.Join(details, "; ") + ")"
	}
	if e.Position != nil {
		msg += fmt.Sprintf(" at character %d", *e.Position)
	}
	return errors.New(msg)
}

// text is a tool result made of plain text.
func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}
