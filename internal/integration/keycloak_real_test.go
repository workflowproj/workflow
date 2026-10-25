package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/geoffo-dev/workflow-management/internal/identity/directory"
)

// TestKeycloakRealm syncs from a real Keycloak running the development realm of the k3d setup
// (deploy/k3d/keycloak/realm-wfm.json). It is skipped unless WFM_TEST_KEYCLOAK_URL is set:
//
//	docker run -d --name kc -p 127.0.0.1:8180:8080 -e KC_BOOTSTRAP_ADMIN_USERNAME=admin \
//	  -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
//	  -v $PWD/deploy/k3d/keycloak/realm-wfm.json:/opt/keycloak/data/import/realm-wfm.json:ro \
//	  quay.io/keycloak/keycloak:26.4.7 start-dev --import-realm
//	WFM_TEST_KEYCLOAK_URL=http://127.0.0.1:8180 go test ./internal/integration -run TestKeycloakRealm
//
// It disables and re-enables bob through the Admin API (master realm admin/admin, or
// WFM_TEST_KEYCLOAK_ADMIN / WFM_TEST_KEYCLOAK_ADMIN_PASSWORD).
func TestKeycloakRealm(t *testing.T) {
	base := os.Getenv("WFM_TEST_KEYCLOAK_URL")
	if base == "" {
		t.Skip("WFM_TEST_KEYCLOAK_URL is not set")
	}
	e := newEnv(t,
		withEnv("WFM_OIDC_PROVIDERS", "keycloak"), withEnv("WFM_OIDC_KEYCLOAK_ISSUER", base+"/realms/wfm"),
		withEnv("WFM_OIDC_KEYCLOAK_CLIENT_ID", "workflow"), withEnv("WFM_OIDC_KEYCLOAK_CLIENT_SECRET", "workflow-dev-client-secret"),
		withEnv("WFM_KEYCLOAK_ENABLED", "true"), withEnv("WFM_KEYCLOAK_URL", base), withEnv("WFM_KEYCLOAK_REALM", "wfm"),
		withEnv("WFM_KEYCLOAK_CLIENT_ID", "workflow-sync"), withEnv("WFM_KEYCLOAK_CLIENT_SECRET", "workflow-sync-dev-secret"),
		withEnv("WFM_KEYCLOAK_SYNC_GROUP", "/workflow-users"), withEnv("WFM_KEYCLOAK_GROUP_PREFIX", "wfm-"),
		withEnv("WFM_BOOTSTRAP_ADMINS", "alice@example.com"),
	)
	e.keycloakSyncer().Client.PageSize = 1 // page through everything
	admin := e.login("admin@example.com", "Admin")

	run := syncNow(admin)
	t.Logf("first run: %v %v", run["stats"], run["error"])
	wantStats(t, run, map[string]int{"usersCreated": 2, "users": 2, "groupsCreated": 3, "groups": 3, "membershipsAdded": 2, "memberships": 2})
	alice, bob := e.accountByEmail("alice@example.com"), e.accountByEmail("bob@example.com")
	if alice == nil || alice.Name != "Alice Anders" || alice.OrgRole != "admin" || bob == nil || bob.Source != "keycloak" {
		t.Fatalf("alice = %+v, bob = %+v", alice, bob)
	}
	if e.accountByEmail("carol@example.com") != nil || e.accountByEmail("dave@example.com") != nil {
		t.Fatal("carol (outside the access group) or dave (disabled) was provisioned")
	}
	groups := fmt.Sprint(e.keycloakGroups())
	if groups != fmt.Sprint(map[string][]string{"wfm-admins": {"alice@example.com"}, "wfm-engineering": {}, "wfm-engineering/wfm-platform": {"bob@example.com"}}) {
		t.Fatalf("groups = %s", groups)
	}
	before := e.rowCounts()
	wantStats(t, syncNow(admin), map[string]int{"users": 2, "groups": 3, "memberships": 2})
	if after := e.rowCounts(); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("re-run changed rows: %v -> %v", before, after)
	}

	// Disable bob in Keycloak, sync, enable him again.
	kcAdmin := keycloakAdmin(t, base)
	var found []obj
	kcAdmin("GET", "/admin/realms/wfm/users?exact=true&username=bob", nil, &found)
	if len(found) != 1 {
		t.Fatalf("bob in keycloak: %v", found)
	}
	bobID := str(found[0], "id")
	eq(t, e.linkedTo(bobID), bob.ID, "bob linked by Keycloak user id")
	bobC := e.login("bob@example.com", "")
	kcAdmin("PUT", "/admin/realms/wfm/users/"+bobID, obj{"enabled": false}, nil)
	t.Cleanup(func() { kcAdmin("PUT", "/admin/realms/wfm/users/"+bobID, obj{"enabled": true}, nil) })
	wantStats(t, syncNow(admin), map[string]int{"usersDeactivated": 1, "users": 2, "groups": 3, "memberships": 2})
	bobC.must("GET", "/api/v1/me", nil, 401, nil)
	kcAdmin("PUT", "/admin/realms/wfm/users/"+bobID, obj{"enabled": true}, nil)
	wantStats(t, syncNow(admin), map[string]int{"usersReactivated": 1, "users": 2, "groups": 3, "memberships": 2})
	e.login("bob@example.com", "").must("GET", "/api/v1/me", nil, 200, nil)

	// Out of the access group: deactivated (confirmed through his own groups), then back.
	var access obj
	kcAdmin("GET", "/admin/realms/wfm/group-by-path/workflow-users", nil, &access)
	membership := "/admin/realms/wfm/users/" + bobID + "/groups/" + str(access, "id")
	kcAdmin("DELETE", membership, nil, nil)
	t.Cleanup(func() { kcAdmin("PUT", membership, nil, nil) })
	wantStats(t, syncNow(admin), map[string]int{"usersDeactivated": 1, "users": 1, "groups": 3, "membershipsRemoved": 1, "memberships": 1})
	kcAdmin("PUT", membership, nil, nil)
	wantStats(t, syncNow(admin), map[string]int{"usersReactivated": 1, "users": 2, "groups": 3, "membershipsAdded": 1, "memberships": 2})

	// Created in Keycloak, then deleted there: marked deleted here.
	kcAdmin("POST", "/admin/realms/wfm/users", obj{
		"username": "erin", "email": "erin@example.com", "emailVerified": true, "enabled": true,
		"firstName": "Erin", "lastName": "Evans", "groups": []string{"/workflow-users"},
	}, nil)
	kcAdmin("GET", "/admin/realms/wfm/users?exact=true&username=erin", nil, &found)
	erinID := str(found[0], "id")
	t.Cleanup(func() { kcAdmin("DELETE", "/admin/realms/wfm/users/"+erinID, nil, nil) })
	wantStats(t, syncNow(admin), map[string]int{"usersCreated": 1, "users": 3, "groups": 3, "memberships": 2})
	kcAdmin("DELETE", "/admin/realms/wfm/users/"+erinID, nil, nil)
	wantStats(t, syncNow(admin), map[string]int{"usersDeleted": 1, "users": 2, "groups": 3, "memberships": 2})
	if erin := e.accountByEmail("erin@example.com"); erin == nil || !erin.Deleted {
		t.Fatalf("erin = %+v, want deleted", erin)
	}

	// A client without the realm-management roles (workflow-ci only calls Workflow's API).
	sync := e.keycloakSyncer()
	cfg := sync.Cfg
	cfg.ClientID, cfg.ClientSecret = "workflow-ci", "workflow-ci-dev-secret"
	sync.Client = directory.NewKeycloakClient(cfg)
	run = syncNow(admin)
	msg, _ := run["error"].(string)
	t.Logf("without realm-management roles: %s", msg)
	if str(run, "status") != "failed" || !strings.Contains(msg, "HTTP 403") || !strings.Contains(msg, "view-users and query-groups") {
		t.Fatalf("run = %v", run)
	}
}

// keycloakAdmin returns a caller of the Keycloak Admin API as the master realm's admin.
func keycloakAdmin(t *testing.T, base string) func(method, path string, body, out any) {
	user, pass := os.Getenv("WFM_TEST_KEYCLOAK_ADMIN"), os.Getenv("WFM_TEST_KEYCLOAK_ADMIN_PASSWORD")
	if user == "" {
		user, pass = "admin", "admin"
	}
	res, err := http.PostForm(base+"/realms/master/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {user}, "password": {pass},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&tok)
	res.Body.Close()
	if tok.AccessToken == "" {
		t.Fatalf("keycloak admin token: HTTP %d", res.StatusCode)
	}
	return func(method, path string, body, out any) {
		t.Helper()
		var rdr *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		} else {
			rdr = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, base+path, rdr)
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 && (method != "DELETE" || res.StatusCode != http.StatusNotFound) {
			t.Fatalf("keycloak admin %s %s: HTTP %d", method, path, res.StatusCode)
		}
		if out != nil {
			_ = json.NewDecoder(res.Body).Decode(out)
		}
	}
}
