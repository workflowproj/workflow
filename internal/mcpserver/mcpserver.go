// Package mcpserver exposes the tracker to AI assistants over the Model Context Protocol
// (streamable HTTP at /mcp). Tools call the same handlers as the REST API, as the person whose
// access token authenticated the request, so permissions, workflow rules, history and
// automation apply exactly as they do in the UI.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/identity"
)

const instructions = `Workflow is the team's issue tracker (projects, epics, stories, tasks, bugs, sub-tasks, sprints, releases).
Issues are identified by keys such as PLAT-12. Find work with search_issues (JQL), read one with get_issue (it lists the
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
}

// Handler serves /mcp. It accepts only bearer tokens (personal access tokens or service-account
// tokens): a browser cookie never reaches the tools, so a web page cannot drive them.
func (s *Server) Handler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s.server(identity.ActorFrom(r.Context()))
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: s.Log})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			unauthorized(w, "send an access token: Authorization: Bearer <token>")
			return
		}
		a, err := s.Auth.Authenticate(r)
		if err != nil || a == nil || a.TokenID == nil {
			unauthorized(w, "invalid or expired access token")
			return
		}
		h.ServeHTTP(w, r.WithContext(identity.WithActor(r.Context(), a)))
	})
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="workflow"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"error":"unauthorized","message":%q}`, msg)
}

func (s *Server) server(a *authz.Actor) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "workflow", Title: "Workflow", Version: s.Version}, &mcp.ServerOptions{Instructions: instructions})
	t := &tools{api: s.API, actor: a, publicURL: strings.TrimRight(s.PublicURL, "/")}
	t.register(srv)
	return srv
}

// tools holds what every tool needs; handlers run with the actor in their context.
type tools struct {
	api       api.StrictServerInterface
	actor     *authz.Actor
	publicURL string
}

// ctx puts the actor where the API handlers look for it, after checking the token's scope
// allows the operation (read-only tokens can only use read tools).
func (t *tools) ctx(ctx context.Context, write bool) (context.Context, error) {
	method := "GET"
	if write {
		method = "POST"
	}
	if err := identity.CheckScope(t.actor, method, "/api/v1/mcp"); err != nil {
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
