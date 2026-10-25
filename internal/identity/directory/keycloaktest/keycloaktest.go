// Package keycloaktest is an in-memory Keycloak for tests: the client-credentials token endpoint
// and the parts of the Admin REST API that directory sync reads (users, groups, group children,
// group members, group-by-path), with first/max paging, Keycloak 23+ group listing (subgroups
// through /children, not nested), short-lived and revocable tokens, and fault injection.
//
//	kc := keycloaktest.New()
//	defer kc.Close()
//	ann := kc.AddUser(keycloaktest.User{Username: "ann", Email: "ann@example.com"})
//	staff := kc.AddGroup("", "staff")
//	kc.AddMembers(staff, ann)
//
// Point a client at kc.URL with realm kc.Realm and the credentials kc.ClientID / kc.ClientSecret.
package keycloaktest

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// User is a Keycloak user. Enabled and EmailVerified default to true in AddUser unless Disabled
// or Unverified are set.
type User struct {
	ID            string
	Username      string
	Email         string
	FirstName     string
	LastName      string
	Enabled       bool
	EmailVerified bool
	// ServiceAccountClientID marks a client's service-account user. Keycloak leaves these out of
	// the user list; set IncludeServiceAccounts to list them anyway.
	ServiceAccountClientID string

	// Disabled and Unverified are conveniences for AddUser.
	Disabled   bool
	Unverified bool
}

// Group is a Keycloak group; Path is "/parent/child".
type Group struct {
	ID          string
	Name        string
	Path        string
	Description string
	ParentID    string
	members     []string
}

// Server is the fake. Change its fields and contents between syncs to simulate Keycloak changes.
type Server struct {
	*httptest.Server
	Realm        string
	ClientID     string
	ClientSecret string

	// TokenTTL is the expires_in of issued tokens (default 5 minutes).
	TokenTTL time.Duration
	// MaxTokenUses makes a token invalid (401) after this many admin requests, like a token
	// that expired mid-run. 0 means no limit.
	MaxTokenUses int
	// Forbidden answers every admin request with 403, like a service account without the
	// realm-management roles.
	Forbidden bool
	// RejectTokens answers every admin request with 401, whatever the token.
	RejectTokens bool
	// LegacyGroups lists subgroups inline in subGroups and has no /children endpoint, like
	// Keycloak before 23.
	LegacyGroups bool
	// IncludeServiceAccounts lists service-account users in /users.
	IncludeServiceAccounts bool
	// Now is the fake's clock for token expiry (default time.Now).
	Now func() time.Time

	mu       sync.Mutex
	users    map[string]*User
	groups   map[string]*Group
	tokens   map[string]*token
	issued   int
	requests []string
}

type token struct {
	expires time.Time
	uses    int
}

