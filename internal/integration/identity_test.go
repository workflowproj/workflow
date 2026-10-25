package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/geoffo-dev/workflow-management/internal/identity/directory"
)

// ---------------------------------------------------------------------------
// SCIM

type scimClient struct {
	c    *Client
	base string
}

func (e *Env) scim(token string) *scimClient {
	c := e.withToken(token)
	return &scimClient{c: c, base: "/scim/v2"}
}

func (s *scimClient) call(method, path string, body any, want int) obj {
	s.c.e.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	var r Response
	if raw != nil {
		r = s.c.do(method, s.base+path, raw, "Content-Type", "application/scim+json")
	} else {
		r = s.c.do(method, s.base+path, nil)
	}
	if r.Status != want {
		s.c.e.t.Fatalf("SCIM %s %s: status %d, want %d\n%s", method, path, r.Status, want, r.Body)
	}
	return r.JSON()
}

func patchOps(ops ...obj) obj {
	return obj{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"}, "Operations": ops}
}

func TestSCIMProvisioning(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	admin.must("POST", "/api/v1/projects", obj{"key": "ENG", "name": "Engineering", "template": "kanban", "access": "private"}, 201, nil)

	var conn obj
	admin.must("POST", "/api/v1/admin/scim-connections", obj{"name": "Okta"}, 201, &conn)
	if !strings.HasPrefix(str(conn, "token"), "wfm_scim_") || !strings.HasSuffix(str(conn, "baseUrl"), "/scim/v2") {
		t.Fatalf("connection = %v", conn)
	}
	okta := e.scim(str(conn, "token"))
	e.scim("wfm_scim_wrong").call("GET", "/Users", nil, 401)

	spc := okta.call("GET", "/ServiceProviderConfig", nil, 200)
	eq(t, spc["patch"].(obj)["supported"], any(true), "patch supported")
	eq(t, len(okta.call("GET", "/ResourceTypes", nil, 200)["Resources"].([]any)), 2, "resource types")

	// Okta-style create.
	jane := okta.call("POST", "/Users", obj{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:User"}, "userName": "Jane@Example.com", "externalId": "00u1",
		"name":   obj{"givenName": "Jane", "familyName": "Doe"},
		"emails": []obj{{"primary": true, "value": "jane@example.com", "type": "work"}}, "displayName": "Jane Doe", "active": true,
	}, 201)
	janeID := str(jane, "id")
	eq(t, str(jane, "meta", "resourceType"), "User", "meta")
	okta.call("POST", "/Users", obj{"userName": "jane@example.com", "emails": []obj{{"value": "jane@example.com"}}}, 409)
	list := okta.call("GET", "/Users?filter="+url.QueryEscape(`userName eq "jane@example.com"`), nil, 200)
	eq(t, fmt.Sprint(list["totalResults"]), "1", "filter by userName")
	eq(t, fmt.Sprint(okta.call("GET", "/Users?filter="+url.QueryEscape(`userName eq "nobody@example.com"`), nil, 200)["totalResults"]), "0", "no match")
	okta.call("GET", "/Users?filter="+url.QueryEscape(`userName xx "a"`), nil, 400)

	// SCIM-managed users can sign in; their profile is owned by the IdP.
	janeC := e.login("jane@example.com", "Ignored")
	eq(t, str(janeC.Me, "displayName"), "Jane Doe", "profile from SCIM")
	eq(t, str(janeC.Me, "source"), "scim", "source on /me")
	var pat obj
	janeC.must("POST", "/api/v1/me/tokens", obj{"name": "cli", "scopes": []string{"read"}, "expiresInDays": 30}, 201, &pat)
	janeTok := e.withToken(str(pat, "token"))
	janeTok.must("GET", "/api/v1/me", nil, 200, nil)

	// Groups → project roles.
	g := okta.call("POST", "/Groups", obj{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Group"}, "displayName": "Engineering",
		"externalId": "00g1", "members": []obj{{"value": janeID}}}, 201)
	groupID := str(g, "id")
	janeC.must("GET", "/api/v1/projects/ENG", nil, 404, nil)
	admin.must("POST", "/api/v1/projects/ENG/members", obj{"groupId": groupID, "roleKey": "member"}, 201, nil)
	janeC.must("GET", "/api/v1/projects/ENG", nil, 200, nil)
	admin.must("PATCH", "/api/v1/admin/groups/"+groupID, obj{"displayName": "Renamed"}, 400, nil) // owned by the IdP

	// Entra-style member removal by filter, then add back.
	okta.call("PATCH", "/Groups/"+groupID, patchOps(obj{"op": "Remove", "path": fmt.Sprintf(`members[value eq "%s"]`, janeID)}), 200)
	janeC.must("GET", "/api/v1/projects/ENG", nil, 404, nil)
	okta.call("PATCH", "/Groups/"+groupID, patchOps(obj{"op": "Add", "path": "members", "value": []obj{{"value": janeID}}}), 200)
	janeC.must("GET", "/api/v1/projects/ENG", nil, 200, nil)
	g = okta.call("GET", "/Groups/"+groupID, nil, 200)
	eq(t, len(g["members"].([]any)), 1, "member listed")

	// A group mapped to the admin org role makes its members organisation admins.
	janeC.must("GET", "/api/v1/admin/users", nil, 403, nil)
	admin.must("PATCH", "/api/v1/admin/groups/"+groupID, obj{"orgRole": "admin"}, 200, nil)
	janeC.must("GET", "/api/v1/admin/users", nil, 200, nil)
	admin.must("PATCH", "/api/v1/admin/groups/"+groupID, obj{"orgRole": nil}, 200, nil)

	// Profile updates (Entra sends replace without a path).
	okta.call("PATCH", "/Users/"+janeID, patchOps(obj{"op": "Replace", "value": obj{"displayName": "Jane Q. Doe", "name.givenName": "Janet"}}), 200)
	u := okta.call("GET", "/Users/"+janeID, nil, 200)
	eq(t, str(u, "displayName"), "Jane Q. Doe", "display name patched")
	eq(t, str(u, "name", "givenName"), "Janet", "dotted path patched")

	// Entra deactivation ("False" as a string) revokes sessions and tokens immediately.
	okta.call("PATCH", "/Users/"+janeID, patchOps(obj{"op": "Replace", "path": "active", "value": "False"}), 200)
	janeC.must("GET", "/api/v1/me", nil, 401, nil)
	janeTok.must("GET", "/api/v1/me", nil, 401, nil)
	r := e.anon().do("POST", "/api/v1/auth/dev-login", obj{"email": "jane@example.com"})
	eq(t, r.Status, 403, "deactivated user cannot sign in")

	// Okta reactivates with PUT.
	okta.call("PUT", "/Users/"+janeID, obj{"userName": "jane@example.com", "name": obj{"givenName": "Jane", "familyName": "Doe"},
		"emails": []obj{{"value": "jane@example.com", "primary": true}}, "displayName": "Jane Doe", "active": true}, 200)
	e.login("jane@example.com", "")
	janeTok.must("GET", "/api/v1/me", nil, 401, nil) // tokens stay revoked

	// Delete hides the user from SCIM; re-creating restores the same account.
	okta.call("DELETE", "/Users/"+janeID, nil, 204)
	okta.call("GET", "/Users/"+janeID, nil, 404)
	again := okta.call("POST", "/Users", obj{"userName": "jane@example.com", "emails": []obj{{"value": "jane@example.com"}}, "active": true}, 201)
	eq(t, str(again, "id"), janeID, "restored account keeps its history")

	// Groups from another connection are invisible; users are shared.
	var conn2 obj
	admin.must("POST", "/api/v1/admin/scim-connections", obj{"name": "Entra"}, 201, &conn2)
	entra := e.scim(str(conn2, "token"))
	entra.call("GET", "/Groups/"+groupID, nil, 404)
	eq(t, fmt.Sprint(entra.call("GET", "/Users?filter="+url.QueryEscape(`userName eq "jane@example.com"`), nil, 200)["totalResults"]), "1", "users visible")

	// Rotating a token invalidates the old one.
	var rotated obj
	admin.must("POST", "/api/v1/admin/scim-connections/"+str(conn, "id")+"/rotate", nil, 200, &rotated)
	okta.call("GET", "/Users", nil, 401)
	e.scim(str(rotated, "token")).call("GET", "/Users", nil, 200)

	// Deleting a SCIM group removes the project membership it granted.
	e.scim(str(rotated, "token")).call("DELETE", "/Groups/"+groupID, nil, 204)
	var members []obj
	admin.get("/api/v1/projects/ENG/members", &members)
	for _, m := range members {
		if str(m, "group", "id") == groupID {
			t.Fatal("membership of deleted group remains")
		}
	}

	var prov obj
	admin.get("/api/v1/admin/provisioning", &prov)
	eq(t, prov["oidc"].(obj)["jitProvisioning"], any(false), "JIT off while SCIM is connected (auto)")
	eq(t, len(prov["scim"].(obj)["connections"].([]any)), 2, "SCIM connections listed")

	var audit []obj
	admin.get("/api/v1/admin/audit?action=scim.", &audit)
	if len(audit) == 0 {
		t.Fatal("no SCIM audit events")
	}
}

// ---------------------------------------------------------------------------
// OIDC

type fakeIdP struct {
	*httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	codes  map[string]idpGrant
	tokens int
}

type idpGrant struct {
	claims    jwt.MapClaims
	challenge string
	clientID  string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key, codes: map[string]idpGrant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(obj{
			"issuer": f.URL, "authorization_endpoint": f.URL + "/authorize", "token_endpoint": f.URL + "/token",
			"jwks_uri": f.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"end_session_endpoint": f.URL + "/logout",
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		b64 := base64.RawURLEncoding.EncodeToString
		_ = json.NewEncoder(w).Encode(obj{"keys": []obj{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		g, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		f.tokens++
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(obj{"error": "invalid_grant"})
			return
		}
		user, _, _ := r.BasicAuth()
		if user == "" {
			user = r.Form.Get("client_id")
		}
		if user != g.clientID {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, g.claims)
		tok.Header["kid"] = "k1"
		signed, _ := tok.SignedString(key)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(obj{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": signed})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// sign signs claims with the IdP's key (logout tokens, access tokens for the API).
func (f *fakeIdP) sign(claims jwt.MapClaims) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	signed, _ := tok.SignedString(f.key)
	return signed
}

// signIn runs the browser side of the authorisation code flow and returns the final redirect.
func (f *fakeIdP) signIn(t *testing.T, c *Client, provider string, claims jwt.MapClaims) string {
	t.Helper()
	r := c.do("GET", "/auth/"+provider+"/login?returnTo=/projects/ENG", nil)
	if r.Status != http.StatusFound {
		t.Fatalf("login: status %d %s", r.Status, r.Body)
	}
	authURL, _ := url.Parse(r.Header.Get("Location"))
	if !strings.HasPrefix(authURL.String(), f.URL+"/authorize") {
		t.Fatalf("login redirected to %s", authURL)
	}
	q := authURL.Query()
	eq(t, q.Get("code_challenge_method"), "S256", "PKCE")
	now := time.Now()
	full := jwt.MapClaims{"iss": f.URL, "aud": q.Get("client_id"), "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": q.Get("nonce")}
	for k, v := range claims {
		full[k] = v
	}
	code := fmt.Sprintf("code-%d", now.UnixNano())
	f.mu.Lock()
	f.codes[code] = idpGrant{claims: full, challenge: q.Get("code_challenge"), clientID: q.Get("client_id")}
	f.mu.Unlock()
	cb, _ := url.Parse(q.Get("redirect_uri"))
	r = c.do("GET", cb.Path+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), nil)
	if r.Status != http.StatusFound {
		t.Fatalf("callback: status %d %s", r.Status, r.Body)
	}
	return r.Header.Get("Location")
}

func oidcEnv(t *testing.T, idp *fakeIdP, extra ...envOption) *Env {
	opts := []envOption{
		withEnv("WFM_OIDC_PROVIDERS", "corp"), withEnv("WFM_OIDC_CORP_ISSUER", idp.URL),
		withEnv("WFM_OIDC_CORP_CLIENT_ID", "wfm-client"), withEnv("WFM_OIDC_CORP_CLIENT_SECRET", "shh"),
		withEnv("WFM_OIDC_CORP_DISPLAY_NAME", "Corp SSO"), withEnv("WFM_ALLOWED_DOMAINS", "example.com"),
	}
	return newEnv(t, append(opts, extra...)...)
}

func TestOIDCSignIn(t *testing.T) {
	idp := newFakeIdP(t)
	e := oidcEnv(t, idp, withEnv("WFM_BOOTSTRAP_ADMINS", "boss@example.com"))

	var providers struct {
		DevLogin  bool  `json:"devLogin"`
		Providers []obj `json:"providers"`
	}
	e.anon().get("/api/v1/auth/providers", &providers)
	eq(t, len(providers.Providers), 1, "providers")
	eq(t, str(providers.Providers[0], "name"), "Corp SSO", "provider name")

	// First sign-in provisions just in time; returnTo is honoured.
	c := e.anon()
	loc := idp.signIn(t, c, "corp", jwt.MapClaims{"sub": "u-1", "email": "Ada@Example.com", "email_verified": true, "name": "Ada Lovelace"})
	eq(t, loc, "/projects/ENG", "return to")
	var me obj
	c.get("/api/v1/me", &me)
	eq(t, str(me, "email"), "ada@example.com", "email normalised")
	eq(t, str(me, "orgRole"), "admin", "first user is admin")

	// Bootstrap admins are promoted; others are members. Profile updates follow the IdP.
	boss := e.anon()
	idp.signIn(t, boss, "corp", jwt.MapClaims{"sub": "u-2", "email": "boss@example.com", "email_verified": "true", "name": "Boss"})
	boss.get("/api/v1/me", &me)
	eq(t, str(me, "orgRole"), "admin", "bootstrap admin")
	bob := e.anon()
	idp.signIn(t, bob, "corp", jwt.MapClaims{"sub": "u-3", "email": "bob@example.com", "email_verified": true, "name": "Bob"})
	bob.get("/api/v1/me", &me)
	eq(t, str(me, "orgRole"), "member", "member")
	idp.signIn(t, bob, "corp", jwt.MapClaims{"sub": "u-3", "email": "bob@example.com", "email_verified": true, "name": "Robert"})
	bob.get("/api/v1/me", &me)
	eq(t, str(me, "displayName"), "Robert", "profile refreshed")

	// Refusals.
	cases := map[string]jwt.MapClaims{
		"domain_not_allowed": {"sub": "u-4", "email": "eve@evil.test", "email_verified": true},
		"email_unverified":   {"sub": "u-5", "email": "carl@example.com", "email_verified": false},
	}
	for code, claims := range cases {
		loc := idp.signIn(t, e.anon(), "corp", claims)
		eq(t, loc, "/login?error="+code, code)
	}
	// Tampered state and a replayed code are rejected.
	x := e.anon()
	r := x.do("GET", "/auth/corp/login", nil)
	authURL, _ := url.Parse(r.Header.Get("Location"))
	r = x.do("GET", "/auth/corp/callback?code=nope&state="+url.QueryEscape(authURL.Query().Get("state")+"x"), nil)
	eq(t, r.Header.Get("Location"), "/login?error=invalid_state", "state mismatch")
	r = x.do("GET", "/auth/corp/login", nil) // the failed callback cleared the state cookie
	authURL, _ = url.Parse(r.Header.Get("Location"))
	r = x.do("GET", "/auth/corp/callback?code=nope&state="+url.QueryEscape(authURL.Query().Get("state")), nil)
	eq(t, r.Header.Get("Location"), "/login?error=exchange_failed", "unknown code")
	// Open redirects are not allowed.
	r = e.anon().do("GET", "/auth/corp/login?returnTo="+url.QueryEscape("//evil.test/x"), nil)
	eq(t, r.Status, http.StatusFound, "login with bad returnTo")

	// Deactivated users cannot sign in.
	admin := e.anon()
	idp.signIn(t, admin, "corp", jwt.MapClaims{"sub": "u-1", "email": "ada@example.com", "email_verified": true})
	bob.get("/api/v1/me", &me)
	admin.must("PATCH", "/api/v1/admin/users/"+str(me, "id"), obj{"active": false}, 200, nil)
	bob.must("GET", "/api/v1/me", nil, 401, nil)
	eq(t, idp.signIn(t, e.anon(), "corp", jwt.MapClaims{"sub": "u-3", "email": "bob@example.com", "email_verified": true}), "/login?error=deactivated", "deactivated")

	// With JIT disabled, unknown people are refused but existing users still sign in.
	e2 := oidcEnv(t, idp, withEnv("WFM_JIT_PROVISIONING", "false"))
	eq(t, idp.signIn(t, e2.anon(), "corp", jwt.MapClaims{"sub": "n-1", "email": "new@example.com", "email_verified": true}), "/login?error=not_provisioned", "JIT disabled")
	e2.login("known@example.com", "Known") // pre-provisioned (dev login stands in for an admin invite)
	eq(t, idp.signIn(t, e2.anon(), "corp", jwt.MapClaims{"sub": "n-2", "email": "known@example.com", "email_verified": true}), "/projects/ENG", "known user")

	// Logout ends the session server-side.
	c.must("POST", "/api/v1/auth/logout", nil, 204, nil)
	c.must("GET", "/api/v1/me", nil, 401, nil)
}

// ---------------------------------------------------------------------------
// Google directory sync

type fakeDirectory struct {
	users   []directory.User
	groups  []directory.Group
	members map[string][]directory.Member
}

func (f *fakeDirectory) ListUsers(context.Context, string) ([]directory.User, error) {
	return f.users, nil
}
func (f *fakeDirectory) ListGroups(context.Context) ([]directory.Group, error) { return f.groups, nil }
func (f *fakeDirectory) ListMembers(_ context.Context, key string) ([]directory.Member, error) {
	return f.members[key], nil
}

func gUser(id, email, name string) directory.User {
	u := directory.User{ID: id, PrimaryEmail: email, OrgUnitPath: "/Engineering"}
	u.Name.FullName = name
	return u
}

func TestGoogleDirectorySync(t *testing.T) {
	e := newEnv(t,
		withEnv("WFM_GOOGLE_DIRECTORY_ENABLED", "true"), withEnv("WFM_GOOGLE_SERVICE_ACCOUNT_KEY", "{}"),
		withEnv("WFM_GOOGLE_ADMIN_SUBJECT", "admin@example.com"), withEnv("WFM_GOOGLE_SYNC_GROUP", "wfm-users@example.com"),
		withEnv("WFM_GOOGLE_GROUP_PREFIX", "team-"),
	)
	admin := e.login("admin@example.com", "Admin")
	admin.must("POST", "/api/v1/projects", obj{"key": "PLAT", "name": "Platform", "template": "kanban", "access": "private"}, 201, nil)

	dir := &fakeDirectory{
		users: []directory.User{
			gUser("g1", "Ann@example.com", "Ann"), gUser("g2", "ben@example.com", "Ben"), gUser("g3", "cat@example.com", "Cat (no access)"),
		},
		groups: []directory.Group{{ID: "grp-plat", Email: "team-platform@example.com", Name: "Platform team"}, {ID: "grp-all", Email: "all@example.com", Name: "Everyone"}},
		members: map[string][]directory.Member{
			"wfm-users@example.com": {{ID: "g1", Email: "ann@example.com", Type: "USER"}, {ID: "g2", Email: "ben@example.com", Type: "USER"}},
			"grp-plat":              {{ID: "g1", Email: "ann@example.com", Type: "USER"}, {ID: "nested", Type: "GROUP"}},
		},
	}
	e.App.Syncer.NewClient = func(context.Context) (directory.Client, error) { return dir, nil }

	run := func() {
		t.Helper()
		admin.must("POST", "/api/v1/admin/directory-sync/run", nil, 202, nil)
		e.DrainJobs()
	}
	run()
	var users []obj
	admin.get("/api/v1/admin/users?q=example.com", &users)
	emails := map[string]obj{}
	for _, u := range users {
		emails[str(u, "email")] = u
	}
	if emails["ann@example.com"] == nil || emails["ben@example.com"] == nil || emails["cat@example.com"] != nil {
		t.Fatalf("synced users = %v", keysOf(emails))
	}
	eq(t, str(emails["ann@example.com"], "source"), "google", "source")

	// Only prefixed groups are synced; nested groups are ignored.
	var groups []obj
	admin.get("/api/v1/groups", &groups)
	var platGroup obj
	for _, g := range groups {
		if str(g, "displayName") == "Platform team" {
			platGroup = g
		}
		if str(g, "displayName") == "Everyone" {
			t.Fatal("group without prefix was synced")
		}
	}
	eq(t, fmt.Sprint(platGroup["memberCount"]), "1", "group members")
	admin.must("POST", "/api/v1/projects/PLAT/members", obj{"groupId": platGroup["id"], "roleKey": "member"}, 201, nil)
	ann := e.login("ann@example.com", "")
	ann.must("GET", "/api/v1/projects/PLAT", nil, 200, nil)

	// Suspension deactivates and revokes; removal from the access group deactivates;
	// deletion upstream soft-deletes (anonymised later by maintenance).
	dir.users[0].Suspended = true
	dir.members["wfm-users@example.com"] = dir.members["wfm-users@example.com"][:1] // ben removed from access group
	run()
	ann.must("GET", "/api/v1/me", nil, 401, nil)
	admin.get("/api/v1/admin/users?q=example.com", &users)
	for _, u := range users {
		if str(u, "email") == "ben@example.com" && u["active"] != false {
			t.Fatalf("ben should be deactivated: %v", u)
		}
	}
	dir.users[0].Suspended = false
	run()
	e.login("ann@example.com", "") // reactivated

	dir.users = dir.users[:1] // ben deleted in Google
	run()
	var runs struct {
		DirectorySync obj `json:"directorySync"`
	}
	admin.get("/api/v1/admin/provisioning", &runs)
	last := runs.DirectorySync["runs"].([]any)[0].(obj)
	eq(t, str(last, "status"), "succeeded", "last run status")
	eq(t, fmt.Sprint(last["stats"].(obj)["usersDeleted"]), "1", "deleted upstream")

	// JIT is off while directory sync is on: unknown people cannot sign in through OIDC.
	eq(t, e.App.Prov.JITEnabled(context.Background()), false, "JIT disabled by directory sync")
}

func keysOf(m map[string]obj) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Tokens, scopes, CSRF, service accounts, audit

func TestTokensScopesAndCSRF(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	admin.createProject("TOK", "kanban")

	// Cookie sessions need X-Requested-With on unsafe methods.
	noCSRF := *admin
	noCSRF.csrf = false
	eq(t, noCSRF.do("POST", "/api/v1/issues", obj{"projectKey": "TOK", "typeKey": "task", "summary": "x"}).Status, 403, "CSRF header required")
	eq(t, noCSRF.do("GET", "/api/v1/me", nil).Status, 200, "GET without header is fine")

	mk := func(c *Client, scopes ...string) *Client {
		var tok obj
		c.must("POST", "/api/v1/me/tokens", obj{"name": strings.Join(scopes, "+"), "scopes": scopes, "expiresInDays": 7}, 201, &tok)
		if !strings.HasPrefix(str(tok, "token"), "wfm_pat_") {
			t.Fatalf("token = %v", tok)
		}
		return e.withToken(str(tok, "token"))
	}
	read := mk(admin, "read")
	write := mk(admin, "write")
	adm := mk(admin, "admin")

	read.must("GET", "/api/v1/projects/TOK", nil, 200, nil)
	read.must("POST", "/api/v1/issues", obj{"projectKey": "TOK", "typeKey": "task", "summary": "x"}, 403, nil)
	write.must("POST", "/api/v1/issues", obj{"projectKey": "TOK", "typeKey": "task", "summary": "via token"}, 201, nil) // no CSRF header needed
	write.must("GET", "/api/v1/admin/users", nil, 403, nil)
	adm.must("GET", "/api/v1/admin/users", nil, 200, nil)
	e.withToken("wfm_pat_bogus").must("GET", "/api/v1/me", nil, 401, nil)

	// Only organisation admins can mint admin-scoped tokens.
	mel := e.login("mel@example.com", "Mel")
	mel.must("POST", "/api/v1/me/tokens", obj{"name": "x", "scopes": []string{"admin"}, "expiresInDays": 7}, 400, nil)

	// Revocation.
	var toks []obj
	admin.get("/api/v1/me/tokens", &toks)
	eq(t, len(toks), 3, "tokens listed")
	for _, tk := range toks {
		if str(tk, "name") == "read" {
			admin.must("DELETE", "/api/v1/me/tokens/"+str(tk, "id"), nil, 204, nil)
		}
	}
	read.must("GET", "/api/v1/me", nil, 401, nil)

	// Service accounts get project roles and tokens like people, but never sign in interactively.
	var sa obj
	admin.must("POST", "/api/v1/admin/users", obj{"kind": "service", "displayName": "CI bot"}, 201, &sa)
	var satok obj
	admin.must("POST", "/api/v1/admin/users/"+str(sa, "id")+"/tokens", obj{"name": "ci", "scopes": []string{"write"}, "expiresInDays": 90}, 201, &satok)
	bot := e.withToken(str(satok, "token"))
	bot.must("POST", "/api/v1/issues", obj{"projectKey": "TOK", "typeKey": "task", "summary": "from CI"}, 201, nil) // open project
	var issue obj
	bot.get("/api/v1/issues/TOK-2", &issue)
	eq(t, str(issue, "reporter", "kind"), "service", "service account reporter")
	admin.must("POST", "/api/v1/admin/users/"+mel.id()+"/tokens", obj{"name": "x", "scopes": []string{"read"}, "expiresInDays": 1}, 400, nil) // people mint their own

	// Expired tokens stop working.
	e.exec(`UPDATE api_tokens SET expires_at = now() - interval '1 minute' WHERE name = 'ci'`)
	bot.must("GET", "/api/v1/me", nil, 401, nil)

	// Audit log records security-relevant actions.
	var audit []obj
	admin.get("/api/v1/admin/audit", &audit)
	actions := map[string]bool{}
	for _, ev := range audit {
		actions[str(ev, "action")] = true
	}
	for _, want := range []string{"token.create", "token.revoke", "user.create"} {
		if !actions[want] {
			t.Fatalf("audit lacks %s: %v", want, actions)
		}
	}
}

// ---------------------------------------------------------------------------
// Groups from sign-in tokens, logout from the IdP, IdP access tokens for the API

// meID is the id of whoever the client is signed in as.
func meID(c *Client) string {
	var me obj
	c.get("/api/v1/me", &me)
	return str(me, "id")
}

func TestOIDCGroupsClaim(t *testing.T) {
	idp := newFakeIdP(t)
	e := oidcEnv(t, idp, withEnv("WFM_OIDC_CORP_GROUPS_CLAIM", "groups,realm_access.roles"), withEnv("WFM_OIDC_CORP_GROUPS_PREFIX", "wfm-"))
	admin := e.anon()
	idp.signIn(t, admin, "corp", jwt.MapClaims{"sub": "u-1", "email": "ada@example.com", "email_verified": true, "name": "Ada"})
	bob := e.anon()
	signIn := func(groups, roles []any) {
		idp.signIn(t, bob, "corp", jwt.MapClaims{"sub": "u-2", "email": "bob@example.com", "email_verified": true, "name": "Bob",
			"groups": groups, "realm_access": map[string]any{"roles": roles}})
	}
	groups := func() map[string]obj {
		var list []obj
		admin.get("/api/v1/groups", &list)
		out := map[string]obj{}
		for _, g := range list {
			out[str(g, "displayName")] = g
		}
		return out
	}
	members := func(g obj) []string {
		var d obj
		admin.get("/api/v1/groups/"+str(g, "id"), &d)
		var out []string
		for _, m := range d["members"].([]any) {
			out = append(out, str(m.(map[string]any), "email"))
		}
		return out
	}

	// Values at every claim path count, with Keycloak's leading "/" trimmed and the prefix applied.
	signIn([]any{"/wfm-admins", "/sales"}, []any{"wfm-qa", "offline_access"})
	g := groups()
	if _, ok := g["corp/wfm-admins"]; !ok {
		t.Fatalf("claim groups: %v", g)
	}
	if _, ok := g["corp/sales"]; ok {
		t.Fatal("the prefix filters groups")
	}
	eq(t, str(g["corp/wfm-qa"], "source"), "oidc", "nested claim path, source oidc")
	eq(t, fmt.Sprint(members(g["corp/wfm-admins"])), "[bob@example.com]", "member")

	// Group -> role mappings apply; the IdP owns the name.
	admin.must("PATCH", "/api/v1/admin/groups/"+str(g["corp/wfm-admins"], "id"), obj{"orgRole": "admin"}, 200, nil)
	admin.must("PATCH", "/api/v1/admin/groups/"+str(g["corp/wfm-admins"], "id"), obj{"displayName": "Renamed"}, 400, nil)
	var me obj
	bob.get("/api/v1/me", &me)
	eq(t, str(me, "orgRole"), "admin", "role from the group mapping")

	// The next sign-in is authoritative: a group the token no longer lists is left.
	signIn([]any{"/sales"}, []any{"wfm-qa"})
	bob.get("/api/v1/me", &me)
	eq(t, str(me, "orgRole"), "member", "mapping gone with the membership")
	eq(t, len(members(groups()["corp/wfm-admins"])), 0, "membership removed")
	eq(t, fmt.Sprint(members(groups()["corp/wfm-qa"])), "[bob@example.com]", "kept")

	// Groups managed elsewhere are untouched by sign-in.
	var local obj
	admin.must("POST", "/api/v1/admin/groups", obj{"displayName": "Local team"}, 201, &local)
	admin.must("PUT", "/api/v1/admin/groups/"+str(local, "id")+"/members/"+meID(bob), nil, 204, nil)
	signIn([]any{}, nil)
	eq(t, fmt.Sprint(members(local)), "[bob@example.com]", "local group kept")
	eq(t, len(members(groups()["corp/wfm-qa"])), 0, "no claim groups left")
}

func TestOIDCBackchannelLogout(t *testing.T) {
	idp := newFakeIdP(t)
	e := oidcEnv(t, idp)
	session := func(sub, sid string) *Client {
		c := e.anon()
		idp.signIn(t, c, "corp", jwt.MapClaims{"sub": sub, "sid": sid, "email": sub + "@example.com", "email_verified": true})
		c.must("GET", "/api/v1/me", nil, 200, nil)
		return c
	}
	adaLaptop, adaPhone, bob := session("ada", "sid-laptop"), session("ada", "sid-phone"), session("bob", "sid-bob")
	now := time.Now()
	logoutToken := func(extra jwt.MapClaims) string {
		c := jwt.MapClaims{"iss": idp.URL, "aud": "wfm-client", "iat": now.Unix(), "exp": now.Add(2 * time.Minute).Unix(), "jti": fmt.Sprint(now.UnixNano()),
			"events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}}}
		for k, v := range extra {
			if v == nil {
				delete(c, k)
				continue
			}
			c[k] = v
		}
		return idp.sign(c)
	}
	post := func(tok string) Response {
		return e.anon().do("POST", "/auth/corp/backchannel-logout", "logout_token="+url.QueryEscape(tok), "Content-Type", "application/x-www-form-urlencoded")
	}

	// Tokens that are not valid logout tokens change nothing.
	for name, tok := range map[string]string{
		"garbage":       "not-a-jwt",
		"wrong aud":     logoutToken(jwt.MapClaims{"aud": "someone-else", "sid": "sid-laptop"}),
		"expired":       logoutToken(jwt.MapClaims{"exp": now.Add(-time.Minute).Unix(), "sid": "sid-laptop"}),
		"no exp":        logoutToken(jwt.MapClaims{"exp": nil, "sid": "sid-laptop"}),
		"nonce":         logoutToken(jwt.MapClaims{"nonce": "n", "sid": "sid-laptop"}),
		"no event":      logoutToken(jwt.MapClaims{"events": nil, "sid": "sid-laptop"}),
		"no sid or sub": logoutToken(nil),
	} {
		r := post(tok)
		if r.Status != http.StatusBadRequest || r.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	adaLaptop.must("GET", "/api/v1/me", nil, 200, nil)

	// By sid: that one session ends.
	r := post(logoutToken(jwt.MapClaims{"sid": "sid-laptop", "sub": "ada"}))
	eq(t, r.Status, http.StatusOK, "logout by sid: "+string(r.Body))
	eq(t, r.Header.Get("Cache-Control"), "no-store", "no-store")
	adaLaptop.must("GET", "/api/v1/me", nil, 401, nil)
	adaPhone.must("GET", "/api/v1/me", nil, 200, nil)

	// By sub: every session of that person at this provider ends; others stay.
	adaTablet := session("ada", "")
	eq(t, post(logoutToken(jwt.MapClaims{"sub": "ada"})).Status, http.StatusOK, "logout by sub")
	adaPhone.must("GET", "/api/v1/me", nil, 401, nil)
	adaTablet.must("GET", "/api/v1/me", nil, 401, nil)
	bob.must("GET", "/api/v1/me", nil, 200, nil)

	// Unknown providers are refused.
	eq(t, e.anon().do("POST", "/auth/nope/backchannel-logout", "logout_token=x", "Content-Type", "application/x-www-form-urlencoded").Status, http.StatusBadRequest, "unknown provider")
}

func TestOIDCSingleLogout(t *testing.T) {
	idp := newFakeIdP(t)
	e := oidcEnv(t, idp, withEnv("WFM_OIDC_CORP_SINGLE_LOGOUT", "true"))
	c := e.anon()
	idp.signIn(t, c, "corp", jwt.MapClaims{"sub": "ada", "email": "ada@example.com", "email_verified": true})
	var out struct {
		RedirectURL string `json:"redirectUrl"`
	}
	c.must("POST", "/api/v1/auth/logout", nil, 200, &out)
	u, err := url.Parse(out.RedirectURL)
	if err != nil || !strings.HasPrefix(out.RedirectURL, idp.URL+"/logout?") {
		t.Fatalf("redirect: %q", out.RedirectURL)
	}
	q := u.Query()
	eq(t, q.Get("client_id"), "wfm-client", "client_id")
	eq(t, q.Get("post_logout_redirect_uri"), e.Cfg.PublicURL+"/login", "back to the login page")
	if parts := strings.Split(q.Get("id_token_hint"), "."); len(parts) != 3 {
		t.Fatalf("id_token_hint: %q", q.Get("id_token_hint"))
	}
	c.must("GET", "/api/v1/me", nil, 401, nil)

	// Without single logout (or for sessions that did not come from the provider) it is a plain 204.
	plain := oidcEnv(t, idp)
	p := plain.anon()
	idp.signIn(t, p, "corp", jwt.MapClaims{"sub": "ada", "email": "ada@example.com", "email_verified": true})
	p.must("POST", "/api/v1/auth/logout", nil, 204, nil)
	dev := e.login("dev@example.com", "Dev")
	dev.must("POST", "/api/v1/auth/logout", nil, 204, nil)
}

func TestOIDCAccessTokensForTheAPI(t *testing.T) {
	idp := newFakeIdP(t)
	e := oidcEnv(t, idp, withEnv("WFM_OIDC_CORP_API_AUDIENCE", "wfm-api"), withEnv("WFM_OIDC_CORP_API_CLIENTS", "ci-pipeline=ci-bot@service.example"))
	admin := e.anon()
	idp.signIn(t, admin, "corp", jwt.MapClaims{"sub": "u-ada", "email": "ada@example.com", "email_verified": true, "name": "Ada"})
	admin.createProject("EXT", "kanban")
	bob := e.anon()
	idp.signIn(t, bob, "corp", jwt.MapClaims{"sub": "u-bob", "email": "bob@example.com", "email_verified": true, "name": "Bob"})
	var bot obj
	admin.must("POST", "/api/v1/admin/users", obj{"kind": "service", "displayName": "CI bot", "email": "ci-bot@service.example"}, 201, &bot)
	admin.must("POST", "/api/v1/projects/EXT/members", obj{"userId": str(bot, "id"), "roleKey": "member"}, 201, nil)

	now := time.Now()
	token := func(extra jwt.MapClaims) *Client {
		c := jwt.MapClaims{"iss": idp.URL, "aud": "wfm-api", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "typ": "Bearer", "sub": "u-bob", "scope": "openid wfm:read"}
		for k, v := range extra {
			if v == nil {
				delete(c, k)
				continue
			}
			c[k] = v
		}
		return e.withToken(idp.sign(c)) // sends no CSRF header

	}

	// A person's token, read-only: reads work, writes do not.
	var me obj
	token(nil).must("GET", "/api/v1/me", nil, 200, &me)
	eq(t, str(me, "email"), "bob@example.com", "acts as the person with that subject")
	token(nil).must("POST", "/api/v1/issues", obj{"projectKey": "EXT", "typeKey": "task", "summary": "nope"}, 403, nil)

	// wfm:write allows changes (no CSRF header needed), but not administration.
	writer := token(jwt.MapClaims{"scope": "wfm:read wfm:write"})
	writer.must("POST", "/api/v1/issues", obj{"projectKey": "EXT", "typeKey": "task", "summary": "from a pipeline"}, 201, nil)
	writer.must("GET", "/api/v1/admin/users", nil, 403, nil)
	token(jwt.MapClaims{"sub": "u-ada", "scope": "wfm:admin"}).must("GET", "/api/v1/admin/users", nil, 200, nil)
	writer.must("POST", "/api/v1/me/tokens", obj{"name": "x", "scopes": []string{"read"}, "expiresInDays": 1}, 403, nil)

	// A client in API_CLIENTS acts as its service account (client-credentials tokens).
	ci := token(jwt.MapClaims{"sub": "service-account-ci-pipeline", "azp": "ci-pipeline", "client_id": "ci-pipeline", "scope": "wfm:write"})
	var issue obj
	ci.must("POST", "/api/v1/issues", obj{"projectKey": "EXT", "typeKey": "task", "summary": "from CI"}, 201, &issue)
	eq(t, str(issue, "reporter", "kind"), "service", "service account")

	// Refused: sign-in ID tokens (client id as audience), ID/logout token types, expired tokens,
	// unknown subjects, other issuers, and tokens without a wfm scope.
	for name, c := range map[string]*Client{
		"id token audience": token(jwt.MapClaims{"aud": "wfm-client"}),
		"typ ID":            token(jwt.MapClaims{"typ": "ID"}),
		"logout token":      token(jwt.MapClaims{"typ": "Logout", "events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}}}),
		"expired":           token(jwt.MapClaims{"exp": now.Add(-time.Minute).Unix()}),
		"unknown subject":   token(jwt.MapClaims{"sub": "u-nobody"}),
		"other issuer":      token(jwt.MapClaims{"iss": "https://elsewhere.example"}),
		"unknown client":    token(jwt.MapClaims{"sub": "service-account-x", "client_id": "other-client"}),
	} {
		if r := c.do("GET", "/api/v1/me", nil); r.Status != http.StatusUnauthorized {
			t.Fatalf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	token(jwt.MapClaims{"scope": "openid profile"}).must("GET", "/api/v1/me", nil, 403, nil)

	// Deactivated people's tokens stop working at once.
	admin.must("PATCH", "/api/v1/admin/users/"+meID(bob), obj{"active": false}, 200, nil)
	token(nil).must("GET", "/api/v1/me", nil, 401, nil)
}
