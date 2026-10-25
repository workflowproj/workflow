package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// tokenError is an RFC 6749 §5.2 error from the token or revocation endpoint.
type tokenError struct {
	status int
	code   string
	desc   string
}

func (e *tokenError) Error() string { return e.code + ": " + e.desc }

func invalidGrant(desc string) *tokenError {
	return &tokenError{http.StatusBadRequest, "invalid_grant", desc}
}

func invalidRequest(desc string) *tokenError {
	return &tokenError{http.StatusBadRequest, "invalid_request", desc}
}

func (s *Server) writeTokenError(w http.ResponseWriter, err error) {
	var te *tokenError
	if !errors.As(err, &te) {
		s.Log.Error("oauth token endpoint failed", "error", err)
		te = &tokenError{http.StatusInternalServerError, "server_error", "the request could not be completed"}
	}
	if te.code == "invalid_client" {
		w.Header().Set("WWW-Authenticate", `Basic realm="workflow"`)
	}
	writeError(w, te.status, te.code, te.desc)
}

func parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		return invalidRequest("send the parameters as application/x-www-form-urlencoded")
	}
	for k, v := range r.PostForm {
		if len(v) > 1 {
			return invalidRequest("parameter " + k + " is repeated")
		}
	}
	return nil
}

// authenticateClient identifies the client at the token and revocation endpoints. Public
// clients send their client_id; confidential (dynamically registered) clients also prove their
// secret with HTTP Basic or client_secret in the body.
func (s *Server) authenticateClient(r *http.Request) (dbq.OauthClient, error) {
	id, secret, basic := r.BasicAuth()
	if basic {
		// RFC 6749 §2.3.1: the credentials are form-encoded before Basic encoding.
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
		if form := r.PostForm.Get("client_id"); form != "" && form != id {
			return dbq.OauthClient{}, invalidRequest("client_id does not match the Authorization header")
		}
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	bad := &tokenError{http.StatusUnauthorized, "invalid_client", "unknown client or wrong client credentials"}
	c, err := s.client(r.Context(), id, r, false)
	if err != nil {
		return c, bad
	}
	if c.TokenAuthMethod != "none" && (secret == "" || c.SecretHash == nil || !secure.Equal(secure.Hash(secret), *c.SecretHash)) {
		return c, bad
	}
	return c, nil
}

// tokenResponse is a successful token response (RFC 6749 §5.1).
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

// token is the token endpoint: POST /oauth/token.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if limited(w, r, s.tokenLimit) {
		return
	}
	if err := parseForm(w, r); err != nil {
		s.writeTokenError(w, err)
		return
	}
	client, err := s.authenticateClient(r)
	if err != nil {
		s.writeTokenError(w, err)
		return
	}
	if res := r.PostForm.Get("resource"); res != "" && !s.resourceMatches(res) {
		s.writeTokenError(w, &tokenError{http.StatusBadRequest, "invalid_target", "this server issues tokens for " + s.Resource() + " only"})
		return
	}
	var resp *tokenResponse
	switch gt := r.PostForm.Get("grant_type"); gt {
	case "authorization_code":
		resp, err = s.exchangeCode(r.Context(), client, r.PostForm)
	case "refresh_token":
		resp, err = s.refresh(r.Context(), client, r.PostForm)
	case "":
		err = invalidRequest("grant_type is required")
	default:
		err = &tokenError{http.StatusBadRequest, "unsupported_grant_type", "grant type " + gt + " is not supported"}
	}
	if err != nil {
		s.writeTokenError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// exchangeCode redeems an authorisation code. Codes are single use: presenting one again
// revokes the tokens it produced (OAuth 2.1 §4.1.3).
func (s *Server) exchangeCode(ctx context.Context, client dbq.OauthClient, f url.Values) (*tokenResponse, error) {
	code, verifier := f.Get("code"), f.Get("code_verifier")
	if code == "" {
		return nil, invalidRequest("code is required")
	}
	if verifier == "" {
		return nil, invalidRequest("code_verifier is required (PKCE)")
	}
	var resp *tokenResponse
	var replayed bool
	err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		row, err := q.GetOAuthCode(ctx, secure.Hash(code))
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidGrant("unknown authorisation code")
		}
		if err != nil {
			return err
		}
		c, g := row.OauthCode, row.OauthGrant
		if g.ClientID != client.ID {
			return invalidGrant("the code was issued to another client")
		}
		if c.UsedAt != nil {
			if c.FamilyID != nil {
				n, err := q.RevokeOAuthFamily(ctx, *c.FamilyID)
				if err != nil {
					return err
				}
				s.Log.Warn("oauth code replayed; tokens revoked", "client_id", client.ID, "grant", g.ID, "revoked", n)
				audit(ctx, q, nil, "oauth.code_replay", client, g.ID.String(), map[string]any{"userId": g.UserID, "revokedTokens": n})
			}
			replayed = true
			return nil
		}
		if time.Now().After(c.ExpiresAt) {
			return invalidGrant("the authorisation code has expired")
		}
		if ru := f.Get("redirect_uri"); ru != "" && ru != c.RedirectUri {
			return invalidGrant("redirect_uri does not match the authorisation request")
		}
		if !pkceRE.MatchString(verifier) || !secure.Equal(s256(verifier), c.CodeChallenge) {
			return invalidGrant("code_verifier does not match the code challenge")
		}
		if res := f.Get("resource"); res != "" && canonicalResource(res) != c.Resource {
			return &tokenError{http.StatusBadRequest, "invalid_target", "resource does not match the authorisation request"}
		}
		if g.RevokedAt != nil {
			return invalidGrant("access was revoked")
		}
		family := uuid.New()
		if n, err := q.UseOAuthCode(ctx, dbq.UseOAuthCodeParams{CodeHash: c.CodeHash, FamilyID: &family}); err != nil {
			return err
		} else if n == 0 {
			return invalidGrant("the authorisation code was already used")
		}
		resp, err = s.issueTokens(ctx, q, g, family, intersect(c.Scopes, g.Scopes), c.Resource)
		return err
	})
	if err == nil && replayed {
		// Reported after the transaction commits, so the revocation sticks.
		err = invalidGrant("the authorisation code was already used")
	}
	return resp, err
}

