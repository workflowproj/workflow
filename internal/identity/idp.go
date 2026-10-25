package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/secure"
)

// ---------------------------------------------------------------------------
// Groups from token claims

// maxClaimGroups bounds what one sign-in can create; a token listing more is cut short.
const maxClaimGroups = 200

// claimGroups collects the values at each dotted claim path ("groups", "realm_access.roles"),
// keeping those that start with prefix. A leading "/" (Keycloak group paths) is trimmed, and
// the prefix may be written with or without it.
func claimGroups(claims map[string]any, paths []string, prefix string) []string {
	var out []string
	add := func(v string) {
		name := strings.TrimPrefix(strings.TrimSpace(v), "/")
		if name == "" || len(name) > 200 || slices.Contains(out, name) || len(out) >= maxClaimGroups {
			return
		}
		if prefix == "" || strings.HasPrefix(name, strings.TrimPrefix(prefix, "/")) {
			out = append(out, name)
		}
	}
	for _, path := range paths {
		var cur any = claims
		for _, part := range strings.Split(path, ".") {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = m[part]
		}
		switch v := cur.(type) {
		case string:
			add(v)
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					add(s)
				}
			}
		}
	}
	return out
}

// claimGroupPrefix is the external id prefix of one provider's claim groups.
func claimGroupPrefix(provider string) string { return provider + "|" }

