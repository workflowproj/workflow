package integration

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/geoffo-dev/workflow-management/internal/identity/directory"
	"github.com/geoffo-dev/workflow-management/internal/identity/directory/keycloaktest"
)

// ---------------------------------------------------------------------------
// Keycloak directory sync, against keycloaktest (an in-memory Keycloak Admin REST API).

type kcUser = keycloaktest.User

// keycloakEnv runs the app with Keycloak sign-in (provider "keycloak") and Keycloak directory sync
// pointed at the fake.
func keycloakEnv(t *testing.T, kc *keycloaktest.Server, extra ...envOption) *Env {
	t.Helper()
	opts := []envOption{
		withEnv("WFM_OIDC_PROVIDERS", "keycloak"), withEnv("WFM_OIDC_KEYCLOAK_ISSUER", kc.URL+"/realms/"+kc.Realm),
		withEnv("WFM_OIDC_KEYCLOAK_CLIENT_ID", "workflow"), withEnv("WFM_OIDC_KEYCLOAK_CLIENT_SECRET", "workflow-secret"),
		withEnv("WFM_KEYCLOAK_ENABLED", "true"), withEnv("WFM_KEYCLOAK_URL", kc.URL+"/"), withEnv("WFM_KEYCLOAK_REALM", kc.Realm),
		withEnv("WFM_KEYCLOAK_CLIENT_ID", kc.ClientID), withEnv("WFM_KEYCLOAK_CLIENT_SECRET", kc.ClientSecret),
	}
	return newEnv(t, append(opts, extra...)...)
}

func (e *Env) keycloakSyncer() *directory.KeycloakSyncer {
	e.t.Helper()
	s, ok := e.App.Directory.(*directory.KeycloakSyncer)
	if !ok {
		e.t.Fatalf("directory sync = %T, want Keycloak", e.App.Directory)
	}
	return s
}

// syncNow presses "Run now", runs the queued job and returns the newest run.
func syncNow(admin *Client) obj {
	admin.e.t.Helper()
	admin.must("POST", "/api/v1/admin/directory-sync/run", nil, 202, nil)
	admin.e.DrainJobs()
	var prov struct {
		DirectorySync obj `json:"directorySync"`
	}
	admin.get("/api/v1/admin/provisioning", &prov)
	runs, _ := prov.DirectorySync["runs"].([]any)
	if len(runs) == 0 {
		admin.e.t.Fatal("no directory sync runs")
	}
	return runs[0].(obj)
}

// runStats returns a run's stats as name -> count.
func runStats(run obj) map[string]int {
	out := map[string]int{}
	st, _ := run["stats"].(obj)
	for k, v := range st {
		n, _ := v.(float64)
		out[k] = int(n)
	}
	return out
}

func wantStats(t *testing.T, run obj, want map[string]int) {
	t.Helper()
	if str(run, "status") != "succeeded" {
		t.Fatalf("run %s: %v", str(run, "status"), run["error"])
	}
	if got := runStats(run); !maps.Equal(got, want) {
		t.Fatalf("stats = %v\nwant    %v", got, want)
	}
}

type account struct {
	ID, Source, Email, Name, OrgRole string
	Active, Deleted                  bool
}

// accountByID / accountByEmail read an account straight from the database (deleted ones too).
func (e *Env) accountWhere(cond string, arg any) *account {
	e.t.Helper()
	var a account
	err := e.Pool.QueryRow(context.Background(), `SELECT id::text, source, email, display_name, org_role, active, deleted_at IS NOT NULL
		FROM users WHERE `+cond, arg).Scan(&a.ID, &a.Source, &a.Email, &a.Name, &a.OrgRole, &a.Active, &a.Deleted)
	if err != nil {
		return nil
	}
	return &a
}

func (e *Env) accountByEmail(email string) *account { return e.accountWhere("email = $1", email) }
func (e *Env) accountByID(id string) *account       { return e.accountWhere("id = $1::uuid", id) }

// linkedTo returns the account linked to a Keycloak user id ("" when none).
func (e *Env) linkedTo(subject string) string {
	var id string
	_ = e.Pool.QueryRow(context.Background(), `SELECT user_id::text FROM user_identities WHERE provider = 'keycloak' AND subject = $1`, subject).Scan(&id)
	return id
}

