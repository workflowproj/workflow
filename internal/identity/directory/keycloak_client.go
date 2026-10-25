package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/config"
)

// KeycloakUser is the part of Keycloak's UserRepresentation the sync reads.
type KeycloakUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Enabled       bool   `json:"enabled"`
	// ServiceAccountClientID is set on a client's service-account user (never provisioned).
	ServiceAccountClientID string `json:"serviceAccountClientId"`
}

// KeycloakGroup is the part of Keycloak's GroupRepresentation the sync reads.
type KeycloakGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
	// SubGroupCount is set by Keycloak 23+, which no longer nests the tree in SubGroups (only
	// group-by-path still does); subgroups are read from /groups/{id}/children instead.
	SubGroupCount *int            `json:"subGroupCount"`
	SubGroups     []KeycloakGroup `json:"subGroups"`
}

// errKeycloakNotFound is a 404 from the Admin REST API.
var errKeycloakNotFound = errors.New("not found")

// KeycloakClient reads a realm through the Keycloak Admin REST API with a client-credentials
// token of a confidential client whose service account has the realm-management roles
// view-users and query-groups. Keycloak is configured by an administrator and usually runs on a
// private address inside the cluster, so requests do not go through the outbound guard that
// protects user-supplied URLs.
type KeycloakClient struct {
	BaseURL      string
	Realm        string
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
	// PageSize is the max of each paged request (default 100).
	PageSize int
	// Now is the clock for token expiry (tests).
	Now func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewKeycloakClient builds a client for the configured realm with conservative timeouts.
func NewKeycloakClient(cfg config.Keycloak) *KeycloakClient {
	return &KeycloakClient{
		BaseURL: strings.TrimRight(cfg.URL, "/"), Realm: cfg.Realm, ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		HTTP: newKeycloakHTTPClient(), PageSize: 100, Now: time.Now,
	}
}

func newKeycloakHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 10 * time.Second
	t.ResponseHeaderTimeout = 60 * time.Second
	return &http.Client{
		Transport: t,
		// One page of a large realm can take a while; the job's own timeout bounds the run.
		Timeout: 2 * time.Minute,
		// The token endpoint and the Admin API never redirect; refuse rather than follow.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *KeycloakClient) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *KeycloakClient) pageSize() int {
	if c.PageSize > 0 {
		return c.PageSize
	}
	return 100
}

// accessToken returns a cached token, or a new one when it is missing, about to expire, or
// refresh is set (after a 401).
func (c *KeycloakClient) accessToken(ctx context.Context, refresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !refresh && c.token != "" && c.now().Before(c.expiry) {
		return c.token, nil
	}
	c.token = ""
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.ClientID}, "client_secret": {c.ClientSecret}}
	endpoint := c.BaseURL + "/realms/" + url.PathEscape(c.Realm) + "/protocol/openid-connect/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	issued := c.now()
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("keycloak token request: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		msg := strings.TrimSpace(e.Error + ": " + e.Description)
		if e.Error == "" {
			msg = snippet(body)
		}
		hint := ""
		if res.StatusCode == http.StatusUnauthorized || e.Error == "unauthorized_client" || e.Error == "invalid_client" {
			hint = " (check WFM_KEYCLOAK_CLIENT_ID and WFM_KEYCLOAK_CLIENT_SECRET, and that the client is confidential with service accounts enabled)"
		}
		if res.StatusCode == http.StatusNotFound {
			hint = fmt.Sprintf(" (is %q the realm name and %s Keycloak's base URL?)", c.Realm, c.BaseURL)
		}
		return "", fmt.Errorf("keycloak token request: HTTP %d: %s%s", res.StatusCode, msg, hint)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.AccessToken == "" {
		return "", fmt.Errorf("keycloak token request: no access token in the response")
	}
	ttl := time.Duration(t.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	// Renew a little early so a token does not expire between the check and the request.
	c.token, c.expiry = t.AccessToken, issued.Add(ttl-min(30*time.Second, ttl/5))
	return c.token, nil
}

// get reads one Admin API resource below /admin/realms/{realm}. A 401 renews the token and
// retries once (the token was revoked or expired early).
func (c *KeycloakClient) get(ctx context.Context, path string, params url.Values, out any) error {
	u := c.BaseURL + "/admin/realms/" + url.PathEscape(c.Realm) + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.accessToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		res, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("keycloak %s: %w", path, err)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("keycloak %s: %w", path, err)
		}
		switch res.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("keycloak %s: unexpected response: %w", path, err)
			}
			return nil
		case http.StatusUnauthorized:
			if attempt == 0 {
				continue
			}
			return fmt.Errorf("keycloak %s: HTTP 401: Keycloak does not accept the sync client's token", path)
		case http.StatusForbidden:
			return fmt.Errorf("keycloak %s: HTTP 403: the service account of client %q needs the realm-management roles view-users and query-groups (Clients → %s → Service accounts roles → Assign role → realm-management)", path, c.ClientID, c.ClientID)
		case http.StatusNotFound:
			return fmt.Errorf("keycloak %s: %w (HTTP 404: %s)", path, errKeycloakNotFound, snippet(body))
		default:
			return fmt.Errorf("keycloak %s: HTTP %d: %s", path, res.StatusCode, snippet(body))
		}
	}
}

