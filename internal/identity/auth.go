package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/config"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/ratelimit"
	"github.com/workflowproj/workflow/internal/secure"
	"github.com/workflowproj/workflow/internal/tracker"
)

const (
	SessionCookie = "wfm_session"
	stateCookie   = "wfm_oidc"
	TokenPrefix   = "wfm_pat_"
)

// Auth authenticates requests and runs the OIDC sign-in flow.
type Auth struct {
	Cfg   *config.Config
	Q     *dbq.Queries
	Prov  *Provisioner
	Box   *secure.Box
	Log   *slog.Logger
	Track *tracker.Service
	// HTTPClient is used for OIDC discovery and token exchange (tests point it at a fake IdP).
	HTTPClient *http.Client

	mu        sync.Mutex
	providers map[string]*oidcProvider
}

type oidcProvider struct {
	cfg      config.OIDCProvider
	provider *oidc.Provider
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func (a *Auth) secureCookies() bool { return strings.HasPrefix(a.Cfg.PublicURL, "https://") }

func (a *Auth) ctx(ctx context.Context) context.Context {
	if a.HTTPClient != nil {
		return oidc.ClientContext(ctx, a.HTTPClient)
	}
	return ctx
}

// provider lazily performs OIDC discovery so a slow IdP never blocks startup.
func (a *Auth) provider(ctx context.Context, id string) (*oidcProvider, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p, ok := a.providers[id]; ok {
		return p, nil
	}
	idx := slices.IndexFunc(a.Cfg.OIDCProviders, func(p config.OIDCProvider) bool { return p.ID == id })
	if idx < 0 {
		return nil, apperr.NotFound("sign-in provider")
	}
	pc := a.Cfg.OIDCProviders[idx]
	prov, err := oidc.NewProvider(a.ctx(ctx), pc.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", id, err)
	}
	p := &oidcProvider{
		cfg:      pc,
		provider: prov,
		oauth: oauth2.Config{
			ClientID: pc.ClientID, ClientSecret: pc.ClientSecret, Endpoint: prov.Endpoint(), Scopes: pc.Scopes,
			RedirectURL: fmt.Sprintf("%s/auth/%s/callback", a.Cfg.PublicURL, id),
		},
		verifier: prov.Verifier(&oidc.Config{ClientID: pc.ClientID}),
	}
	if a.providers == nil {
		a.providers = map[string]*oidcProvider{}
	}
	a.providers[id] = p
	return p, nil
}

type oidcState struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
	Expires  int64  `json:"e"`
}

var safePath = regexp.MustCompile(`^/[^/\\]`)

// SafeReturnTo only allows same-origin relative paths. Control characters are refused because
// browsers strip tabs and newlines from URLs, which would turn "/\t/evil.example" into "//evil.example".
func SafeReturnTo(s string) string {
	if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "/"
	}
	if u, err := url.Parse(s); err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	if s == "/" || safePath.MatchString(s) {
		return s
	}
	return "/"
}

// HandleLogin starts the authorisation code flow with PKCE: GET /auth/{provider}/login.
func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	p, err := a.provider(r.Context(), id)
	if err != nil {
		a.Log.Error("oidc provider unavailable", "provider", id, "error", err)
		http.Redirect(w, r, "/login?error=provider_unavailable", http.StatusFound)
		return
	}
	st := oidcState{
		Provider: id, State: secure.Token("", 16), Nonce: secure.Token("", 16), Verifier: oauth2.GenerateVerifier(),
		ReturnTo: SafeReturnTo(r.URL.Query().Get("returnTo")), Expires: time.Now().Add(10 * time.Minute).Unix(),
	}
	raw, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: a.Box.Sign(base64.RawURLEncoding.EncodeToString(raw)), Path: "/auth/",
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(st.Verifier), oidc.Nonce(st.Nonce), oauth2.SetAuthURLParam("prompt", "select_account")}
	if p.cfg.IsGoogle() && len(p.cfg.AllowedDomains) == 1 {
		opts = append(opts, oauth2.SetAuthURLParam("hd", p.cfg.AllowedDomains[0]))
	}
	http.Redirect(w, r, p.oauth.AuthCodeURL(st.State, opts...), http.StatusFound)
}

