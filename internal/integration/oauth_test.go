package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// ---------------------------------------------------------------------------
// OAuth helpers: what an MCP client and a browser do

type pkce struct{ verifier, challenge string }

func newPKCE() pkce {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	v := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(v))
	return pkce{v, base64.RawURLEncoding.EncodeToString(sum[:])}
}

// postForm sends an application/x-www-form-urlencoded request, as clients call /oauth/token.
func (e *Env) postForm(path string, v url.Values) Response {
	e.t.Helper()
	res, err := http.Post(e.Srv.URL+path, "application/x-www-form-urlencoded", strings.NewReader(v.Encode()))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return Response{Status: res.StatusCode, Body: raw, Header: res.Header}
}

// register performs dynamic client registration.
func (e *Env) register(name string, redirects ...string) string {
	e.t.Helper()
	r := e.anon().do("POST", "/oauth/register", obj{"client_name": name, "redirect_uris": redirects, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("register: %d %s", r.Status, r.Body)
	}
	return str(r.JSON(), "client_id")
}

type authzReq struct {
	client, redirect, scope, state, resource string
	pkce                                     pkce
	extra                                    url.Values
}

func (e *Env) authorizePath(a authzReq) string {
	v := url.Values{"response_type": {"code"}, "client_id": {a.client}, "redirect_uri": {a.redirect}, "state": {a.state},
		"code_challenge": {a.pkce.challenge}, "code_challenge_method": {"S256"}}
	if a.scope != "" {
		v.Set("scope", a.scope)
	}
	if a.resource != "-" {
		res := a.resource
		if res == "" {
			res = e.Srv.URL + "/mcp"
		}
		v.Set("resource", res)
	}
	for k, vs := range a.extra {
		v[k] = vs
	}
	return "/oauth/authorize?" + v.Encode()
}

// location follows nothing: it returns the redirect target of a 302.
func location(t *testing.T, r Response) *url.URL {
	t.Helper()
	if r.Status != http.StatusFound {
		t.Fatalf("expected a redirect, got %d %s", r.Status, r.Body)
	}
	u, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// consentRequest returns the sealed request of a consent page URL (/oauth/consent/<request>).
func consentRequest(to *url.URL) string {
	req, ok := strings.CutPrefix(to.Path, "/oauth/consent/")
	if !ok {
		return ""
	}
	return req
}

// consent walks the browser through /oauth/authorize and the consent page, returning the
// redirect back to the client.
func (c *Client) consent(a authzReq, approve bool, scopes ...string) *url.URL {
	t := c.e.t
	t.Helper()
	to := location(t, c.do("GET", c.e.authorizePath(a), nil))
	if consentRequest(to) == "" {
		t.Fatalf("expected the consent page, got %s", to)
	}
	req := consentRequest(to)
	var details obj
	c.get("/api/v1/oauth/consent?request="+url.QueryEscape(req), &details)
	var out obj
	c.must("POST", "/api/v1/oauth/consent", obj{"request": req, "approve": approve, "scopes": scopes}, 200, &out)
	u, err := url.Parse(str(out, "redirectUrl"))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (e *Env) exchange(client, code, redirect, verifier string) Response {
	return e.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {e.Srv.URL + "/mcp"}})
}

func (e *Env) refreshTokens(client, refresh string) Response {
	return e.postForm("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {refresh}})
}

func wantOAuthError(t *testing.T, r Response, status int, code string) {
	t.Helper()
	if r.Status != status || str(r.JSON(), "error") != code {
		t.Fatalf("want %d %s, got %d %s", status, code, r.Status, r.Body)
	}
}

// mcpStatus posts one JSON-RPC message to /mcp with a token and returns the HTTP response.
func (e *Env) mcpPost(token, body string) Response {
	c := e.withToken(token)
	return c.do("POST", "/mcp", json.RawMessage(body), "Accept", "application/json, text/event-stream")
}

// ---------------------------------------------------------------------------

func TestOAuthDiscovery(t *testing.T) {
	e := newEnv(t)
	anon := e.anon()
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		var prm oauthex.ProtectedResourceMetadata
		r := anon.get(p, &prm)
		eq(t, prm.Resource, e.Srv.URL+"/mcp", "resource")
		sameStrings(t, prm.AuthorizationServers, []string{e.Srv.URL}, "authorization servers")
		sameStrings(t, prm.ScopesSupported, []string{"mcp:read", "mcp:write"}, "scopes")
		eq(t, r.Header.Get("Access-Control-Allow-Origin"), "*", "CORS")
	}
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		var m obj
		anon.get(p, &m)
		eq(t, str(m, "issuer"), e.Srv.URL, "issuer")
		eq(t, str(m, "authorization_endpoint"), e.Srv.URL+"/oauth/authorize", "authorize")
		eq(t, str(m, "token_endpoint"), e.Srv.URL+"/oauth/token", "token")
		eq(t, str(m, "registration_endpoint"), e.Srv.URL+"/oauth/register", "register")
		eq(t, str(m, "revocation_endpoint"), e.Srv.URL+"/oauth/revoke", "revoke")
		eq(t, fmt.Sprint(m["code_challenge_methods_supported"]), "[S256]", "PKCE")
		eq(t, slices.Contains(m["token_endpoint_auth_methods_supported"].([]any), any("none")), true, "public clients")
		eq(t, m["client_id_metadata_document_supported"], any(true), "CIMD")
		eq(t, fmt.Sprint(m["grant_types_supported"]), "[authorization_code refresh_token]", "grants")
	}
	// CORS preflight for browser-based clients, without credentials.
	r := anon.do("OPTIONS", "/oauth/token", nil, "Origin", "https://app.example", "Access-Control-Request-Method", "POST")
	eq(t, r.Status, http.StatusNoContent, "preflight")
	eq(t, r.Header.Get("Access-Control-Allow-Origin"), "*", "preflight origin")
	eq(t, r.Header.Get("Access-Control-Allow-Credentials"), "", "no credentials")

	// /mcp challenges point at the metadata and name the scopes.
	r = anon.do("POST", "/mcp", json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	eq(t, r.Status, http.StatusUnauthorized, "no token")
	wa := r.Header.Get("WWW-Authenticate")
	wantContains(t, wa, `resource_metadata="`+e.Srv.URL+`/.well-known/oauth-protected-resource/mcp"`, `scope="mcp:read mcp:write"`)
	r = e.mcpPost("wfm_oat_nope", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	eq(t, r.Status, http.StatusUnauthorized, "bad token")
	wantContains(t, r.Header.Get("WWW-Authenticate"), `error="invalid_token"`, "resource_metadata=")

	// Turning OAuth off leaves tokens only.
	off := newEnv(t, withEnv("WFM_OAUTH_ENABLED", "false"))
	r = off.anon().do("GET", "/.well-known/oauth-authorization-server", nil)
	eq(t, r.Status, http.StatusNotFound, "metadata when disabled")
	r = off.anon().do("POST", "/mcp", json.RawMessage(`{}`))
	eq(t, strings.Contains(r.Header.Get("WWW-Authenticate"), "resource_metadata"), false, "no OAuth challenge when disabled")
	noDCR := newEnv(t, withEnv("WFM_OAUTH_DYNAMIC_REGISTRATION", "false"))
	var m obj
	noDCR.anon().get("/.well-known/oauth-authorization-server", &m)
	eq(t, m["registration_endpoint"], nil, "no registration endpoint when disabled")
	r = noDCR.anon().do("POST", "/oauth/register", obj{"redirect_uris": []string{"https://app.example/cb"}})
	wantOAuthError(t, r, http.StatusForbidden, "access_denied")
}

func TestOAuthRegistration(t *testing.T) {
	e := newEnv(t)
	anon := e.anon()
	for _, bad := range [][]string{
		{"http://evil.example/cb"},        // http only on loopback
		{"javascript:alert(1)"},           // executable scheme
		{"https://app.example/cb#frag"},   // fragment
		{"https://user:pw@app.example/x"}, // user info
		{},                                // none
	} {
		r := anon.do("POST", "/oauth/register", obj{"client_name": "Bad", "redirect_uris": bad})
		wantOAuthError(t, r, http.StatusBadRequest, "invalid_redirect_uri")
	}
	r := anon.do("POST", "/oauth/register", obj{"redirect_uris": []string{"https://app.example/cb"}, "grant_types": []string{"client_credentials"}})
	wantOAuthError(t, r, http.StatusBadRequest, "invalid_client_metadata")
	r = anon.do("POST", "/oauth/register", "redirect_uris=https://x", "Content-Type", "application/x-www-form-urlencoded")
	wantOAuthError(t, r, http.StatusBadRequest, "invalid_client_metadata")

	// Public client (no secret); loopback, https and desktop-app redirects are accepted.
	r = anon.do("POST", "/oauth/register", obj{"client_name": "Cursor", "redirect_uris": []string{"http://127.0.0.1/callback", "https://app.example/cb", "cursor://anysphere.cursor-mcp/oauth/callback"},
		"logo_uri": "http://insecure.example/logo.png", "client_uri": "https://cursor.example"})
	eq(t, r.Status, http.StatusCreated, "register: "+string(r.Body))
	reg := r.JSON()
	eq(t, str(reg, "token_endpoint_auth_method"), "none", "public by default")
	eq(t, reg["client_secret"], nil, "no secret for public clients")
	eq(t, reg["logo_uri"], nil, "non-https logo dropped")
	eq(t, str(reg, "client_uri"), "https://cursor.example", "client uri kept")
	if !strings.HasPrefix(str(reg, "client_id"), "wfmc_") {
		t.Fatalf("client id: %v", reg["client_id"])
	}

	// Confidential client: the secret is required at the token endpoint.
	r = anon.do("POST", "/oauth/register", obj{"client_name": "Server app", "redirect_uris": []string{"https://app.example/cb"}, "token_endpoint_auth_method": "client_secret_post"})
	eq(t, r.Status, http.StatusCreated, "register confidential")
	conf := r.JSON()
	if !strings.HasPrefix(str(conf, "client_secret"), "wfm_ocs_") {
		t.Fatalf("secret: %s", r.Body)
	}
	tr := e.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {str(conf, "client_id")}, "code": {"x"}, "code_verifier": {strings.Repeat("a", 43)}})
	wantOAuthError(t, tr, http.StatusUnauthorized, "invalid_client")
	tr = e.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {str(conf, "client_id")}, "client_secret": {str(conf, "client_secret")}, "code": {"x"}, "code_verifier": {strings.Repeat("a", 43)}})
	wantOAuthError(t, tr, http.StatusBadRequest, "invalid_grant")
}