// New starts a fake Keycloak with realm "wfm" and the sync client "workflow-sync".
func New() *Server {
	s := &Server{
		Realm: "wfm", ClientID: "workflow-sync", ClientSecret: "workflow-sync-secret",
		TokenTTL: 5 * time.Minute, Now: time.Now,
		users: map[string]*User{}, groups: map[string]*Group{}, tokens: map[string]*token{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// AddUser adds a user and returns its id (a new UUID unless u.ID is set).
func (s *Server) AddUser(u User) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	u.Enabled = !u.Disabled
	u.EmailVerified = !u.Unverified && u.Email != ""
	u.Username = strings.ToLower(u.Username)
	s.users[u.ID] = &u
	return u.ID
}

// UpdateUser changes a user in place (set Enabled / EmailVerified directly).
func (s *Server) UpdateUser(id string, fn func(*User)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.users[id]; u != nil {
		fn(u)
	}
}

// DeleteUser removes a user and their group memberships.
func (s *Server) DeleteUser(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, id)
	for _, g := range s.groups {
		g.members = slices.DeleteFunc(g.members, func(m string) bool { return m == id })
	}
}

// AddGroup adds a group under parentID ("" for a top-level group) and returns its id.
func (s *Server) AddGroup(parentID, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := &Group{ID: uuid.NewString(), Name: name, Path: "/" + name, ParentID: parentID}
	if p := s.groups[parentID]; p != nil {
		g.Path = p.Path + "/" + name
	}
	s.groups[g.ID] = g
	return g.ID
}

// SetGroupDescription sets a group's description.
func (s *Server) SetGroupDescription(id, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.groups[id]; g != nil {
		g.Description = description
	}
}

// DeleteGroup removes a group and its subgroups.
func (s *Server) DeleteGroup(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteGroup(id)
}

func (s *Server) deleteGroup(id string) {
	for _, g := range s.groups {
		if g.ParentID == id {
			s.deleteGroup(g.ID)
		}
	}
	delete(s.groups, id)
}

// AddMembers makes users direct members of a group.
func (s *Server) AddMembers(groupID string, userIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[groupID]
	for _, id := range userIDs {
		if g != nil && !slices.Contains(g.members, id) {
			g.members = append(g.members, id)
		}
	}
}

// RemoveMember removes a direct member from a group.
func (s *Server) RemoveMember(groupID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.groups[groupID]; g != nil {
		g.members = slices.DeleteFunc(g.members, func(m string) bool { return m == userID })
	}
}

// RevokeTokens invalidates every token issued so far, so the next admin request gets a 401.
func (s *Server) RevokeTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.tokens)
}

// TokensIssued counts successful token requests.
func (s *Server) TokensIssued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issued
}

// Requests returns the requests served so far ("GET /admin/realms/wfm/users?first=0&max=2").
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// ResetRequests clears the request log.
func (s *Server) ResetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		line += "?" + r.URL.RawQuery
	}
	s.requests = append(s.requests, line)

	if r.URL.Path == "/realms/"+s.Realm+"/protocol/openid-connect/token" && r.Method == http.MethodPost {
		s.token(w, r)
		return
	}
	prefix := "/admin/realms/" + s.Realm
	if !strings.HasPrefix(r.URL.Path, prefix+"/") || r.Method != http.MethodGet {
		if strings.HasPrefix(r.URL.Path, "/admin/realms/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Realm not found."})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Unable to find matching target resource method"})
		return
	}
	if s.RejectTokens || !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "HTTP 401 Unauthorized"})
		return
	}
	if s.Forbidden {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "HTTP 403 Forbidden"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	q := r.URL.Query()
	switch parts := strings.Split(strings.Trim(rest, "/"), "/"); {
	case rest == "/users":
		s.listUsers(w, q)
	case rest == "/groups":
		var top []*Group
		for _, g := range s.groups {
			if g.ParentID == "" {
				top = append(top, g)
			}
		}
		s.writeGroups(w, q, top)
	case strings.HasPrefix(rest, "/group-by-path/"):
		path := "/" + strings.Trim(strings.TrimPrefix(rest, "/group-by-path/"), "/")
		for _, g := range s.groups {
			if g.Path == path {
				writeJSON(w, http.StatusOK, s.groupRep(g))
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Group path does not exist"})
	case len(parts) == 3 && parts[0] == "groups" && parts[2] == "children" && !s.LegacyGroups:
		if s.groups[parts[1]] == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Could not find group by id"})
			return
		}
		var kids []*Group
		for _, g := range s.groups {
			if g.ParentID == parts[1] {
				kids = append(kids, g)
			}
		}
		s.writeGroups(w, q, kids)
	case len(parts) == 3 && parts[0] == "groups" && parts[2] == "members":
		g := s.groups[parts[1]]
		if g == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Could not find group by id"})
			return
		}
		var members []*User
		for _, id := range g.members {
			if u := s.users[id]; u != nil {
				members = append(members, u)
			}
		}
		slices.SortFunc(members, func(a, b *User) int { return strings.Compare(a.Username, b.Username) })
		first, size := paging(q)
		out := []map[string]any{}
		for _, u := range window(members, first, size) {
			out = append(out, userRep(u))
		}
		writeJSON(w, http.StatusOK, out)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Unable to find matching target resource method"})
	}
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type", "error_description": "Unsupported grant_type"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != s.ClientID || secret != s.ClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized_client", "error_description": "Invalid client or Invalid client credentials"})
		return
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	tok := base64.RawURLEncoding.EncodeToString(b)
	s.tokens[tok] = &token{expires: s.Now().Add(s.TokenTTL)}
	s.issued++
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": tok, "expires_in": int(s.TokenTTL.Seconds()), "refresh_expires_in": 0,
		"token_type": "Bearer", "not-before-policy": 0, "scope": "profile email",
	})
}