// refresh rotates a refresh token: the old one stops working and a new pair is issued with a
// fresh refresh-token lifetime. Presenting a refresh token that was already rotated means it
// was copied, so the whole family is revoked (OAuth 2.1 §4.3.1, RFC 9700 §4.14.2).
func (s *Server) refresh(ctx context.Context, client dbq.OauthClient, f url.Values) (*tokenResponse, error) {
	raw := f.Get("refresh_token")
	if raw == "" {
		return nil, invalidRequest("refresh_token is required")
	}
	var resp *tokenResponse
	var reused bool
	err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		row, err := q.GetOAuthToken(ctx, secure.Hash(raw))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.OauthToken.Kind != "refresh") {
			return invalidGrant("unknown refresh token")
		}
		if err != nil {
			return err
		}
		t, g := row.OauthToken, row.OauthGrant
		if g.ClientID != client.ID {
			return invalidGrant("the refresh token was issued to another client")
		}
		if t.UsedAt != nil {
			n, err := q.RevokeOAuthFamily(ctx, t.FamilyID)
			if err != nil {
				return err
			}
			s.Log.Warn("oauth refresh token reused; family revoked", "client_id", client.ID, "grant", g.ID, "revoked", n)
			audit(ctx, q, nil, "oauth.refresh_reuse", client, g.ID.String(), map[string]any{"userId": g.UserID, "revokedTokens": n})
			reused = true
			return nil
		}
		if t.RevokedAt != nil || g.RevokedAt != nil || time.Now().After(t.ExpiresAt) {
			return invalidGrant("the refresh token has expired or was revoked")
		}
		scopes := intersect(t.Scopes, g.Scopes)
		if sc := f.Get("scope"); sc != "" {
			asked := parseScopes(sc)
			if !covers(scopes, asked) {
				return &tokenError{http.StatusBadRequest, "invalid_scope", "a refresh cannot add scopes; authorise again"}
			}
			scopes = asked
		}
		if n, err := q.UseOAuthRefreshToken(ctx, t.ID); err != nil {
			return err
		} else if n == 0 {
			return invalidGrant("the refresh token was already used")
		}
		resp, err = s.issueTokens(ctx, q, g, t.FamilyID, scopes, t.Resource)
		return err
	})
	if err == nil && reused {
		// Reported after the transaction commits, so the revocation sticks.
		err = invalidGrant("the refresh token was already used; sign in again")
	}
	return resp, err
}

// issueTokens creates an access token and a refresh token in a family.
func (s *Server) issueTokens(ctx context.Context, q *dbq.Queries, g dbq.OauthGrant, family uuid.UUID, scopes []string, resource string) (*tokenResponse, error) {
	if len(scopes) == 0 {
		return nil, invalidGrant("no access remains on this grant")
	}
	u, err := q.GetUser(ctx, g.UserID)
	if err != nil || !u.Active || u.DeletedAt != nil {
		return nil, invalidGrant("the account is not active")
	}
	access := secure.Token(AccessTokenPrefix, 32)
	refresh := secure.Token(RefreshTokenPrefix, 32)
	now := time.Now()
	for _, t := range []struct {
		kind, raw string
		ttl       time.Duration
	}{{"access", access, s.Cfg.OAuth.AccessTokenTTL}, {"refresh", refresh, s.Cfg.OAuth.RefreshTokenTTL}} {
		if _, err := q.CreateOAuthToken(ctx, dbq.CreateOAuthTokenParams{
			GrantID: g.ID, FamilyID: family, Kind: t.kind, TokenHash: secure.Hash(t.raw), Scopes: scopes,
			Resource: resource, ExpiresAt: now.Add(t.ttl),
		}); err != nil {
			return nil, err
		}
	}
	if err := q.TouchOAuthGrant(ctx, g.ID); err != nil {
		return nil, err
	}
	return &tokenResponse{
		AccessToken: access, TokenType: "Bearer", ExpiresIn: int(s.Cfg.OAuth.AccessTokenTTL.Seconds()),
		RefreshToken: refresh, Scope: strings.Join(scopes, " "),
	}, nil
}

