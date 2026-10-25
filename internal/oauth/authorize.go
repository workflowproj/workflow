package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/secure"
)

// ConsentPath is the web app's consent page: ConsentPath/<sealed request> asks the person,
// ConsentPath?error=<code> explains why an app could not be connected. The request travels in
// the path so it survives the sign-in page's returnTo unchanged.
const ConsentPath = "/oauth/consent"

// maxState bounds the client's state value, which travels inside the consent page's URL.
const maxState = 512

// pkceRE matches an S256 code challenge or a code verifier (RFC 7636 §4.1–4.2).
var pkceRE = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// pending is an authorisation request waiting for the person's decision. It travels to the
// consent page and back sealed (encrypted and authenticated with WFM_SECRET_KEY), so it cannot
// be read or altered and nothing is stored until the person decides.
type pending struct {
	Kind      string   `json:"k"`
	ClientID  string   `json:"c"`
	Redirect  string   `json:"r"`
	State     string   `json:"s"`
	Challenge string   `json:"p"`
	Resource  string   `json:"a"`
	Scopes    []string `json:"o"`
	Expires   int64    `json:"e"`
}

const pendingKind = "oauth-authorize"

func (s *Server) seal(p pending) string {
	p.Kind = pendingKind
	raw, _ := json.Marshal(p)
	return s.Box.Seal(raw)
}

func (s *Server) open(sealed string) (pending, error) {
	var p pending
	raw, err := s.Box.Open(sealed)
	if err != nil || json.Unmarshal(raw, &p) != nil || p.Kind != pendingKind {
		return p, apperr.New(http.StatusBadRequest, "invalid_request", "this authorisation request is not valid; start connecting again from the app")
	}
	if time.Now().Unix() > p.Expires {
		return p, apperr.New(http.StatusBadRequest, "expired", "this authorisation request has expired; start connecting again from the app")
	}
	return p, nil
}

// authorize is the authorisation endpoint: GET /oauth/authorize.
//
// Until the client and redirect URI are validated, errors are shown on Workflow's own page and
// never sent to the redirect URI; after that they go back to the client (RFC 6749 §4.1.2.1).
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "use GET", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	page := func(code string) {
		http.Redirect(w, r, ConsentPath+"?error="+url.QueryEscape(code), http.StatusFound)
	}
	for _, single := range []string{"client_id", "redirect_uri", "response_type", "state", "code_challenge", "code_challenge_method", "scope"} {
		if len(q[single]) > 1 {
			page("invalid_request")
			return
		}
	}
	client, err := s.client(ctx, q.Get("client_id"), r, true)
	if errors.Is(err, errClientDocument) {
		page("invalid_client_metadata")
		return
	}
	if err != nil {
		if !errors.Is(err, errUnknownClient) {
			s.Log.Error("oauth client lookup failed", "error", err)
		}
		page("invalid_client")
		return
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" && len(client.RedirectUris) == 1 && !isLoopbackRedirect(client.RedirectUris[0]) {
		redirect = client.RedirectUris[0]
	}
	if redirect == "" || !redirectMatches(client.RedirectUris, redirect) {
		page("invalid_redirect_uri")
		return
	}

	state := q.Get("state")
	fail := func(code, desc string) {
		http.Redirect(w, r, s.errorRedirect(redirect, state, code, desc), http.StatusFound)
	}
	switch {
	case q.Get("response_type") != "code":
		fail("unsupported_response_type", "only response_type=code is supported")
		return
	case state == "":
		fail("invalid_request", "state is required")
		return
	case len(state) > maxState:
		fail("invalid_request", "state is too long")
		return
	case q.Get("code_challenge") == "":
		fail("invalid_request", "PKCE is required: send code_challenge and code_challenge_method=S256")
		return
	case q.Get("code_challenge_method") != "S256":
		fail("invalid_request", "code_challenge_method must be S256")
		return
	case !pkceRE.MatchString(q.Get("code_challenge")):
		fail("invalid_request", "code_challenge is not a valid S256 challenge")
		return
	}
	// RFC 8707: tokens are only ever issued for the MCP endpoint. A client that omits the
	// resource gets that audience; one that names another resource is refused.
	if res := q["resource"]; len(res) > 1 || (len(res) == 1 && !s.resourceMatches(res[0])) {
		fail("invalid_target", "this server issues tokens for "+s.Resource()+" only")
		return
	}
	p := pending{
		ClientID: client.ID, Redirect: redirect, State: state, Challenge: q.Get("code_challenge"),
		Resource: s.Resource(), Scopes: parseScopes(q.Get("scope")), Expires: time.Now().Add(requestTTL).Unix(),
	}

	prompt := q.Get("prompt")
	user := s.sessionUser(r)
	if user != nil && prompt != "consent" && prompt != "login" {
		// Returning people skip the consent page while their grant covers what is asked.
		grant, err := s.Q.GetActiveOAuthGrant(ctx, dbq.GetActiveOAuthGrantParams{UserID: user.User.ID, ClientID: client.ID})
		if err == nil && covers(grant.Scopes, p.Scopes) {
			to, err := s.issueCode(ctx, s.Q, grant, p)
			if err != nil {
				s.Log.Error("oauth code issue failed", "error", err)
				fail("server_error", "could not complete the authorisation")
				return
			}
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
	}
	if prompt == "none" {
		if user == nil {
			fail("login_required", "sign in to Workflow first")
		} else {
			fail("consent_required", "approve access to Workflow first")
		}
		return
	}
	http.Redirect(w, r, ConsentPath+"/"+url.PathEscape(s.seal(p)), http.StatusFound)
}

