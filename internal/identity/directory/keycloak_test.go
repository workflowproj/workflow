package directory_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/identity/directory"
	"github.com/geoffo-dev/workflow-management/internal/identity/directory/keycloaktest"
)

// clock is a settable time shared by the fake and the client.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClient(t *testing.T) (*keycloaktest.Server, *directory.KeycloakClient, *clock) {
	t.Helper()
	kc := keycloaktest.New()
	t.Cleanup(kc.Close)
	clk := &clock{t: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	kc.Now = clk.now
	c := directory.NewKeycloakClient(config.Keycloak{URL: kc.URL + "/", Realm: kc.Realm, ClientID: kc.ClientID, ClientSecret: kc.ClientSecret})
	c.Now = clk.now
	return kc, c, clk
}

func usernames(us []directory.KeycloakUser) []string {
	var out []string
	for _, u := range us {
		out = append(out, u.Username)
	}
	return out
}

func TestKeycloakClientTokens(t *testing.T) {
	kc, c, clk := newClient(t)
	kc.TokenTTL = time.Minute
	kc.AddUser(keycloaktest.User{Username: "ann", Email: "ann@example.com"})
	ctx := context.Background()

	list := func() {
		t.Helper()
		us, err := c.ListUsers(ctx)
		if err != nil || len(us) != 1 {
			t.Fatalf("users = %v, %v", us, err)
		}
	}
	list()
	list()
	if n := kc.TokensIssued(); n != 1 {
		t.Fatalf("tokens = %d, want the token reused", n)
	}
	// Renewed shortly before it expires (60s token: renewed after 48s).
	clk.add(50 * time.Second)
	list()
	if n := kc.TokensIssued(); n != 2 {
		t.Fatalf("tokens = %d, want a renewal before expiry", n)
	}
	// Revoked (or expired early) mid-run: the 401 renews the token and the request is retried once.
	kc.RevokeTokens()
	list()
	if n := kc.TokensIssued(); n != 3 {
		t.Fatalf("tokens = %d, want a renewal after the 401", n)
	}
	// A token that keeps failing is an error, not a loop.
	kc.RejectTokens = true
	if _, err := c.ListUsers(ctx); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v, want HTTP 401", err)
	}
}

func TestKeycloakClientPaging(t *testing.T) {
	kc, c, _ := newClient(t)
	c.PageSize = 2
	for _, name := range []string{"eve", "ann", "dan", "bob", "cat"} {
		kc.AddUser(keycloaktest.User{Username: name, Email: name + "@example.com"})
	}
	us, err := c.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(usernames(us), ","); got != "ann,bob,cat,dan,eve" {
		t.Fatalf("users = %s", got)
	}
	var pages []string
	for _, r := range kc.Requests() {
		if strings.Contains(r, "/users?") {
			pages = append(pages, r[strings.Index(r, "first="):])
		}
	}
	if got := strings.Join(pages, " "); got != "first=0&max=2 first=2&max=2 first=4&max=2" {
		t.Fatalf("pages = %s", got)
	}
	// A full last page needs one more (empty) page to know it was the last.
	kc.DeleteUser(us[4].ID)
	kc.ResetRequests()
	if us, err = c.ListUsers(context.Background()); err != nil || len(us) != 4 || len(kc.Requests()) != 3 {
		t.Fatalf("users = %d, requests = %v, err = %v", len(us), kc.Requests(), err)
	}
}

func groupPaths(gs []directory.KeycloakGroup) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Path)
	}
	return out
}

func TestKeycloakClientGroups(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		kc, c, _ := newClient(t)
		kc.LegacyGroups = legacy
		c.PageSize = 2
		eng := kc.AddGroup("", "engineering")
		platform := kc.AddGroup(eng, "platform")
		kc.AddGroup(platform, "api")
		kc.AddGroup(eng, "mobile")
		kc.AddGroup(eng, "web")
		kc.AddGroup("", "sales")
		ann := kc.AddUser(keycloaktest.User{Username: "ann", Email: "ann@example.com"})
		bob := kc.AddUser(keycloaktest.User{Username: "bob", Email: "bob@example.com"})
		cat := kc.AddUser(keycloaktest.User{Username: "cat", Email: "cat@example.com"})
		kc.AddMembers(platform, ann, bob, cat)
		ctx := context.Background()

		gs, err := c.ListGroups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := "/engineering /engineering/mobile /engineering/platform /engineering/platform/api /engineering/web /sales"
		if got := strings.Join(groupPaths(gs), " "); got != want {
			t.Fatalf("legacy=%v: groups = %s", legacy, got)
		}
		children := slices.ContainsFunc(kc.Requests(), func(r string) bool { return strings.Contains(r, "/children") })
		if children == legacy {
			t.Fatalf("legacy=%v: /children requested = %v", legacy, children)
		}

		g, err := c.GroupByPath(ctx, "/engineering")
		if err != nil || g.ID != eng {
			t.Fatalf("group-by-path = %+v, %v", g, err)
		}
		subs, err := c.Subgroups(ctx, g)
		if err != nil || strings.Join(groupPaths(subs), " ") != "/engineering/mobile /engineering/platform /engineering/platform/api /engineering/web" {
			t.Fatalf("legacy=%v: subgroups = %v, %v", legacy, groupPaths(subs), err)
		}
		ids, err := c.ListMemberIDs(ctx, platform)
		if err != nil || len(ids) != 3 {
			t.Fatalf("members = %v, %v", ids, err)
		}
		if _, err := c.GroupByPath(ctx, "/nope"); err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("missing group: err = %v", err)
		}
	}
}

func TestKeycloakClientErrors(t *testing.T) {
	kc, c, _ := newClient(t)
	ctx := context.Background()

	kc.Forbidden = true
	_, err := c.ListUsers(ctx)
	if err == nil || !strings.Contains(err.Error(), "view-users and query-groups") || !strings.Contains(err.Error(), `"workflow-sync"`) {
		t.Fatalf("403: err = %v", err)
	}
	kc.Forbidden = false

	bad := directory.NewKeycloakClient(config.Keycloak{URL: kc.URL, Realm: "nope", ClientID: kc.ClientID, ClientSecret: kc.ClientSecret})
	if _, err := bad.ListUsers(ctx); err == nil || !strings.Contains(err.Error(), `is "nope" the realm name`) {
		t.Fatalf("unknown realm: err = %v", err)
	}
	bad = directory.NewKeycloakClient(config.Keycloak{URL: kc.URL, Realm: kc.Realm, ClientID: kc.ClientID, ClientSecret: "s3cr3t-wrong"})
	if _, err := bad.ListUsers(ctx); err == nil || !strings.Contains(err.Error(), "WFM_KEYCLOAK_CLIENT_SECRET") || strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("bad secret: err = %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.ListUsers(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
}