// revoke is the revocation endpoint (RFC 7009): POST /oauth/revoke. Revoking a refresh token
// revokes its whole family, including the access tokens issued with it. Unknown tokens and
// tokens of other clients are answered with 200 and left alone.
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if limited(w, r, s.tokenLimit) {
		return
	}
	if err := parseForm(w, r); err != nil {
		s.writeTokenError(w, err)
		return
	}
	client, err := s.authenticateClient(r)
	if err != nil {
		s.writeTokenError(w, err)
		return
	}
	raw := r.PostForm.Get("token")
	if raw == "" {
		s.writeTokenError(w, invalidRequest("token is required"))
		return
	}
	ctx := r.Context()
	row, err := s.Q.GetOAuthToken(ctx, secure.Hash(raw))
	if err == nil && row.OauthGrant.ClientID == client.ID && row.OauthToken.RevokedAt == nil {
		if row.OauthToken.Kind == "refresh" {
			_, err = s.Q.RevokeOAuthFamily(ctx, row.OauthToken.FamilyID)
		} else {
			err = s.Q.RevokeOAuthToken(ctx, row.OauthToken.ID)
		}
		if err != nil {
			s.writeTokenError(w, err)
			return
		}
		audit(ctx, s.Q, nil, "oauth.token_revoke", client, row.OauthGrant.ID.String(), map[string]any{"userId": row.OauthGrant.UserID, "kind": row.OauthToken.Kind})
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// Resource server: /mcp

// Authenticate resolves an OAuth access token presented to /mcp. The token must be live, issued
// for the MCP endpoint (its audience), and its grant and owner still active. The actor carries
// the effective scopes (the token's, narrowed by the grant's current ones) and the app's name
// in Via, so history reads "Ada via Claude".
func (s *Server) Authenticate(ctx context.Context, token string) (*authz.Actor, error) {
	if !s.Enabled() || !strings.HasPrefix(token, AccessTokenPrefix) {
		return nil, ErrInvalidToken
	}
	row, err := s.Q.GetOAuthAccessToken(ctx, dbq.GetOAuthAccessTokenParams{TokenHash: secure.Hash(token), Resource: s.Resource()})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, ErrInvalidToken
	}
	scopes := intersect(row.OauthToken.Scopes, row.OauthGrant.Scopes)
	if len(scopes) == 0 {
		return nil, ErrInvalidToken
	}
	role, err := s.Track.EffectiveOrgRole(ctx, row.User)
	if err != nil {
		return nil, err
	}
	a := authz.UserActor(row.User, role)
	id := row.OauthToken.ID
	a.TokenID = &id
	a.Scopes = ActorScopes(scopes)
	a.Via = row.OauthClient.Name
	if row.OauthToken.LastUsedAt == nil || time.Since(*row.OauthToken.LastUsedAt) > time.Minute {
		_ = s.Q.TouchOAuthToken(ctx, id)
		_ = s.Q.TouchOAuthGrant(ctx, row.OauthGrant.ID)
	}
	return a, nil
}

// ---------------------------------------------------------------------------
// Connected apps

// RevokeGrant disconnects an app: the grant is revoked with all its tokens, so the app must ask
// again. People revoke their own grants; organisation admins (asAdmin) may revoke anyone's.
func (s *Server) RevokeGrant(ctx context.Context, a *authz.Actor, grantID uuid.UUID, asAdmin bool) error {
	row, err := s.Q.GetOAuthGrant(ctx, grantID)
	if err != nil || row.OauthGrant.RevokedAt != nil {
		return apperr.NotFound("connected app")
	}
	own := a.User != nil && row.OauthGrant.UserID == a.User.ID
	if !own && !(asAdmin && a.IsOrgAdmin()) {
		return apperr.NotFound("connected app")
	}
	return db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		if _, err := q.RevokeOAuthGrant(ctx, grantID); err != nil {
			return err
		}
		if err := q.RevokeOAuthGrantTokens(ctx, grantID); err != nil {
			return err
		}
		audit(ctx, q, a, "oauth.revoke", row.OauthClient, grantID.String(), map[string]any{"userId": row.OauthGrant.UserID, "scopes": row.OauthGrant.Scopes})
		return nil
	})
}

// Prune deletes expired codes and tokens and dynamically registered clients that nobody
// authorised within a week.
func (s *Server) Prune(ctx context.Context) error {
	now := time.Now()
	codes, err := s.Q.DeleteExpiredOAuthCodes(ctx, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	tokens, err := s.Q.DeleteExpiredOAuthTokens(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	clients, err := s.Q.DeleteUnusedOAuthClients(ctx, now.Add(-7*24*time.Hour))
	if err != nil {
		return err
	}
	if codes+tokens+clients > 0 {
		s.Log.Info("oauth records pruned", "codes", codes, "tokens", tokens, "clients", clients)
	}
	return nil
}