// SyncClaimGroups makes the person's groups from this provider exactly the ones their token
// lists: groups are created on first sight (source "oidc", external id "<provider>|<group>",
// shown as "<provider>/<group>") and memberships added or removed. Other providers' groups and
// groups from other sources are left alone; group -> role mappings then apply as usual.
func (p *Provisioner) SyncClaimGroups(ctx context.Context, userID uuid.UUID, provider string, groups []string) error {
	return pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		q := p.Q.WithTx(tx)
		current, err := q.UserGroupsBySourcePrefix(ctx, dbq.UserGroupsBySourcePrefixParams{UserID: userID, Source: "oidc", Prefix: claimGroupPrefix(provider)})
		if err != nil {
			return err
		}
		keep := map[uuid.UUID]bool{}
		for _, name := range groups {
			ext := claimGroupPrefix(provider) + name
			g, err := q.GetGroupBySourceExternal(ctx, dbq.GetGroupBySourceExternalParams{Source: "oidc", ExternalID: &ext})
			if errors.Is(err, pgx.ErrNoRows) {
				g, err = q.CreateGroup(ctx, dbq.CreateGroupParams{
					DisplayName: provider + "/" + name, Source: "oidc", ExternalID: &ext,
					Description: "From the groups claim of sign-in tokens from " + provider + ".",
				})
			}
			if err != nil {
				return err
			}
			keep[g.ID] = true
			if err := q.AddGroupMember(ctx, dbq.AddGroupMemberParams{GroupID: g.ID, UserID: userID}); err != nil {
				return err
			}
		}
		for _, g := range current {
			if !keep[g.ID] {
				if err := q.RemoveGroupMember(ctx, dbq.RemoveGroupMemberParams{GroupID: g.ID, UserID: userID}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// IdP sessions and logout

// idpSession is what a sign-in remembers about the IdP session it came from.
type idpSession struct {
	Provider, Subject, SID, IDToken string
}

// StartSessionIDP starts a session that the IdP can end (back-channel logout, by sid or
// subject) and that can end the IdP session in turn (RP-initiated logout, with the ID token as
// hint; kept sealed).
func (a *Auth) StartSessionIDP(w http.ResponseWriter, r *http.Request, user dbq.User, s idpSession) error {
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	sealed := ""
	if s.IDToken != "" {
		sealed = a.Box.Seal([]byte(s.IDToken))
	}
	return a.startSession(w, r, user, dbq.CreateSessionParams{IdpProvider: opt(s.Provider), IdpSubject: opt(s.Subject), IdpSid: opt(s.SID), IdpIDToken: opt(sealed)})
}

const backchannelEvent = "http://schemas.openid.net/event/backchannel-logout"

// HandleBackchannelLogout ends the sessions an IdP says are over (OpenID Connect Back-Channel
// Logout 1.0): POST /auth/{provider}/backchannel-logout with a signed logout_token. The token
// must come from the provider's keys, name our client as audience, not be expired, carry the
// back-channel logout event and no nonce (so an ID token cannot be replayed as one). Sessions go
// by sid when the token has one, else every session of that subject.
func (a *Auth) HandleBackchannelLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("provider")
	bad := func(desc string, err error) {
		a.Log.Warn("back-channel logout refused", "provider", id, "reason", desc, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request", "error_description": desc})
	}
	p, err := a.provider(r.Context(), id)
	if err != nil {
		bad("unknown or unavailable provider", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	raw := r.PostFormValue("logout_token")
	if raw == "" {
		bad("logout_token is required", nil)
		return
	}
	tok, err := p.verifier.Verify(a.ctx(r.Context()), raw)
	if err != nil {
		bad("invalid logout token", err)
		return
	}
	var c struct {
		SID    string         `json:"sid"`
		Nonce  *string        `json:"nonce"`
		Events map[string]any `json:"events"`
	}
	if err := tok.Claims(&c); err != nil {
		bad("invalid logout token", err)
		return
	}
	if _, ok := c.Events[backchannelEvent]; !ok || c.Nonce != nil {
		bad("not a logout token", nil)
		return
	}
	if c.SID == "" && tok.Subject == "" {
		bad("logout token names neither sid nor sub", nil)
		return
	}
	var n int64
	if c.SID != "" {
		n, err = a.Q.DeleteSessionsByIDPSid(r.Context(), dbq.DeleteSessionsByIDPSidParams{Provider: &id, Sid: &c.SID})
	} else {
		n, err = a.Q.DeleteSessionsByIDPSubject(r.Context(), dbq.DeleteSessionsByIDPSubjectParams{Provider: &id, Subject: &tok.Subject})
	}
	if err != nil {
		a.Log.Error("back-channel logout", "provider", id, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	a.Log.Info("back-channel logout", "provider", id, "by", map[bool]string{true: "sid", false: "sub"}[c.SID != ""], "sessions", n)
	w.WriteHeader(http.StatusOK)
}

// LogoutURL is where to send the browser to end the IdP session too (RP-initiated logout), or ""
// when the session did not come from a provider with single logout on and an end-session
// endpoint. Call it before EndSession, which forgets the session.
func (a *Auth) LogoutURL(ctx context.Context, r *http.Request) string {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	row, err := a.Q.GetSessionIDP(ctx, secure.Hash(c.Value))
	if err != nil || row.IdpProvider == nil {
		return ""
	}
	p, err := a.provider(ctx, *row.IdpProvider)
	if err != nil || !p.cfg.SingleLogout {
		return ""
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := p.provider.Claims(&meta); err != nil || meta.EndSession == "" {
		return ""
	}
	u, err := url.Parse(meta.EndSession)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("client_id", p.cfg.ClientID)
	q.Set("post_logout_redirect_uri", a.Cfg.PublicURL+"/login")
	if row.IdpIDToken != nil {
		if hint, err := a.Box.Open(*row.IdpIDToken); err == nil {
			q.Set("id_token_hint", string(hint))
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ---------------------------------------------------------------------------
// External JWT bearer tokens

// apiScopes maps the IdP scopes a token may carry to Workflow token scopes.
var apiScopes = map[string]string{"wfm:read": "read", "wfm:write": "write", "wfm:admin": "admin"}

// looksLikeJWT is a cheap check before any key lookups: three base64url segments.
func looksLikeJWT(s string) bool {
	parts := strings.Split(s, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// unverifiedIssuer reads iss from a JWT payload, only to pick which provider verifies it.
func unverifiedIssuer(raw string) string {
	parts := strings.Split(raw, ".")
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var c struct {
		Iss string `json:"iss"`
	}
	_ = json.Unmarshal(b, &c)
	return strings.TrimRight(c.Iss, "/")
}

// authenticateJWT accepts an IdP access token for the API: signed by a provider that has
// API_AUDIENCE set, with that audience (so sign-in ID tokens, whose audience is the client id,
// do not work), unexpired, and not an ID or logout token. A client listed in API_CLIENTS acts as
// its service account (client-credentials tokens); otherwise sub must be a person who has
// signed in with that provider. The token's wfm:read / wfm:write / wfm:admin scopes become the
// actor's scopes, and at least one is needed.
func (a *Auth) authenticateJWT(ctx context.Context, raw string) (*authz.Actor, error) {
	iss := unverifiedIssuer(raw)
	for _, pc := range a.Cfg.OIDCProviders {
		if pc.APIAudience == "" || strings.TrimRight(pc.Issuer, "/") != iss {
			continue
		}
		p, err := a.provider(ctx, pc.ID)
		if err != nil {
			return nil, err
		}
		tok, err := p.provider.Verifier(&oidc.Config{ClientID: pc.APIAudience}).Verify(a.ctx(ctx), raw)
		if err != nil {
			return nil, apperr.Unauthorized()
		}
		var c struct {
			Typ      string         `json:"typ"`
			Scope    string         `json:"scope"`
			Azp      string         `json:"azp"`
			ClientID string         `json:"client_id"`
			Events   map[string]any `json:"events"`
		}
		if err := tok.Claims(&c); err != nil || strings.EqualFold(c.Typ, "ID") || strings.EqualFold(c.Typ, "Logout") || c.Events != nil {
			return nil, apperr.Unauthorized()
		}
		var scopes []string
		for _, s := range strings.Fields(c.Scope) {
			if m, ok := apiScopes[s]; ok && !slices.Contains(scopes, m) {
				scopes = append(scopes, m)
			}
		}
		if len(scopes) == 0 {
			return nil, apperr.Forbidden("the token has none of the scopes wfm:read, wfm:write or wfm:admin")
		}
		client := c.ClientID
		if client == "" {
			client = c.Azp
		}
		var user dbq.User
		if account, ok := pc.APIClients[client]; ok && client != "" {
			user, err = a.Q.GetServiceAccountByUsername(ctx, account)
			if err != nil {
				a.Log.Warn("API token for an unknown service account", "provider", pc.ID, "client", client, "account", account)
				return nil, apperr.Unauthorized()
			}
		} else {
			user, err = a.Q.GetUserByIdentity(ctx, dbq.GetUserByIdentityParams{Provider: pc.ID, Subject: tok.Subject})
			if err != nil || !user.Active || user.DeletedAt != nil {
				return nil, apperr.Unauthorized()
			}
		}
		role, err := a.Track.EffectiveOrgRole(ctx, user)
		if err != nil {
			return nil, err
		}
		actor := authz.UserActor(user, role)
		actor.External = true
		actor.TokenName = fmt.Sprintf("%s token", pc.DisplayName)
		if client != "" {
			actor.TokenName = fmt.Sprintf("%s client %s", pc.DisplayName, client)
		}
		actor.Scopes = scopes
		return actor, nil
	}
	return nil, apperr.Unauthorized()
}