// keycloakGroups maps each Keycloak-synced group's name to its members' emails.
func (e *Env) keycloakGroups() map[string][]string {
	e.t.Helper()
	rows, err := e.Pool.Query(context.Background(), `SELECT g.display_name, coalesce(u.email, '') FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id LEFT JOIN users u ON u.id = gm.user_id
		WHERE g.source = 'keycloak' ORDER BY 1, 2`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var g, email string
		if err := rows.Scan(&g, &email); err != nil {
			e.t.Fatal(err)
		}
		out[g] = append(out[g], email)
		if email == "" {
			out[g] = []string{}
		}
	}
	return out
}

func (e *Env) rowCounts() map[string]int {
	e.t.Helper()
	out := map[string]int{}
	for _, table := range []string{"users", "user_identities", "groups", "group_members", "sessions", "audit_events WHERE action <> 'directory.sync' AND action <> 'directory.sync_requested'"} {
		var n int
		if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			e.t.Fatal(err)
		}
		out[strings.Fields(table)[0]] = n
	}
	return out
}

func TestKeycloakDirectorySync(t *testing.T) {
	kc := keycloaktest.New()
	t.Cleanup(kc.Close)
	kc.MaxTokenUses = 5              // tokens stop working mid-run: the sync renews them
	kc.IncludeServiceAccounts = true // and service-account users are never provisioned

	e := keycloakEnv(t, kc,
		withEnv("WFM_KEYCLOAK_SYNC_GROUP", "workflow-users"), withEnv("WFM_KEYCLOAK_GROUP_PREFIX", "Team-"),
		withEnv("WFM_BOOTSTRAP_ADMINS", "boss@example.com"),
	)
	e.keycloakSyncer().Client.PageSize = 2 // every listing spans several pages
	admin := e.login("admin@example.com", "Admin")

	// Accounts that exist before the first sync: Dora (development login) is matched by email
	// and taken over; Ivy is already linked to her Keycloak user under an old address; Sam is
	// managed by SCIM and must be left alone.
	dora := e.login("dora@example.com", "Dora (dev)")
	ivyOld := e.login("ivy.old@example.com", "Ivy")
	var conn obj
	admin.must("POST", "/api/v1/admin/scim-connections", obj{"name": "Okta"}, 201, &conn)
	e.scim(str(conn, "token")).call("POST", "/Users", obj{
		"userName": "sam@example.com", "emails": []obj{{"value": "sam@example.com"}}, "displayName": "Sam (SCIM)", "active": true,
	}, 201)

	access := kc.AddGroup("", "workflow-users")
	contractors := kc.AddGroup(access, "contractors") // members of subgroups of the access group are in scope
	platform := kc.AddGroup("", "team-platform")
	api := kc.AddGroup(platform, "team-api")
	backend := kc.AddGroup(platform, "backend") // no prefix: not mirrored
	other := kc.AddGroup("", "other")           // not mirrored, but its prefixed subgroup is
	hidden := kc.AddGroup(other, "team-hidden")
	kc.SetGroupDescription(platform, "Platform engineering")

	ann := kc.AddUser(kcUser{Username: "ann", Email: "Ann@Example.com", FirstName: "Ann", LastName: "Archer"})
	ben := kc.AddUser(kcUser{Username: "ben", Email: "ben@example.com", FirstName: "Ben"})
	cat := kc.AddUser(kcUser{Username: "cat", Email: "cat@example.com"}) // not in the access group
	doraK := kc.AddUser(kcUser{Username: "dora", Email: "dora@example.com", FirstName: "Dora", LastName: "Diaz"})
	ivy := kc.AddUser(kcUser{Username: "ivy", Email: "ivy@example.com", FirstName: "Ivy", LastName: "Ives"})
	sam := kc.AddUser(kcUser{Username: "sam", Email: "sam@example.com", FirstName: "Samuel"})
	una := kc.AddUser(kcUser{Username: "una", Email: "una@example.com", Unverified: true})
	dis := kc.AddUser(kcUser{Username: "dis", Email: "dis@example.com", Disabled: true})
	boss := kc.AddUser(kcUser{Username: "boss", Email: "boss@example.com"})
	robot := kc.AddUser(kcUser{Username: "service-account-ci", ServiceAccountClientID: "ci"})
	kc.AddMembers(access, ann, doraK, ivy, sam, una, dis, boss, robot)
	kc.AddMembers(contractors, ben)
	kc.AddMembers(platform, ann, cat)
	kc.AddMembers(api, ben)
	kc.AddMembers(backend, ann)
	kc.AddMembers(hidden, doraK)
	if _, err := e.Pool.Exec(context.Background(), `INSERT INTO user_identities (user_id, provider, subject) VALUES ($1, 'keycloak', $2)`, ivyOld.id(), ivy); err != nil {
		t.Fatal(err)
	}

	// First sync.
	wantStats(t, syncNow(admin), map[string]int{
		"usersCreated": 3, "usersUpdated": 2, "usersSkipped": 2, "users": 5,
		"groupsCreated": 3, "groups": 3, "membershipsAdded": 3, "memberships": 3,
	})
	if kc.TokensIssued() < 3 {
		t.Fatalf("tokens issued = %d, want renewals during the run", kc.TokensIssued())
	}
	reqs := strings.Join(kc.Requests(), "\n")
	for _, want := range []string{
		"/users?briefRepresentation=false&first=8&max=2",            // paged
		"/group-by-path/workflow-users",                             // access group
		"/groups/" + access + "/children?briefRepresentation=false", // its subgroups
		"/groups/" + contractors + "/members?briefRepresentation=true&first=0&max=2",
		"/groups/" + platform + "/children", "/groups/" + other + "/children", // nested groups (Keycloak 23+)
	} {
		if !strings.Contains(reqs, want) {
			t.Fatalf("no request for %s in\n%s", want, reqs)
		}
	}

	a := e.accountByEmail("ann@example.com")
	if a == nil || a.Source != "keycloak" || a.Name != "Ann Archer" || !a.Active || e.linkedTo(ann) != a.ID {
		t.Fatalf("ann = %+v (linked %q)", a, e.linkedTo(ann))
	}
	if b := e.accountByEmail("ben@example.com"); b == nil || b.Source != "keycloak" || b.Name != "Ben" || e.linkedTo(ben) != b.ID {
		t.Fatalf("ben = %+v", b)
	}
	for _, email := range []string{"cat@example.com", "una@example.com", "dis@example.com"} {
		if e.accountByEmail(email) != nil {
			t.Fatalf("%s should not be provisioned", email)
		}
	}
	eq(t, e.linkedTo(cat)+e.linkedTo(una)+e.linkedTo(dis)+e.linkedTo(robot), "", "no links for people without an account")
	if d := e.accountByID(dora.id()); d.Source != "keycloak" || d.Name != "Dora Diaz" || e.linkedTo(doraK) != dora.id() {
		t.Fatalf("dora (matched by email) = %+v", d)
	}
	if i := e.accountByID(ivyOld.id()); i.Email != "ivy@example.com" || i.Source != "keycloak" || i.Name != "Ivy Ives" {
		t.Fatalf("ivy (matched by identity) = %+v", i)
	}
	if s := e.accountByEmail("sam@example.com"); s.Source != "scim" || s.Name != "Sam (SCIM)" || e.linkedTo(sam) != "" {
		t.Fatalf("SCIM user was touched: %+v (linked %q)", s, e.linkedTo(sam))
	}
	eq(t, e.accountByEmail("boss@example.com").OrgRole, "admin", "bootstrap admin")
	eq(t, e.accountByEmail("boss@example.com").Name, "boss", "name falls back to the username")

	groups := e.keycloakGroups()
	if fmt.Sprint(groups) != fmt.Sprint(map[string][]string{
		"other/team-hidden": {"dora@example.com"}, "team-platform": {"ann@example.com"}, "team-platform/team-api": {"ben@example.com"},
	}) {
		t.Fatalf("groups = %v", groups)
	}

	// Mapped organisation roles apply to synced groups; their membership stays read-only here.
	var list []obj
	admin.get("/api/v1/groups?q=team-platform", &list)
	var platformGroup obj
	for _, g := range list {
		if str(g, "displayName") == "team-platform" {
			platformGroup = g
		}
	}
	eq(t, str(platformGroup, "source"), "keycloak", "group source")
	eq(t, str(platformGroup, "description"), "Platform engineering", "group description")
	admin.must("PATCH", "/api/v1/admin/groups/"+str(platformGroup, "id"), obj{"orgRole": "admin"}, 200, nil)
	admin.must("PUT", "/api/v1/admin/groups/"+str(platformGroup, "id")+"/members/"+dora.id(), nil, 400, nil)
	annC := e.login("ann@example.com", "")
	annC.must("GET", "/api/v1/admin/users", nil, 200, nil) // admin through team-platform
	benC := e.login("ben@example.com", "")

	// Nothing changed upstream: nothing changes here.
	before := e.rowCounts()
	wantStats(t, syncNow(admin), map[string]int{"usersSkipped": 2, "users": 5, "groups": 3, "memberships": 3})
	if after := e.rowCounts(); !maps.Equal(before, after) {
		t.Fatalf("re-run changed rows: %v -> %v", before, after)
	}

	// Changes in Keycloak.
	kc.UpdateUser(ann, func(u *kcUser) { u.Enabled = false })         // disabled: deactivated, signed out
	kc.RemoveMember(contractors, ben)                                 // left the access group: deactivated
	kc.DeleteUser(ivy)                                                // deleted: marked deleted
	kc.UpdateUser(sam, func(u *kcUser) { u.Enabled = false })         // SCIM's, not ours
	kc.UpdateUser(doraK, func(u *kcUser) { u.FirstName = "Dorothy" }) // profile
	kc.RemoveMember(hidden, doraK)
	kc.AddMembers(platform, doraK)
	kc.DeleteGroup(api)
	wantStats(t, syncNow(admin), map[string]int{
		"usersDeactivated": 2, "usersDeleted": 1, "usersUpdated": 1, "usersSkipped": 2, "users": 3,
		"groupsDeleted": 1, "groups": 2, "membershipsAdded": 1, "membershipsRemoved": 1, "memberships": 2,
	})
	annC.must("GET", "/api/v1/me", nil, 401, nil)
	benC.must("GET", "/api/v1/me", nil, 401, nil)
	if a := e.accountByEmail("ann@example.com"); a.Active || a.Deleted {
		t.Fatalf("ann = %+v, want deactivated", a)
	}
	if b := e.accountByEmail("ben@example.com"); b.Active || b.Deleted {
		t.Fatalf("ben = %+v, want deactivated", b)
	}
	if i := e.accountByID(ivyOld.id()); i.Active || !i.Deleted {
		t.Fatalf("ivy = %+v, want deleted", i)
	}
	if s := e.accountByEmail("sam@example.com"); !s.Active || s.Source != "scim" {
		t.Fatalf("SCIM user was touched: %+v", s)
	}
	eq(t, e.accountByID(dora.id()).Name, "Dorothy Diaz", "profile follows Keycloak")
	groups = e.keycloakGroups()
	if fmt.Sprint(groups) != fmt.Sprint(map[string][]string{
		"other/team-hidden": {}, "team-platform": {"ann@example.com", "dora@example.com"},
	}) {
		t.Fatalf("groups = %v", groups)
	}

	// Re-enabled, and back in the access group: active again.
	kc.UpdateUser(ann, func(u *kcUser) { u.Enabled = true })
	kc.AddMembers(contractors, ben)
	wantStats(t, syncNow(admin), map[string]int{"usersReactivated": 2, "usersSkipped": 2, "users": 4, "groups": 2, "memberships": 2})
	e.login("ann@example.com", "").must("GET", "/api/v1/me", nil, 200, nil)
	e.login("ben@example.com", "").must("GET", "/api/v1/me", nil, 200, nil)

	// JIT accounts are off while Keycloak provisions people; the status names the directory.
	eq(t, e.App.Prov.JITEnabled(context.Background()), false, "JIT disabled by Keycloak sync")
	var prov struct {
		DirectorySync obj `json:"directorySync"`
	}
	admin.get("/api/v1/admin/provisioning", &prov)
	d := prov.DirectorySync
	got := fmt.Sprint(d["configured"], d["source"], d["url"], d["realm"], d["provider"], d["group"], d["groupPrefix"], d["syncGroups"], d["intervalMinutes"])
	eq(t, got, fmt.Sprint(true, "keycloak", kc.URL, "wfm", "keycloak", "/workflow-users", "Team-", true, 15), "provisioning status")
	eq(t, str(d["runs"].([]any)[0].(obj), "source"), "keycloak", "run source")
	var audit []obj
	admin.get("/api/v1/admin/audit?action=directory.sync", &audit)
	if len(audit) == 0 || str(audit[0], "data", "source") != "keycloak" {
		t.Fatalf("audit = %v", audit)
	}
}