func (s *Server) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	t := s.tokens[strings.TrimPrefix(h, "Bearer ")]
	if t == nil || !s.Now().Before(t.expires) || (s.MaxTokenUses > 0 && t.uses >= s.MaxTokenUses) {
		return false
	}
	t.uses++
	return true
}

func (s *Server) listUsers(w http.ResponseWriter, q url.Values) {
	var all []*User
	for _, u := range s.users {
		if u.ServiceAccountClientID == "" || s.IncludeServiceAccounts {
			all = append(all, u)
		}
	}
	slices.SortFunc(all, func(a, b *User) int { return strings.Compare(a.Username, b.Username) })
	first, size := paging(q)
	out := []map[string]any{}
	for _, u := range window(all, first, size) {
		out = append(out, userRep(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) writeGroups(w http.ResponseWriter, q url.Values, groups []*Group) {
	slices.SortFunc(groups, func(a, b *Group) int { return strings.Compare(a.Name, b.Name) })
	first, size := paging(q)
	out := []map[string]any{}
	for _, g := range window(groups, first, size) {
		out = append(out, s.groupRep(g))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) groupRep(g *Group) map[string]any {
	var kids []*Group
	for _, c := range s.groups {
		if c.ParentID == g.ID {
			kids = append(kids, c)
		}
	}
	slices.SortFunc(kids, func(a, b *Group) int { return strings.Compare(a.Name, b.Name) })
	rep := map[string]any{"id": g.ID, "name": g.Name, "path": g.Path, "attributes": map[string]any{}, "realmRoles": []string{}, "clientRoles": map[string]any{}}
	if g.Description != "" {
		rep["description"] = g.Description
	}
	if g.ParentID != "" {
		rep["parentId"] = g.ParentID
	}
	subs := []map[string]any{}
	if s.LegacyGroups {
		// Keycloak < 23 nests the whole tree; 26 leaves subGroups empty, even in group-by-path.
		for _, c := range kids {
			subs = append(subs, s.groupRep(c))
		}
	}
	rep["subGroups"] = subs
	if !s.LegacyGroups {
		rep["subGroupCount"] = len(kids)
	}
	return rep
}

func userRep(u *User) map[string]any {
	rep := map[string]any{
		"id": u.ID, "username": u.Username, "enabled": u.Enabled, "emailVerified": u.EmailVerified,
		"createdTimestamp": 1700000000000, "totp": false, "disableableCredentialTypes": []string{},
		"requiredActions": []string{}, "notBefore": 0, "access": map[string]bool{"manage": false},
	}
	if u.Email != "" {
		rep["email"] = u.Email
	}
	if u.FirstName != "" {
		rep["firstName"] = u.FirstName
	}
	if u.LastName != "" {
		rep["lastName"] = u.LastName
	}
	if u.ServiceAccountClientID != "" {
		rep["serviceAccountClientId"] = u.ServiceAccountClientID
	}
	return rep
}

// paging reads first/max like Keycloak (max defaults to 100).
func paging(q url.Values) (first, size int) {
	first, size = 0, 100
	if v, err := strconv.Atoi(q.Get("first")); err == nil && v >= 0 {
		first = v
	}
	if v, err := strconv.Atoi(q.Get("max")); err == nil && v >= 0 {
		size = v
	}
	return first, size
}

func window[T any](all []T, first, size int) []T {
	if first >= len(all) {
		return nil
	}
	return all[first:min(len(all), first+size)]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