// HandleCallback completes sign-in: GET /auth/{provider}/callback.
func (a *Auth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(code string, err error) {
		if err != nil {
			a.Log.Warn("sign-in failed", "code", code, "error", err)
		}
		http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth/", MaxAge: -1})
		http.Redirect(w, r, "/login?error="+code, http.StatusFound)
	}
	id := r.PathValue("provider")
	if e := r.URL.Query().Get("error"); e != "" {
		fail("access_denied", nil)
		return
	}
	c, err := r.Cookie(stateCookie)
	if err != nil {
		fail("invalid_state", err)
		return
	}
	payload, ok := a.Box.Verify(c.Value)
	if !ok {
		fail("invalid_state", errors.New("bad signature"))
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	var st oidcState
	if err != nil || json.Unmarshal(raw, &st) != nil || st.Provider != id || time.Now().Unix() > st.Expires ||
		!secure.Equal(st.State, r.URL.Query().Get("state")) {
		fail("invalid_state", nil)
		return
	}
	p, err := a.provider(r.Context(), id)
	if err != nil {
		fail("provider_unavailable", err)
		return
	}
	ctx := a.ctx(r.Context())
	tok, err := p.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		fail("exchange_failed", err)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idTok, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		fail("invalid_token", err)
		return
	}
	if !secure.Equal(idTok.Nonce, st.Nonce) {
		fail("invalid_token", errors.New("nonce mismatch"))
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
		GivenName     string `json:"given_name"`
		FamilyName    string `json:"family_name"`
		Picture       string `json:"picture"`
		HD            string `json:"hd"`
	}
	var all map[string]any
	if err := idTok.Claims(&claims); err != nil || idTok.Claims(&all) != nil {
		fail("invalid_token", err)
		return
	}
	verified := claims.EmailVerified == true || claims.EmailVerified == "true"
	user, err := a.Prov.SignIn(r.Context(), Claims{
		Provider: id, Subject: idTok.Subject, Email: claims.Email, EmailVerified: verified, Name: claims.Name,
		GivenName: claims.GivenName, FamilyName: claims.FamilyName, Picture: claims.Picture, HostedDomain: strings.ToLower(claims.HD),
		IsGoogle: p.cfg.IsGoogle(),
	}, p.cfg.AllowedDomains)
	var le *LoginError
	if errors.As(err, &le) {
		fail(le.Code, nil)
		return
	}
	if err != nil {
		fail("server_error", err)
		return
	}
	// Groups come from the token's claim, unless Keycloak directory sync already brings this
	// provider's groups (one source of truth, and no duplicate of every group).
	kc := a.Cfg.Keycloak
	if len(p.cfg.GroupsClaim) > 0 && !(kc.Enabled && kc.SyncGroups && kc.Provider == id) {
		groups := claimGroups(all, p.cfg.GroupsClaim, p.cfg.GroupsPrefix)
		if err := a.Prov.SyncClaimGroups(r.Context(), user.ID, id, groups); err != nil {
			fail("server_error", err)
			return
		}
	}
	sid, _ := all["sid"].(string)
	if err := a.StartSessionIDP(w, r, user, idpSession{Provider: id, Subject: idTok.Subject, SID: sid, IDToken: rawID}); err != nil {
		fail("server_error", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth/", MaxAge: -1})
	tracker.Audit(r.Context(), a.Q, authz.UserActor(user, user.OrgRole), "user.login", "user", user.ID.String(), nil, map[string]any{"provider": id})
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
}

// StartSession creates a server-side session and sets the cookie.
func (a *Auth) StartSession(w http.ResponseWriter, r *http.Request, user dbq.User) error {
	return a.startSession(w, r, user, dbq.CreateSessionParams{})
}

// startSession creates the session row (with any IdP details in p) and sets the cookie.
func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, user dbq.User, p dbq.CreateSessionParams) error {
	token := secure.Token("", 32)
	expires := time.Now().Add(a.Cfg.SessionTTL)
	ua := r.UserAgent()
	if len(ua) > 400 {
		ua = ua[:400]
	}
	ip := clientIP(r)
	p.ID, p.UserID, p.ExpiresAt, p.Ip, p.UserAgent = secure.Hash(token), user.ID, expires, &ip, &ua
	if err := a.Q.CreateSession(r.Context(), p); err != nil {
		return err
	}
	if err := a.Q.TouchUserLogin(r.Context(), user.ID); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: a.secureCookies(),
		SameSite: http.SameSiteLaxMode, Expires: expires,
	})
	return nil
}