func TestOAuthFlow(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	ada.createProject("OA", "kanban")
	redirect := "http://127.0.0.1/callback"
	client := e.register("Claude", redirect, "https://claude.example/api/mcp/auth_callback")

	// Signed out: the consent page asks the person to sign in first (the page's API is 401).
	p := newPKCE()
	req := authzReq{client: client, redirect: "http://127.0.0.1:53682/callback", state: "st-1", pkce: p, scope: "mcp:read mcp:write"}
	anon := e.anon()
	to := location(t, anon.do("GET", e.authorizePath(req), nil))
	eq(t, consentRequest(to) != "", true, "signed out → consent page")
	r := anon.do("GET", "/api/v1/oauth/consent?request="+url.QueryEscape(consentRequest(to)), nil)
	eq(t, r.Status, http.StatusUnauthorized, "consent API needs a session")

	// Errors before the redirect URI is validated stay on Workflow; later ones go back to the client.
	for _, bad := range []authzReq{
		{client: client, redirect: "https://evil.example/cb", state: "s", pkce: p},
		{client: client, redirect: "http://localhost:53682/callback", state: "s", pkce: p}, // localhost ≠ 127.0.0.1
		{client: client, redirect: "https://claude.example/api/mcp/auth_callback/x", state: "s", pkce: p},
	} {
		to := location(t, ada.do("GET", e.authorizePath(bad), nil))
		eq(t, to.String(), "/oauth/consent?error=invalid_redirect_uri", "unregistered redirect URI")
	}
	to = location(t, ada.do("GET", e.authorizePath(authzReq{client: "wfmc_unknown", redirect: redirect, state: "s", pkce: p}), nil))
	eq(t, to.String(), "/oauth/consent?error=invalid_client", "unknown client")
	clientErr := func(a authzReq, code string) {
		t.Helper()
		to := location(t, ada.do("GET", e.authorizePath(a), nil))
		eq(t, to.Host+to.Path, "127.0.0.1:53682/callback", "error goes to the client")
		eq(t, to.Query().Get("error"), code, "error code")
		eq(t, to.Query().Get("state"), a.state, "state echoed")
		eq(t, to.Query().Get("iss"), e.Srv.URL, "iss")
	}
	clientErr(authzReq{client: client, redirect: req.redirect, state: "s", pkce: pkce{}}, "invalid_request")
	clientErr(authzReq{client: client, redirect: req.redirect, state: "s", pkce: p, extra: url.Values{"code_challenge_method": {"plain"}}}, "invalid_request")
	clientErr(authzReq{client: client, redirect: req.redirect, state: "", pkce: p}, "invalid_request")
	clientErr(authzReq{client: client, redirect: req.redirect, state: strings.Repeat("s", 600), pkce: p}, "invalid_request")
	clientErr(authzReq{client: client, redirect: req.redirect, state: "s", pkce: p, resource: "https://other.example/mcp"}, "invalid_target")
	clientErr(authzReq{client: client, redirect: req.redirect, state: "s", pkce: p, extra: url.Values{"response_type": {"token"}}}, "unsupported_response_type")
	clientErr(authzReq{client: client, redirect: req.redirect, state: "s", pkce: p, extra: url.Values{"prompt": {"none"}}}, "consent_required")

	// The consent page shows the app, where it returns to and what it asks for.
	to = location(t, ada.do("GET", e.authorizePath(req), nil))
	var details obj
	ada.get("/api/v1/oauth/consent?request="+url.QueryEscape(consentRequest(to)), &details)
	eq(t, str(details, "client", "name"), "Claude", "app name")
	eq(t, str(details, "client", "kind"), "dynamic", "registered client")
	eq(t, str(details, "redirectHost"), "127.0.0.1", "redirect host")
	eq(t, details["nativeRedirect"], any(true), "loopback redirect flagged")
	eq(t, details["alreadyGranted"], any(false), "first time")
	eq(t, fmt.Sprint(details["requestedScopes"]), "[mcp:read mcp:write]", "requested scopes")
	r = ada.do("GET", "/api/v1/oauth/consent?request=tampered", nil)
	eq(t, r.Status, http.StatusBadRequest, "tampered request")
	// Approval must come from the browser session, not a token.
	pat := func(scopes ...string) string {
		var tok obj
		ada.must("POST", "/api/v1/me/tokens", obj{"name": "t", "scopes": scopes, "expiresInDays": 7}, 201, &tok)
		return str(tok, "token")
	}
	r = e.withToken(pat("read", "write")).do("POST", "/api/v1/oauth/consent", obj{"request": consentRequest(to), "approve": true, "scopes": []string{"mcp:read"}})
	eq(t, r.Status, http.StatusForbidden, "tokens cannot approve apps")

	// Deny sends access_denied back to the client.
	back := ada.consent(req, false)
	eq(t, back.Query().Get("error"), "access_denied", "denied")
	eq(t, back.Query().Get("state"), "st-1", "denied state")

	// Approve read and write: a code comes back on the loopback port the client chose.
	back = ada.consent(req, true, "mcp:read", "mcp:write")
	eq(t, back.Host, "127.0.0.1:53682", "loopback port kept")
	eq(t, back.Query().Get("state"), "st-1", "state")
	eq(t, back.Query().Get("iss"), e.Srv.URL, "iss (RFC 9207)")
	code := back.Query().Get("code")

	// Token endpoint negatives: wrong verifier, wrong redirect, wrong resource, wrong client.
	wantOAuthError(t, e.exchange(client, code, req.redirect, newPKCE().verifier), http.StatusBadRequest, "invalid_grant")
	wantOAuthError(t, e.exchange(client, code, "http://127.0.0.1:1/other", p.verifier), http.StatusBadRequest, "invalid_grant")
	wantOAuthError(t, e.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "code_verifier": {p.verifier}, "resource": {"https://other.example/mcp"}}), http.StatusBadRequest, "invalid_target")
	other := e.register("Other", redirect)
	wantOAuthError(t, e.exchange(other, code, req.redirect, p.verifier), http.StatusBadRequest, "invalid_grant")
	wantOAuthError(t, e.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}}), http.StatusBadRequest, "invalid_request")
	wantOAuthError(t, e.postForm("/oauth/token", url.Values{"grant_type": {"password"}, "client_id": {client}}), http.StatusBadRequest, "unsupported_grant_type")

	// The right verifier gets tokens (failed attempts did not burn the code).
	r = e.exchange(client, code, req.redirect, p.verifier)
	eq(t, r.Status, http.StatusOK, "exchange: "+string(r.Body))
	eq(t, r.Header.Get("Cache-Control"), "no-store", "no-store")
	tok := r.JSON()
	access, refresh := str(tok, "access_token"), str(tok, "refresh_token")
	eq(t, str(tok, "token_type"), "Bearer", "token type")
	eq(t, str(tok, "scope"), "mcp:read mcp:write", "scope")
	eq(t, tok["expires_in"], any(float64(3600)), "one hour")
	if !strings.HasPrefix(access, "wfm_oat_") || !strings.HasPrefix(refresh, "wfm_ort_") {
		t.Fatalf("token prefixes: %s", r.Body)
	}

	// MCP tools work as Ada, through the app.
	cs := e.mcpSession(t, access)
	wantContains(t, mustCall(t, cs, "whoami", obj{}), "Ada <ada@example.com>", "read, write", "Connected app: Claude")
	wantContains(t, mustCall(t, cs, "create_issue", obj{"projectKey": "OA", "type": "task", "summary": "Filed by an assistant"}), "Created OA-1")
	var hist []obj
	ada.get("/api/v1/issues/OA-1/history", &hist)
	eq(t, str(hist[0], "actor", "displayName"), "Ada", "history records the person")

	// The access token's audience is /mcp: the REST API refuses it.
	r = e.withToken(access).do("GET", "/api/v1/me", nil)
	eq(t, r.Status, http.StatusUnauthorized, "OAuth tokens are for /mcp only")
	// PATs keep working on both.
	patTok := pat("read")
	e.withToken(patTok).get("/api/v1/me", nil)
	wantContains(t, mustCall(t, e.mcpSession(t, patTok), "whoami", obj{}), "Token scopes: read")

	// A code works once; replaying it revokes the tokens it produced.
	wantOAuthError(t, e.exchange(client, code, req.redirect, p.verifier), http.StatusBadRequest, "invalid_grant")
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`).Status, http.StatusUnauthorized, "replay revoked the access token")
	wantOAuthError(t, e.refreshTokens(client, refresh), http.StatusBadRequest, "invalid_grant")

	// Returning: consent is remembered, the code comes straight back.
	p = newPKCE()
	req = authzReq{client: client, redirect: "http://127.0.0.1:40000/callback", state: "st-2", pkce: p, scope: "mcp:read mcp:write"}
	back = location(t, ada.do("GET", e.authorizePath(req), nil))
	eq(t, back.Host, "127.0.0.1:40000", "remembered consent skips the page")
	r = e.exchange(client, back.Query().Get("code"), req.redirect, p.verifier)
	eq(t, r.Status, http.StatusOK, "exchange 2")
	access, refresh = str(r.JSON(), "access_token"), str(r.JSON(), "refresh_token")
	// prompt=consent shows the page anyway.
	to = location(t, ada.do("GET", e.authorizePath(authzReq{client: client, redirect: redirect, state: "s", pkce: p, extra: url.Values{"prompt": {"consent"}}}), nil))
	eq(t, consentRequest(to) != "", true, "prompt=consent")
	ada.get("/api/v1/oauth/consent?request="+url.QueryEscape(consentRequest(to)), &details)
	eq(t, details["alreadyGranted"], any(true), "already granted")

	// Refresh rotates: the new pair works, the old refresh token does not.
	r = e.refreshTokens(client, refresh)
	eq(t, r.Status, http.StatusOK, "refresh: "+string(r.Body))
	access2, refresh2 := str(r.JSON(), "access_token"), str(r.JSON(), "refresh_token")
	if access2 == access || refresh2 == refresh {
		t.Fatal("refresh rotates both tokens")
	}
	mustCall(t, e.mcpSession(t, access2), "list_projects", obj{})
	// Narrowing scope on refresh is allowed; widening is not.
	r = e.postForm("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {refresh2}, "scope": {"mcp:read"}})
	eq(t, r.Status, http.StatusOK, "narrowing refresh")
	eq(t, str(r.JSON(), "scope"), "mcp:read", "narrowed")
	refresh3 := str(r.JSON(), "refresh_token")
	wantOAuthError(t, e.postForm("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {refresh3}, "scope": {"mcp:write"}}), http.StatusBadRequest, "invalid_scope")
	// Reusing a rotated refresh token revokes the whole chain.
	wantOAuthError(t, e.refreshTokens(client, refresh), http.StatusBadRequest, "invalid_grant")
	eq(t, e.mcpPost(access2, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`).Status, http.StatusUnauthorized, "reuse revoked the chain")
	wantOAuthError(t, e.refreshTokens(client, refresh3), http.StatusBadRequest, "invalid_grant")

	// Revocation (RFC 7009): revoking the refresh token ends its access token too.
	p = newPKCE()
	req.pkce, req.state = p, "st-3"
	back = location(t, ada.do("GET", e.authorizePath(req), nil))
	r = e.exchange(client, back.Query().Get("code"), req.redirect, p.verifier)
	access, refresh = str(r.JSON(), "access_token"), str(r.JSON(), "refresh_token")
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"ping"}`).Status, http.StatusOK, "live token")
	r = e.postForm("/oauth/revoke", url.Values{"client_id": {other}, "token": {refresh}})
	eq(t, r.Status, http.StatusOK, "another client's token: 200, untouched")
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"ping"}`).Status, http.StatusOK, "still live")
	r = e.postForm("/oauth/revoke", url.Values{"client_id": {client}, "token": {refresh}, "token_type_hint": {"refresh_token"}})
	eq(t, r.Status, http.StatusOK, "revoke")
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"ping"}`).Status, http.StatusUnauthorized, "revoked")
	eq(t, e.postForm("/oauth/revoke", url.Values{"client_id": {client}, "token": {"wfm_ort_unknown"}}).Status, http.StatusOK, "unknown token: 200")

	// An expired code is refused.
	p = newPKCE()
	req.pkce = p
	back = location(t, ada.do("GET", e.authorizePath(req), nil))
	e.exec(`UPDATE oauth_codes SET expires_at = now() - interval '1 second' WHERE used_at IS NULL`)
	wantOAuthError(t, e.exchange(client, back.Query().Get("code"), req.redirect, p.verifier), http.StatusBadRequest, "invalid_grant")

	// Omitting the resource defaults to /mcp (the only audience this server issues).
	p = newPKCE()
	back = location(t, ada.do("GET", e.authorizePath(authzReq{client: client, redirect: redirect, state: "s", pkce: p, resource: "-"}), nil))
	r = e.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {back.Query().Get("code")}, "redirect_uri": {redirect}, "code_verifier": {p.verifier}})
	eq(t, r.Status, http.StatusOK, "no resource: "+string(r.Body))
	access = str(r.JSON(), "access_token")
	mustCall(t, e.mcpSession(t, access), "list_projects", obj{})

	// Profile → Connected apps lists the app; disconnecting revokes its tokens.
	var apps []obj
	ada.get("/api/v1/me/connected-apps", &apps)
	eq(t, len(apps), 1, "one connected app")
	eq(t, str(apps[0], "client", "name"), "Claude", "connected app name")
	eq(t, fmt.Sprint(apps[0]["scopes"]), "[mcp:read mcp:write]", "connected app scopes")
	if apps[0]["lastUsedAt"] == nil {
		t.Fatal("last used is recorded")
	}
	bob := e.login("bob@example.com", "Bob")
	r = bob.do("DELETE", "/api/v1/me/connected-apps/"+str(apps[0], "id"), nil)
	eq(t, r.Status, http.StatusNotFound, "someone else's app")
	r = bob.do("GET", "/api/v1/admin/connected-apps", nil)
	eq(t, r.Status, http.StatusForbidden, "admin list needs admin")
	ada.must("DELETE", "/api/v1/me/connected-apps/"+str(apps[0], "id"), nil, 204, nil)
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"ping"}`).Status, http.StatusUnauthorized, "disconnected app's token")
	ada.get("/api/v1/me/connected-apps", &apps)
	eq(t, len(apps), 0, "disconnected")
	// Consent is forgotten: the page shows again.
	to = location(t, ada.do("GET", e.authorizePath(authzReq{client: client, redirect: redirect, state: "s", pkce: newPKCE()}), nil))
	eq(t, consentRequest(to) != "", true, "consent asked again after disconnecting")

	// Grants and revocations are audited.
	var audit []obj
	ada.get("/api/v1/admin/audit?action=oauth.", &audit)
	var actions []string
	for _, a := range audit {
		actions = append(actions, str(a, "action"))
	}
	for _, want := range []string{"oauth.grant", "oauth.code_replay", "oauth.refresh_reuse", "oauth.token_revoke", "oauth.revoke"} {
		if !slices.Contains(actions, want) {
			t.Fatalf("audit %s missing in %v", want, actions)
		}
	}
}