// Offset paging can skip people while others are deleted mid-run: nobody is deactivated or
// deleted on the strength of a listing alone.
func TestKeycloakSyncConfirmsRemovals(t *testing.T) {
	kc := keycloaktest.New()
	t.Cleanup(kc.Close)
	e := keycloakEnv(t, kc, withEnv("WFM_KEYCLOAK_SYNC_GROUP", "/staff"), withEnv("WFM_KEYCLOAK_SYNC_GROUPS", "false"))
	admin := e.login("admin@example.com", "Admin")
	staff := kc.AddGroup("", "staff")
	ann := kc.AddUser(kcUser{Username: "ann", Email: "ann@example.com"})
	ben := kc.AddUser(kcUser{Username: "ben", Email: "ben@example.com"})
	kc.AddMembers(staff, ann, ben)
	wantStats(t, syncNow(admin), map[string]int{"usersCreated": 2, "users": 2})

	// Missing from the listings, but Keycloak still has them in the access group: left as is.
	kc.Unlisted = map[string]bool{ann: true, ben: true}
	wantStats(t, syncNow(admin), map[string]int{"users": 2})
	if !e.accountByEmail("ann@example.com").Active || !e.accountByEmail("ben@example.com").Active {
		t.Fatal("people skipped by a listing were deactivated")
	}
	// Really out of the access group, or really deleted: acted on.
	kc.RemoveMember(staff, ann)
	kc.DeleteUser(ben)
	wantStats(t, syncNow(admin), map[string]int{"usersDeactivated": 1, "usersDeleted": 1, "users": 0})
	if a, b := e.accountByEmail("ann@example.com"), e.accountByEmail("ben@example.com"); a.Active || a.Deleted || !b.Deleted {
		t.Fatalf("ann = %+v, ben = %+v", a, b)
	}
}