// errorRedirect builds an error response to a validated redirect URI.
func (s *Server) errorRedirect(redirect, state, code, desc string) string {
	v := url.Values{"error": {code}, "iss": {s.Issuer()}}
	if desc != "" {
		v.Set("error_description", desc)
	}
	if state != "" {
		v.Set("state", state)
	}
	return withQuery(redirect, v)
}

// withQuery adds parameters to a redirect URI, keeping any query it already has.
func withQuery(redirect string, v url.Values) string {
	u, err := url.Parse(redirect)
	if err != nil {
		return redirect
	}
	q := u.Query()
	for k, vs := range v {
		q[k] = vs
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// issueCode stores a single-use authorisation code for the grant and returns the redirect that
// delivers it, with the state and the issuer (RFC 9207).
func (s *Server) issueCode(ctx context.Context, q *dbq.Queries, grant dbq.OauthGrant, p pending) (string, error) {
	code := secure.Token(codePrefix, 32)
	if err := q.CreateOAuthCode(ctx, dbq.CreateOAuthCodeParams{
		CodeHash: secure.Hash(code), GrantID: grant.ID, RedirectUri: p.Redirect, CodeChallenge: p.Challenge,
		Resource: p.Resource, Scopes: intersect(p.Scopes, grant.Scopes), ExpiresAt: time.Now().Add(codeTTL),
	}); err != nil {
		return "", err
	}
	return withQuery(p.Redirect, url.Values{"code": {code}, "state": {p.State}, "iss": {s.Issuer()}}), nil
}

// ---------------------------------------------------------------------------
// The consent page's API

// Consent describes a pending request for the consent page.
type Consent struct {
	Client      dbq.OauthClient
	RedirectURI string
	// Requested are the scopes the client asked for; Granted those the person already allowed
	// (nil when they have not connected this client).
	Requested []string
	Granted   []string
}

// RedirectHost is the host (or app scheme) the person will be sent to.
func (c *Consent) RedirectHost() string { return redirectHost(c.RedirectURI) }

// NativeRedirect reports a redirect to an app on the person's computer.
func (c *Consent) NativeRedirect() bool { return isNativeRedirect(c.RedirectURI) }

// AlreadyGranted reports that an earlier approval covers this request.
func (c *Consent) AlreadyGranted() bool { return c.Granted != nil && covers(c.Granted, c.Requested) }

func sessionOnly(a *authz.Actor) error {
	if a == nil || a.User == nil {
		return apperr.Unauthorized()
	}
	if a.ViaToken() {
		return apperr.Forbidden("connected apps can only be approved in the browser")
	}
	return nil
}

// pendingClient opens a sealed request and loads its client, re-checking the redirect URI.
func (s *Server) pendingClient(ctx context.Context, sealed string) (pending, dbq.OauthClient, error) {
	if !s.Enabled() {
		return pending{}, dbq.OauthClient{}, apperr.NotFound("route")
	}
	p, err := s.open(sealed)
	if err != nil {
		return p, dbq.OauthClient{}, err
	}
	c, err := s.client(ctx, p.ClientID, nil, false)
	if err != nil || !redirectMatches(c.RedirectUris, p.Redirect) {
		return p, c, apperr.New(http.StatusBadRequest, "invalid_client", "the app asking for access is no longer registered; connect again from the app")
	}
	return p, c, nil
}

// Consent returns what the consent page shows for a sealed request.
func (s *Server) Consent(ctx context.Context, a *authz.Actor, sealed string) (*Consent, error) {
	if err := sessionOnly(a); err != nil {
		return nil, err
	}
	p, c, err := s.pendingClient(ctx, sealed)
	if err != nil {
		return nil, err
	}
	out := &Consent{Client: c, RedirectURI: p.Redirect, Requested: p.Scopes}
	if g, err := s.Q.GetActiveOAuthGrant(ctx, dbq.GetActiveOAuthGrantParams{UserID: a.User.ID, ClientID: c.ID}); err == nil {
		out.Granted = g.Scopes
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// Decide records the person's answer and returns where to send the browser: the client's
// redirect URI with a code, or with error=access_denied. Approving grants the chosen scopes,
// which must be among those requested (write includes read).
func (s *Server) Decide(ctx context.Context, a *authz.Actor, sealed string, approve bool, scopes []string) (string, error) {
	if err := sessionOnly(a); err != nil {
		return "", err
	}
	p, c, err := s.pendingClient(ctx, sealed)
	if err != nil {
		return "", err
	}
	if !approve {
		return s.errorRedirect(p.Redirect, p.State, "access_denied", "the person declined to connect "+c.Name), nil
	}
	chosen := normalizeScopes(scopes)
	if len(scopes) == 0 || !covers(p.Scopes, chosen) {
		return "", apperr.Invalid("scopes", "choose read-only access or read and write access")
	}
	p.Scopes = chosen
	var to string
	err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		grant, err := q.GetActiveOAuthGrant(ctx, dbq.GetActiveOAuthGrantParams{UserID: a.User.ID, ClientID: c.ID})
		var action string
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			grant, err = q.CreateOAuthGrant(ctx, dbq.CreateOAuthGrantParams{UserID: a.User.ID, ClientID: c.ID, Scopes: chosen})
			action = "oauth.grant"
		case err == nil && !slices.Equal(grant.Scopes, chosen):
			grant, err = q.SetOAuthGrantScopes(ctx, dbq.SetOAuthGrantScopesParams{ID: grant.ID, Scopes: chosen})
			action = "oauth.grant_change"
		}
		if err != nil {
			return err
		}
		if action != "" {
			audit(ctx, q, a, action, c, grant.ID.String(), map[string]any{"scopes": chosen})
		}
		to, err = s.issueCode(ctx, q, grant, p)
		return err
	})
	return to, err
}