func TestOAuthReadOnlyAndAdmin(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada") // first user: organisation admin
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("RO", "kanban")
	ada.must("POST", "/api/v1/projects/RO/members", obj{"userId": bob.id(), "roleKey": "member"}, 201, nil)
	redirect := "https://chat.example/oauth/callback"
	client := e.register("Chat", redirect)

	// Bob chooses read only although the app asked for read and write.
	p := newPKCE()
	req := authzReq{client: client, redirect: redirect, state: "s", pkce: p, scope: "mcp:read mcp:write"}
	r := bob.do("POST", "/api/v1/oauth/consent", obj{"request": "x", "approve": true, "scopes": []string{"mcp:read"}})
	eq(t, r.Status, http.StatusBadRequest, "bad request blob")
	back := bob.consent(req, true, "mcp:read")
	r = e.exchange(client, back.Query().Get("code"), redirect, p.verifier)
	eq(t, str(r.JSON(), "scope"), "mcp:read", "read only granted")
	access := str(r.JSON(), "access_token")

	// Only read tools are listed; calling a write tool anyway is a 403 step-up challenge.
	cs := e.mcpSession(t, access)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if slices.Contains(names, "create_issue") || !slices.Contains(names, "search_issues") {
		t.Fatalf("read-only tools: %v", names)
	}
	wantContains(t, mustCall(t, cs, "whoami", obj{}), "Token scopes: read", "Connected app: Chat")
	r = e.mcpPost(access, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_issue","arguments":{"projectKey":"RO","type":"task","summary":"x"}}}`)
	eq(t, r.Status, http.StatusForbidden, "write tool with read-only grant")
	wantContains(t, r.Header.Get("WWW-Authenticate"), `error="insufficient_scope"`, `scope="mcp:read mcp:write"`, "resource_metadata=")
	r = e.mcpPost(access, `[{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add_comment","arguments":{}}}]`)
	eq(t, r.Status, http.StatusForbidden, "batched write call")

	// Asking for read only is remembered; asking for more shows the consent page again.
	p2 := newPKCE()
	back = location(t, bob.do("GET", e.authorizePath(authzReq{client: client, redirect: redirect, state: "s", pkce: p2, scope: "mcp:read"}), nil))
	eq(t, back.Host, "chat.example", "read-only consent remembered")
	to := location(t, bob.do("GET", e.authorizePath(req), nil))
	eq(t, consentRequest(to) != "", true, "more scope needs consent")
	// Upgrading to write changes the grant (and is audited).
	p3 := newPKCE()
	req.pkce = p3
	back = bob.consent(req, true, "mcp:write")
	r = e.exchange(client, back.Query().Get("code"), redirect, p3.verifier)
	eq(t, str(r.JSON(), "scope"), "mcp:read mcp:write", "write implies read")
	mustCall(t, e.mcpSession(t, str(r.JSON(), "access_token")), "create_issue", obj{"projectKey": "RO", "type": "task", "summary": "Now allowed"})
	// Scopes outside the request cannot be granted.
	p4 := newPKCE()
	to = location(t, bob.do("GET", e.authorizePath(authzReq{client: client, redirect: redirect, state: "s", pkce: p4, scope: "mcp:read", extra: url.Values{"prompt": {"consent"}}}), nil))
	r = bob.do("POST", "/api/v1/oauth/consent", obj{"request": consentRequest(to), "approve": true, "scopes": []string{"mcp:write"}})
	eq(t, r.Status, http.StatusBadRequest, "cannot grant more than requested")

	// Admins see and revoke everyone's connected apps.
	var all []obj
	ada.get("/api/v1/admin/connected-apps", &all)
	eq(t, len(all), 1, "admin list")
	eq(t, str(all[0], "user", "email"), "bob@example.com", "owner shown")
	ada.must("DELETE", "/api/v1/admin/connected-apps/"+str(all[0], "id"), nil, 204, nil)
	r = e.mcpPost(str(r.JSON(), "access_token"), `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	var mine []obj
	bob.get("/api/v1/me/connected-apps", &mine)
	eq(t, len(mine), 0, "revoked by an admin")

	// A deactivated person's tokens stop working.
	p5 := newPKCE()
	req.pkce = p5
	back = bob.consent(req, true, "mcp:read")
	access = str(e.exchange(client, back.Query().Get("code"), redirect, p5.verifier).JSON(), "access_token")
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"ping"}`).Status, http.StatusOK, "live")
	e.exec(`UPDATE users SET active = false WHERE email = 'bob@example.com'`)
	eq(t, e.mcpPost(access, `{"jsonrpc":"2.0","id":1,"method":"ping"}`).Status, http.StatusUnauthorized, "inactive owner")
}