func (a *Auth) EndSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = a.Q.DeleteSession(r.Context(), secure.Hash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode})
}

// clientIP is the client address the HTTP layer resolved (X-Forwarded-For counts only from
// trusted proxies), else the connection's peer address.
func clientIP(r *http.Request) string {
	if ip := ratelimit.ClientIPFrom(r.Context()); ip != "" {
		return ip
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}

type ctxKey struct{}

// ActorFrom returns the authenticated actor, or nil.
func ActorFrom(ctx context.Context) *authz.Actor {
	a, _ := ctx.Value(ctxKey{}).(*authz.Actor)
	return a
}

func WithActor(ctx context.Context, a *authz.Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// Authenticate resolves a bearer token or session cookie into an actor (nil if anonymous).
// Bearer tokens are personal access tokens, or access tokens from an OIDC provider that has an
// API audience configured (authenticateJWT).
func (a *Auth) Authenticate(r *http.Request) (*authz.Actor, error) {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if !strings.HasPrefix(token, TokenPrefix) && looksLikeJWT(token) {
			return a.authenticateJWT(ctx, token)
		}
		row, err := a.Q.GetAPITokenUser(ctx, secure.Hash(token))
		if err != nil {
			return nil, apperr.Unauthorized()
		}
		role, err := a.Track.EffectiveOrgRole(ctx, row.User)
		if err != nil {
			return nil, err
		}
		actor := authz.UserActor(row.User, role)
		actor.TokenID = &row.ApiToken.ID
		actor.TokenName = row.ApiToken.Name
		actor.Scopes = row.ApiToken.Scopes
		if row.ApiToken.LastUsedAt == nil || time.Since(*row.ApiToken.LastUsedAt) > time.Minute {
			_ = a.Q.TouchAPIToken(ctx, row.ApiToken.ID)
		}
		return actor, nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	id := secure.Hash(c.Value)
	row, err := a.Q.GetSessionUser(ctx, id)
	if err != nil {
		return nil, nil
	}
	if time.Since(row.SessionLastSeen) > 5*time.Minute {
		_ = a.Q.TouchSession(ctx, id)
	}
	role, err := a.Track.EffectiveOrgRole(ctx, row.User)
	if err != nil {
		return nil, err
	}
	return authz.UserActor(row.User, role), nil
}

// CheckScope enforces token scopes: read = safe methods, write = everything except
// administration, admin = everything (still subject to the owner's own role).
func CheckScope(actor *authz.Actor, method, path string) error {
	if !actor.ViaToken() {
		return nil
	}
	has := func(s string) bool { return slices.Contains(actor.Scopes, s) }
	if strings.HasPrefix(path, "/api/v1/admin/") && !has("admin") {
		return apperr.Forbidden("this token lacks the admin scope")
	}
	safe := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	if !safe && !has("write") && !has("admin") {
		return apperr.Forbidden("this token is read-only")
	}
	return nil
}

// CreateToken issues a personal access token or service-account token.
func (a *Auth) CreateToken(ctx context.Context, q *dbq.Queries, owner uuid.UUID, createdBy *uuid.UUID, name string, scopes []string, days int) (dbq.ApiToken, string, error) {
	if strings.TrimSpace(name) == "" {
		return dbq.ApiToken{}, "", apperr.Invalid("name", "name is required")
	}
	if days < 1 || days > 366 {
		return dbq.ApiToken{}, "", apperr.Invalid("expiresInDays", "tokens expire after 1 to 366 days")
	}
	if len(scopes) == 0 {
		return dbq.ApiToken{}, "", apperr.Invalid("scopes", "at least one scope is required")
	}
	for _, s := range scopes {
		if !slices.Contains([]string{"read", "write", "admin"}, s) {
			return dbq.ApiToken{}, "", apperr.Invalid("scopes", "unknown scope %q", s)
		}
	}
	token := secure.Token(TokenPrefix, 32)
	t, err := q.CreateAPIToken(ctx, dbq.CreateAPITokenParams{
		UserID: owner, Name: strings.TrimSpace(name), TokenHash: secure.Hash(token), TokenPrefix: token[:len(TokenPrefix)+6],
		Scopes: scopes, ExpiresAt: time.Now().AddDate(0, 0, days), CreatedBy: createdBy,
	})
	return t, token, err
}
