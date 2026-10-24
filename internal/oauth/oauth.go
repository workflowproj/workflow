// Package oauth makes Workflow the OAuth 2.1 authorisation server for AI assistants that connect
// to /mcp, following the MCP authorization specification (2025-11-25): protected resource
// metadata (RFC 9728), authorisation server metadata (RFC 8414), client ID metadata documents
// and dynamic client registration (RFC 7591), the authorisation code grant with PKCE (S256 only)
// and resource indicators (RFC 8707), refresh token rotation with reuse detection, and token
// revocation (RFC 7009).
//
// People approve an assistant on a consent page in the web app; the grant is remembered per
// client and scope set and listed under Profile → Connected apps. Access tokens are opaque,
// stored hashed, valid only for the MCP endpoint (their audience) and act as the person, limited
// to read tools or read and write tools.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/netguard"
	"github.com/geoffo-dev/workflow-management/internal/ratelimit"
	"github.com/geoffo-dev/workflow-management/internal/secure"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

const (
	// AccessTokenPrefix marks OAuth access tokens; they are accepted by /mcp only.
	AccessTokenPrefix  = "wfm_oat_"
	RefreshTokenPrefix = "wfm_ort_"
	codePrefix         = "wfm_oac_"
	clientIDPrefix     = "wfmc_"
	clientSecretPrefix = "wfm_ocs_"

	// ScopeRead lets an assistant use the read tools; ScopeWrite adds the tools that change issues.
	ScopeRead  = "mcp:read"
	ScopeWrite = "mcp:write"

	codeTTL = 2 * time.Minute
	// requestTTL bounds how long a person may take to sign in and decide on the consent page.
	requestTTL = 30 * time.Minute
)

// Scopes are the scopes this server issues, least privileged first.
var Scopes = []string{ScopeRead, ScopeWrite}

// Server is the authorisation server and the token verifier for /mcp.
type Server struct {
	Cfg   *config.Config
	Pool  *pgxpool.Pool
	Q     *dbq.Queries
	Box   *secure.Box
	Auth  *identity.Auth
	Track *tracker.Service
	Log   *slog.Logger
	// HTTPClient fetches client ID metadata documents. It must not follow redirects and should
	// refuse private addresses; New sets an SSRF-guarded client.
	HTTPClient *http.Client

	resource      string
	registerLimit *ratelimit.Limiter
	tokenLimit    *ratelimit.Limiter
	fetchLimit    *ratelimit.Limiter
}

// New builds the server. Client ID metadata documents are fetched with the same outbound guard
// as webhooks (WFM_WEBHOOK_ALLOW_PRIVATE, WFM_OUTBOUND_ALLOWED_HOSTS).
func New(cfg *config.Config, pool *pgxpool.Pool, q *dbq.Queries, box *secure.Box, auth *identity.Auth, track *tracker.Service, log *slog.Logger) *Server {
	fetch := netguard.Client(5*time.Second, cfg.WebhookAllowPrivate, cfg.OutboundAllowedHosts...)
	fetch.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Server{
		Cfg: cfg, Pool: pool, Q: q, Box: box, Auth: auth, Track: track, Log: log, HTTPClient: fetch,
		resource:      canonicalResource(cfg.PublicURL + "/mcp"),
		registerLimit: ratelimit.New(30, 30),
		tokenLimit:    ratelimit.New(120, 60),
		fetchLimit:    ratelimit.New(30, 10),
	}
}

// Enabled reports whether assistants may sign in with OAuth.
func (s *Server) Enabled() bool { return s != nil && s.Cfg.OAuth.Enabled }

// Issuer is the authorisation server's issuer identifier: the public URL.
func (s *Server) Issuer() string { return s.Cfg.PublicURL }

// Resource is the MCP endpoint's canonical URI, the audience of every access token.
func (s *Server) Resource() string { return s.resource }

// ResourceMetadataURL is where /mcp's protected resource metadata is published (RFC 9728 §3.1).
func (s *Server) ResourceMetadataURL() string {
	return s.Cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp"
}

// Routes mounts the discovery documents and the OAuth endpoints. The consent page itself is
// part of the web app (/oauth/consent) and calls the API.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.public(http.MethodGet, s.protectedResourceMetadata))
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", s.public(http.MethodGet, s.protectedResourceMetadata))
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.public(http.MethodGet, s.authServerMetadata))
	mux.HandleFunc("/.well-known/openid-configuration", s.public(http.MethodGet, s.authServerMetadata))
	mux.HandleFunc("/oauth/register", s.public(http.MethodPost, s.register))
	mux.HandleFunc("/oauth/token", s.public(http.MethodPost, s.token))
	mux.HandleFunc("/oauth/revoke", s.public(http.MethodPost, s.revoke))
	mux.HandleFunc("/oauth/authorize", s.authorize)
}