// maxPages bounds a paged listing (a server that ignored first would otherwise loop forever).
const maxPages = 100_000

// pageAll reads every page of a list endpoint: first=0,max=N, first=N,... until a short page.
func pageAll[T any](ctx context.Context, c *KeycloakClient, path string, params url.Values) ([]T, error) {
	size := c.pageSize()
	var all []T
	for page := 0; page < maxPages; page++ {
		p := url.Values{}
		for k, v := range params {
			p[k] = v
		}
		p.Set("first", strconv.Itoa(page*size))
		p.Set("max", strconv.Itoa(size))
		var items []T
		if err := c.get(ctx, path, p, &items); err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < size {
			return all, nil
		}
	}
	return nil, fmt.Errorf("keycloak %s: more than %d pages", path, maxPages)
}

// ListUsers returns every user in the realm (Keycloak leaves service accounts out).
func (c *KeycloakClient) ListUsers(ctx context.Context) ([]KeycloakUser, error) {
	return pageAll[KeycloakUser](ctx, c, "/users", url.Values{"briefRepresentation": {"false"}})
}

// GroupByPath finds a group by its path ("/parent/child").
func (c *KeycloakClient) GroupByPath(ctx context.Context, path string) (KeycloakGroup, error) {
	var segs []string
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		segs = append(segs, url.PathEscape(s))
	}
	var g KeycloakGroup
	err := c.get(ctx, "/group-by-path/"+strings.Join(segs, "/"), nil, &g)
	return g, err
}

// ListGroups returns every group in the realm, parents before their subgroups.
func (c *KeycloakClient) ListGroups(ctx context.Context) ([]KeycloakGroup, error) {
	top, err := pageAll[KeycloakGroup](ctx, c, "/groups", url.Values{"briefRepresentation": {"false"}})
	if err != nil {
		return nil, err
	}
	var out []KeycloakGroup
	seen := map[string]bool{}
	for _, g := range top {
		if err := c.walk(ctx, g, "", seen, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Subgroups returns every group below g (children, grandchildren, ...).
func (c *KeycloakClient) Subgroups(ctx context.Context, g KeycloakGroup) ([]KeycloakGroup, error) {
	var out []KeycloakGroup
	seen := map[string]bool{g.ID: true}
	kids, err := c.children(ctx, g)
	if err != nil {
		return nil, err
	}
	for _, k := range kids {
		if err := c.walk(ctx, k, groupPath(g, ""), seen, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *KeycloakClient) walk(ctx context.Context, g KeycloakGroup, parentPath string, seen map[string]bool, out *[]KeycloakGroup) error {
	if seen[g.ID] {
		return nil
	}
	seen[g.ID] = true
	g.Path = groupPath(g, parentPath)
	*out = append(*out, g)
	kids, err := c.children(ctx, g)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := c.walk(ctx, k, g.Path, seen, out); err != nil {
			return err
		}
	}
	return nil
}

// children lists a group's direct subgroups. Keycloak 23+ reports subGroupCount and serves the
// subgroups from /groups/{id}/children (its listings no longer nest them, or only some);
// older versions nest the whole tree in subGroups and have no /children.
func (c *KeycloakClient) children(ctx context.Context, g KeycloakGroup) ([]KeycloakGroup, error) {
	switch {
	case g.SubGroupCount == nil:
		return g.SubGroups, nil
	case *g.SubGroupCount == 0:
		return nil, nil
	case len(g.SubGroups) == *g.SubGroupCount:
		return g.SubGroups, nil // already complete (group-by-path)
	}
	return pageAll[KeycloakGroup](ctx, c, "/groups/"+url.PathEscape(g.ID)+"/children", url.Values{"briefRepresentation": {"false"}})
}

func groupPath(g KeycloakGroup, parentPath string) string {
	if g.Path != "" {
		return g.Path
	}
	return parentPath + "/" + g.Name
}

// ListMemberIDs returns the ids of a group's direct members.
func (c *KeycloakClient) ListMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	members, err := pageAll[KeycloakUser](ctx, c, "/groups/"+url.PathEscape(groupID)+"/members", url.Values{"briefRepresentation": {"true"}})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// snippet is the start of an error response, on one line.
func snippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body[:min(len(body), 200)])), " ")
	if s == "" {
		return "(empty response)"
	}
	return s
}