// Failures are recorded on the run with a message an admin can act on, and never deactivate anyone.
func TestKeycloakSyncFailures(t *testing.T) {
	kc := keycloaktest.New()
	t.Cleanup(kc.Close)
	e := keycloakEnv(t, kc, withEnv("WFM_KEYCLOAK_SYNC_GROUPS", "false"))
	admin := e.login("admin@example.com", "Admin")
	kc.AddUser(kcUser{Username: "ann", Email: "ann@example.com"})
	wantStats(t, syncNow(admin), map[string]int{"usersCreated": 1, "users": 1}) // no access group: everyone
	before := e.rowCounts()

	failed := func(want ...string) {
		t.Helper()
		run := syncNow(admin)
		msg, _ := run["error"].(string)
		if str(run, "status") != "failed" {
			t.Fatalf("run = %v, want failed", run)
		}
		t.Logf("failed run: %s", msg)
		for _, w := range want {
			if !strings.Contains(msg, w) {
				t.Fatalf("error %q does not mention %q", msg, w)
			}
		}
		if strings.Contains(msg, kc.ClientSecret) {
			t.Fatalf("error leaks the client secret: %q", msg)
		}
		if a := e.accountByEmail("ann@example.com"); !a.Active {
			t.Fatal("a failed run deactivated people")
		}
	}

	// The service account lacks the realm-management roles.
	kc.Forbidden = true
	failed("HTTP 403", "view-users", "query-groups", kc.ClientID)
	kc.Forbidden = false

	// The access group does not exist (a typo must not deactivate everyone).
	sync := e.keycloakSyncer()
	sync.Cfg.SyncGroup = "/workflow-uesrs"
	failed("access group /workflow-uesrs", "does not exist")
	sync.Cfg.SyncGroup = ""

	// Wrong client secret (tokens are revoked first, so a new one is requested).
	kc.RevokeTokens()
	sync.Client.ClientSecret = "wrong"
	failed("HTTP 401", "WFM_KEYCLOAK_CLIENT_SECRET")
	sync.Client.ClientSecret = kc.ClientSecret

	// Keycloak unreachable.
	kc.Close()
	failed("keycloak")

	if after := e.rowCounts(); after["users"] != before["users"] || after["user_identities"] != before["user_identities"] {
		t.Fatalf("failed runs changed accounts: %v -> %v", before, after)
	}
}