// public wraps the endpoints clients call directly: CORS for browser-based clients (without
// credentials: these endpoints never read cookies), the method check and the on/off switch.
func (s *Server) public(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Access-Control-Allow-Origin", "*")
		hd.Set("Access-Control-Allow-Methods", method+", OPTIONS")
		hd.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
		hd.Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !s.Enabled() {
			writeError(w, http.StatusNotFound, "invalid_request", "OAuth sign-in for AI assistants is disabled on this server")
			return
		}
		if r.Method != method && !(method == http.MethodGet && r.Method == http.MethodHead) {
			hd.Set("Allow", method+", OPTIONS")
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "use "+method)
			return
		}
		h(w, r)
	}
}

func (s *Server) protectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, &oauthex.ProtectedResourceMetadata{
		Resource:               s.Resource(),
		AuthorizationServers:   []string{s.Issuer()},
		ScopesSupported:        Scopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Workflow",
	})
}

// authServerMetadata is RFC 8414 metadata, also served as /.well-known/openid-configuration for
// clients that only try OpenID discovery (it lists PKCE support, which MCP clients require).
func (s *Server) authServerMetadata(w http.ResponseWriter, _ *http.Request) {
	iss := s.Issuer()
	m := map[string]any{
		"issuer":                                         iss,
		"authorization_endpoint":                         iss + "/oauth/authorize",
		"token_endpoint":                                 iss + "/oauth/token",
		"revocation_endpoint":                            iss + "/oauth/revoke",
		"scopes_supported":                               Scopes,
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none", "client_secret_post", "client_secret_basic"},
		"revocation_endpoint_auth_methods_supported":     []string{"none", "client_secret_post", "client_secret_basic"},
		"authorization_response_iss_parameter_supported": true,
		"client_id_metadata_document_supported":          s.Cfg.OAuth.ClientMetadataDocuments,
		"service_documentation":                          "https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization",
	}
	if s.Cfg.OAuth.DynamicRegistration {
		m["registration_endpoint"] = iss + "/oauth/register"
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, m)
}

// ---------------------------------------------------------------------------
// Scopes

// parseScopes keeps the scopes this server knows, in canonical order, with write implying read.
// Unknown scopes (offline_access, openid, …) are ignored rather than refused, as RFC 6749 §3.3
// allows; no known scope means "everything", and the person chooses on the consent page.
func parseScopes(raw string) []string {
	fields := strings.Fields(raw)
	write := slices.Contains(fields, ScopeWrite)
	read := slices.Contains(fields, ScopeRead)
	switch {
	case write:
		return []string{ScopeRead, ScopeWrite}
	case read:
		return []string{ScopeRead}
	default:
		return slices.Clone(Scopes)
	}
}

// normalizeScopes returns the canonical form of a scope list (write implies read).
func normalizeScopes(in []string) []string { return parseScopes(strings.Join(in, " ")) }

func covers(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func intersect(a, b []string) []string {
	var out []string
	for _, s := range a {
		if slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

// ActorScopes maps OAuth scopes onto the token scopes the API checks (identity.CheckScope).
func ActorScopes(oauthScopes []string) []string {
	if slices.Contains(oauthScopes, ScopeWrite) {
		return []string{"read", "write"}
	}
	return []string{"read"}
}

// CanWrite reports whether an actor may use write tools.
func CanWrite(a *authz.Actor) bool {
	return a != nil && (slices.Contains(a.Scopes, "write") || slices.Contains(a.Scopes, "admin"))
}

// ---------------------------------------------------------------------------
// Resource indicators

// canonicalResource lower-cases the scheme and host, drops default ports and a trailing slash.
func canonicalResource(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	u.Host = host
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String()
}

// resourceMatches checks an RFC 8707 resource indicator against the MCP endpoint.
func (s *Server) resourceMatches(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || strings.Contains(raw, "#") || !u.IsAbs() {
		return false
	}
	return canonicalResource(raw) == s.Resource()
}

// ---------------------------------------------------------------------------
// HTTP helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an OAuth error response (RFC 6749 §5.2).
func writeError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// limited applies a rate limit keyed by client address, answering 429 when exceeded.
func limited(w http.ResponseWriter, r *http.Request, l *ratelimit.Limiter) bool {
	ok, wait := l.Allow(clientIP(r))
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many requests; try again shortly")
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// sessionUser is the person signed in to the web app in this browser, if any. Bearer tokens
// are ignored: only the browser session can authorise an assistant.
func (s *Server) sessionUser(r *http.Request) *authz.Actor {
	r2 := r.Clone(r.Context())
	r2.Header.Del("Authorization")
	a, err := s.Auth.Authenticate(r2)
	if err != nil || a == nil || a.User == nil {
		return nil
	}
	return a
}

var errInvalidToken = errors.New("invalid or expired access token")

// ErrInvalidToken is returned by Authenticate for unknown, expired, revoked or misdirected tokens.
var ErrInvalidToken = errInvalidToken

func audit(ctx context.Context, q *dbq.Queries, a *authz.Actor, action string, client dbq.OauthClient, grantID string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["clientId"] = client.ID
	data["clientName"] = client.Name
	tracker.Audit(ctx, q, a, action, "oauth_grant", grantID, nil, data)
}
