// Package directory syncs people and groups from Google Workspace through the Admin SDK
// Directory API, using a service account with domain-wide delegation and read-only scopes.
package directory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/oauth2/google"

	"github.com/geoffo-dev/workflow-management/internal/config"
)

const defaultBaseURL = "https://admin.googleapis.com/admin/directory/v1"

var Scopes = []string{
	"https://www.googleapis.com/auth/admin.directory.user.readonly",
	"https://www.googleapis.com/auth/admin.directory.group.readonly",
	"https://www.googleapis.com/auth/admin.directory.group.member.readonly",
}

type User struct {
	ID           string `json:"id"`
	PrimaryEmail string `json:"primaryEmail"`
	Name         struct {
		GivenName  string `json:"givenName"`
		FamilyName string `json:"familyName"`
		FullName   string `json:"fullName"`
	} `json:"name"`
	Suspended         bool   `json:"suspended"`
	Archived          bool   `json:"archived"`
	OrgUnitPath       string `json:"orgUnitPath"`
	ThumbnailPhotoURL string `json:"thumbnailPhotoUrl"`
}

type Group struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Member struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Type   string `json:"type"` // USER, GROUP, CUSTOMER
	Status string `json:"status"`
}

// Client is the subset of the Directory API the sync uses.
type Client interface {
	ListUsers(ctx context.Context, query string) ([]User, error)
	ListGroups(ctx context.Context) ([]Group, error)
	ListMembers(ctx context.Context, groupKey string) ([]Member, error)
}

type HTTPClient struct {
	BaseURL  string
	Customer string
	HTTP     *http.Client
}

// NewClient builds an authenticated client from the configured service-account key.
func NewClient(ctx context.Context, cfg config.GoogleDirectory) (*HTTPClient, error) {
	key, err := loadKey(cfg.ServiceAccountKey)
	if err != nil {
		return nil, err
	}
	jwt, err := google.JWTConfigFromJSON(key, Scopes...)
	if err != nil {
		return nil, fmt.Errorf("service account key: %w", err)
	}
	jwt.Subject = cfg.AdminSubject // domain-wide delegation: act as this admin, read-only
	return &HTTPClient{BaseURL: defaultBaseURL, Customer: cfg.Customer, HTTP: jwt.Client(ctx)}, nil
}

// loadKey accepts the JSON itself, base64-encoded JSON, or a path to the file.
func loadKey(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "{") {
		return []byte(v), nil
	}
	if b, err := base64.StdEncoding.DecodeString(v); err == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "{") {
		return b, nil
	}
	b, err := os.ReadFile(v)
	if err != nil {
		return nil, fmt.Errorf("read service account key: %w", err)
	}
	return b, nil
}

func (c *HTTPClient) get(ctx context.Context, path string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("directory API %s: HTTP %d: %s", path, res.StatusCode, strings.TrimSpace(string(body[:min(len(body), 300)])))
	}
	return json.Unmarshal(body, out)
}

func (c *HTTPClient) ListUsers(ctx context.Context, query string) ([]User, error) {
	var all []User
	token := ""
	for {
		params := url.Values{"customer": {c.Customer}, "maxResults": {"500"}, "projection": {"basic"}}
		if query != "" {
			params.Set("query", query)
		}
		if token != "" {
			params.Set("pageToken", token)
		}
		var page struct {
			Users         []User `json:"users"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, "/users", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Users...)
		if page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
}

func (c *HTTPClient) ListGroups(ctx context.Context) ([]Group, error) {
	var all []Group
	token := ""
	for {
		params := url.Values{"customer": {c.Customer}, "maxResults": {"200"}}
		if token != "" {
			params.Set("pageToken", token)
		}
		var page struct {
			Groups        []Group `json:"groups"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := c.get(ctx, "/groups", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Groups...)
		if page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
}

func (c *HTTPClient) ListMembers(ctx context.Context, groupKey string) ([]Member, error) {
	var all []Member
	token := ""
	for {
		params := url.Values{"maxResults": {"200"}, "includeDerivedMembership": {"true"}}
		if token != "" {
			params.Set("pageToken", token)
		}
		var page struct {
			Members       []Member `json:"members"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := c.get(ctx, "/groups/"+url.PathEscape(groupKey)+"/members", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Members...)
		if page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
}