// TestOAuthClientMetadataDocument identifies the client by an https URL serving its metadata.
func TestOAuthClientMetadataDocument(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	docs := map[string]string{}
	var fetches int
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		if r.URL.Path == "/moved.json" {
			http.Redirect(w, r, "/client.json", http.StatusFound)
			return
		}
		doc, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = io.WriteString(w, doc)
	}))
	defer site.Close()
	// The test site has a self-signed certificate on 127.0.0.1; production fetches go through
	// the SSRF guard (see the oauth package's tests).
	hc := site.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	e.App.OAuth.HTTPClient = hc

	id := site.URL + "/client.json"
	docs["/client.json"] = fmt.Sprintf(`{"client_id":%q,"client_name":"Claude Code","client_uri":"https://claude.example","redirect_uris":["http://localhost/callback","http://127.0.0.1/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`, id)
	docs["/wrong-id.json"] = fmt.Sprintf(`{"client_id":%q,"client_name":"Impostor","redirect_uris":["https://evil.example/cb"]}`, id)
	docs["/secret.json"] = fmt.Sprintf(`{"client_id":%q,"client_name":"X","redirect_uris":["https://x.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, site.URL+"/secret.json")
	docs["/notjson.json"] = `<html>`

	p := newPKCE()
	for _, bad := range []string{site.URL + "/wrong-id.json", site.URL + "/secret.json", site.URL + "/notjson.json", site.URL + "/missing.json", site.URL + "/moved.json", site.URL + "/"} {
		to := location(t, ada.do("GET", e.authorizePath(authzReq{client: bad, redirect: "https://evil.example/cb", state: "s", pkce: p}), nil))
		if to.Path != "/oauth/consent" || !strings.HasPrefix(to.Query().Get("error"), "invalid_client") {
			t.Fatalf("%s: %s", bad, to)
		}
	}

	req := authzReq{client: id, redirect: "http://localhost:4567/callback", state: "s", pkce: p}
	to := location(t, ada.do("GET", e.authorizePath(req), nil))
	var details obj
	ada.get("/api/v1/oauth/consent?request="+url.QueryEscape(consentRequest(to)), &details)
	eq(t, str(details, "client", "kind"), "metadata", "metadata document client")
	eq(t, str(details, "client", "name"), "Claude Code", "name from the document")
	eq(t, str(details, "client", "host"), "127.0.0.1", "verified host")
	eq(t, str(details, "redirectHost"), "localhost", "redirect host")
	n := fetches
	back := ada.consent(req, true, "mcp:read", "mcp:write")
	eq(t, fetches, n, "cached document reused within its max-age")
	r := e.exchange(id, back.Query().Get("code"), req.redirect, p.verifier)
	eq(t, r.Status, http.StatusOK, "CIMD exchange: "+string(r.Body))
	wantContains(t, mustCall(t, e.mcpSession(t, str(r.JSON(), "access_token")), "whoami", obj{}), "Connected app: Claude Code")

	// Disabled: https client ids are unknown.
	off := newEnv(t, withEnv("WFM_OAUTH_CLIENT_METADATA_DOCUMENTS", "false"))
	off.App.OAuth.HTTPClient = hc
	to = location(t, off.login("ada@example.com", "Ada").do("GET", off.authorizePath(req), nil))
	eq(t, to.Query().Get("error"), "invalid_client", "CIMD disabled")
}

// TestOAuthGoSDKClient connects with the MCP Go SDK's OAuth client end to end: discovery from
// the 401, dynamic registration (or a metadata document), PKCE, the resource indicator, the
// RFC 9207 issuer check and the token exchange; the "browser" is a signed-in session.
func TestOAuthGoSDKClient(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	ada.createProject("SDK", "kanban")
	redirect := "http://127.0.0.1:43123/callback"
	browser := func(scopes ...string) auth.AuthorizationCodeFetcher {
		return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			u, err := url.Parse(args.URL)
			if err != nil {
				return nil, err
			}
			to := location(t, ada.do("GET", u.RequestURI(), nil))
			if consentRequest(to) != "" {
				var out obj
				ada.must("POST", "/api/v1/oauth/consent", obj{"request": consentRequest(to), "approve": true, "scopes": scopes}, 200, &out)
				if to, err = url.Parse(str(out, "redirectUrl")); err != nil {
					return nil, err
				}
			}
			q := to.Query()
			if q.Get("error") != "" {
				return nil, fmt.Errorf("authorization failed: %s", q.Get("error"))
			}
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		}
	}
	connect := func(h auth.OAuthHandler) *mcp.ClientSession {
		client := mcp.NewClient(&mcp.Implementation{Name: "sdk-oauth-test", Version: "1"}, nil)
		cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.Srv.URL + "/mcp", OAuthHandler: h, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		return cs
	}

	h, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Go SDK", RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"},
		}},
		AuthorizationCodeFetcher: browser("mcp:read", "mcp:write"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cs := connect(h)
	wantContains(t, mustCall(t, cs, "whoami", obj{}), "Ada <ada@example.com>", "Connected app: Go SDK", "read, write")
	wantContains(t, mustCall(t, cs, "create_issue", obj{"projectKey": "SDK", "type": "task", "summary": "Via the Go SDK"}), "Created SDK-1")
	ts, _ := h.TokenSource(context.Background())
	tok, err := ts.Token()
	if err != nil || tok.RefreshToken == "" || time.Until(tok.Expiry) < 50*time.Minute {
		t.Fatalf("token source: %+v %v", tok, err)
	}

	// Read-only grant through the SDK: read tools only, and a write call is refused with 403
	// insufficient_scope (the SDK attempts step-up, which this fetcher answers with read only
	// again, so the call fails).
	ro, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Reader", RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: "none",
		}},
		AuthorizationCodeFetcher: browser("mcp:read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	rcs := connect(ro)
	wantContains(t, mustCall(t, rcs, "whoami", obj{}), "Connected app: Reader", "Token scopes: read")
	if _, err := rcs.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_issue", Arguments: obj{"projectKey": "SDK", "type": "task", "summary": "nope"}}); err == nil {
		t.Fatal("read-only grant called a write tool")
	}

	// Client ID metadata document variant (preferred by the SDK when advertised).
	site := httptest.NewTLSServer(nil)
	defer site.Close()
	docURL := site.URL + "/oauth/client.json"
	site.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(obj{"client_id": docURL, "client_name": "SDK via CIMD", "redirect_uris": []string{"http://127.0.0.1/callback"}, "token_endpoint_auth_method": "none"})
	})
	hc := site.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	e.App.OAuth.HTTPClient = hc
	ch, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: docURL},
		RedirectURL:                    redirect,
		AuthorizationCodeFetcher:       browser("mcp:read", "mcp:write"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, mustCall(t, connect(ch), "whoami", obj{}), "Connected app: SDK via CIMD")
}
