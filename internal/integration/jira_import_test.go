package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/geoffo-dev/workflow-management/internal/jiraimport/jiratest"
)

// importJira walks the wizard's API: connect, choose projects, scan, dry-run and import.
type jiraWizard struct {
	t     *testing.T
	e     *Env
	admin *Client
	id    string
}

func (w *jiraWizard) path(suffix string) string { return "/api/v1/admin/jira-imports/" + w.id + suffix }

func (w *jiraWizard) get() obj {
	var imp obj
	w.admin.get(w.path(""), &imp)
	return imp
}

func (w *jiraWizard) run() obj {
	w.t.Helper()
	w.admin.must("POST", w.path("/run"), nil, 202, nil)
	w.e.DrainJobs()
	return w.get()
}

func counters(imp obj) map[string]float64 {
	out := map[string]float64{}
	if p, ok := imp["progress"].(obj); ok {
		for k, v := range p["counters"].(obj) {
			out[k] = v.(float64)
		}
	}
	return out
}

func problems(list any) []string {
	var out []string
	for _, p := range list.([]any) {
		m := p.(obj)
		out = append(out, m["severity"].(string)+": "+m["message"].(string))
	}
	return out
}

func hasProblem(list any, severity, contains string) bool {
	return slices.ContainsFunc(problems(list), func(p string) bool {
		return strings.HasPrefix(p, severity+":") && strings.Contains(p, contains)
	})
}

func (e *Env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestJiraImportCloud(t *testing.T) {
	e := newEnv(t, withEnv("WFM_ATTACHMENT_MAX_MB", "1"))
	admin := e.login("admin@example.com", "Admin")
	ada := e.login("ada@example.com", "Ada Here") // matched by email
	base := time.Now().AddDate(0, 0, -20)
	site := jiratest.NewCloud(base)
	srv := site.Server()
	defer srv.Close()
	site.FixContentURLs(srv.URL)
	var waits []time.Duration
	e.App.JiraImport.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	// Connect: wrong credentials and the wrong flavor are refused; nothing is stored.
	conn := obj{"flavor": "cloud", "baseUrl": srv.URL + "/browse/PLAT-1", "email": site.Email, "token": "wrong"}
	r := admin.do("POST", "/api/v1/admin/jira-imports/test-connection", conn)
	eq(t, r.Status, 422, "bad token")
	if !strings.Contains(string(r.Body), "credentials") {
		t.Fatalf("bad token message: %s", r.Body)
	}
	conn["token"] = site.Token
	r = admin.do("POST", "/api/v1/admin/jira-imports/test-connection", obj{"flavor": "datacenter", "baseUrl": srv.URL, "token": site.Token})
	eq(t, r.Status, 400, "wrong flavor")
	var test obj
	admin.must("POST", "/api/v1/admin/jira-imports/test-connection", conn, 200, &test)
	eq(t, str(test, "user", "displayName"), "Jira Admin", "connected user")
	ada.must("POST", "/api/v1/admin/jira-imports", conn, 403, nil)

	var imp obj
	admin.must("POST", "/api/v1/admin/jira-imports", conn, 201, &imp)
	w := &jiraWizard{t: t, e: e, admin: admin, id: imp["id"].(string)}
	eq(t, imp["baseUrl"].(string), srv.URL, "base URL normalised")
	eq(t, imp["hasCredentials"].(bool), true, "credentials stored")
	var sealed string
	if err := e.Pool.QueryRow(context.Background(), `SELECT token_enc FROM jira_imports WHERE id = $1`, w.id).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, site.Token) {
		t.Fatal("token stored in clear")
	}

	// Choose projects; templates come from the Jira boards.
	var projects []obj
	admin.get(w.path("/jira-projects"), &projects)
	sameStrings(t, keys(projects), []string{"OPS", "PLAT"}, "jira projects")
	target := func(id, key, name string) obj {
		return obj{"jiraId": id, "jiraKey": key, "jiraName": name, "key": "", "name": "", "template": "", "open": true, "statuses": obj{}, "types": obj{}}
	}
	cfg := obj{"projects": []obj{target("10000", "PLAT", "Platform"), target("10010", "OPS", "Operations")},
		"priorities": obj{}, "storyPointsField": "", "people": obj{}, "componentsAsLabels": true, "attachments": true}
	admin.must("PATCH", w.path(""), obj{"config": cfg}, 200, &imp)
	ps := imp["config"].(obj)["projects"].([]any)
	eq(t, str(ps[0].(obj), "template"), "scrum", "PLAT template")
	eq(t, str(ps[1].(obj), "template"), "kanban", "OPS template")
	eq(t, str(ps[0].(obj), "key"), "PLAT", "default key")

	var dry obj
	admin.must("POST", w.path("/dry-run"), nil, 200, &dry)
	eq(t, dry["ready"].(bool), false, "dry run before the scan")

	// Scan, through a rate limit.
	site.Lock()
	site.Throttle = 1
	site.Unlock()
	admin.must("POST", w.path("/scan"), nil, 202, nil)
	e.DrainJobs()
	if !slices.Contains(waits, 2*time.Second) {
		t.Fatalf("Retry-After not honoured: waits %v", waits)
	}
	imp = w.get()
	eq(t, str(imp, "status"), "scanned", "status after scan")
	scan := imp["scan"].(obj)
	sp := scan["projects"].([]any)[0].(obj)
	eq(t, sp["issues"].(float64), 6.0, "PLAT issues scanned")
	eq(t, sp["maxNumber"].(float64), 7.0, "PLAT max number")
	eq(t, sp["comments"].(float64), 3.0, "comments counted")
	eq(t, sp["sprints"].(float64), 3.0, "sprints counted")
	eq(t, scan["oversizeCount"].(float64), 1.0, "oversize attachments")
	c := imp["config"].(obj)
	eq(t, str(c, "storyPointsField"), jiratest.FieldStoryPoints, "story points field detected")
	eq(t, c["priorities"].(obj)["6"].(string), "highest", "Blocker → highest")
	plat := c["projects"].([]any)[0].(obj)
	eq(t, plat["statuses"].(obj)["10002"].(string), "In Review", "status by name")
	eq(t, plat["statuses"].(obj)["10004"].(string), "To Do", "Backlog by category")
	eq(t, plat["types"].(obj)["10005"].(string), "subtask", "sub-task by level")
	eq(t, plat["types"].(obj)["10006"].(string), "task", "unknown type → task")
	people := imp["people"].([]any)
	if len(people) != 4 {
		t.Fatalf("people: %v", people)
	}
	for _, p := range people {
		pm := p.(obj)
		if pm["id"] == "acc-ada" && str(pm, "match", "id") != ada.id() {
			t.Fatalf("Ada not matched by email: %v", pm)
		}
	}

	admin.must("POST", w.path("/dry-run"), nil, 200, &dry)
	if !dry["ready"].(bool) {
		t.Fatalf("dry run not ready: %v", problems(dry["problems"]))
	}
	eq(t, dry["projects"].([]any)[0].(obj)["issues"].(float64), 6.0, "dry run issues")
	eq(t, dry["projects"].([]any)[0].(obj)["skippedAttachments"].(float64), 1.0, "dry run skipped attachments")
	eq(t, dry["people"].(obj)["matched"].(float64), 1.0, "matched people")
	eq(t, dry["people"].(obj)["placeholders"].(float64), 1.0, "claimable placeholders")
	eq(t, dry["people"].(obj)["unclaimable"].(float64), 2.0, "people without email")
	for _, want := range []string{"warning:1 attachment(s) are larger", "warning:2 person(s) have no email", "info:The Sub-task workflow has no \"In Review\""} {
		sev, msg, _ := strings.Cut(want, ":")
		if !hasProblem(dry["problems"], sev, msg) {
			t.Fatalf("dry run lacks %q: %v", want, problems(dry["problems"]))
		}
	}
	eq(t, e.count(`SELECT count(*) FROM projects`), 0, "dry run writes nothing")

	// Import; Jira fails mid-way (beyond the client's retries), then the run resumes.
	site.FailSearches(2, 50)
	imp = w.run()
	eq(t, str(imp, "status"), "failed", "status after outage")
	if !strings.Contains(str(imp, "error"), "simulated outage") {
		t.Fatalf("error: %v", imp["error"])
	}
	createdBefore := counters(imp)["issuesCreated"]
	if createdBefore == 0 || createdBefore >= 8 {
		t.Fatalf("expected a partial import, created %v", createdBefore)
	}
	site.FailSearches(0, 0)
	imp = w.run()
	eq(t, str(imp, "status"), "succeeded", "status after resume")
	cnt := counters(imp)
	eq(t, cnt["issuesCreated"], 8.0, "issues created across both runs")
	eq(t, cnt["issuesUpdated"]+cnt["issuesUnchanged"], 0.0, "resume did not redo issues")
	eq(t, e.count(`SELECT count(*) FROM issues`), 8, "issues in the database")
	if !hasProblem(imp["progress"].(obj)["problems"], "warning", "Attachments larger than") {
		t.Fatalf("report lacks the skipped attachment: %v", problems(imp["progress"].(obj)["problems"]))
	}

	// Keys and numbers are kept; the next issue continues after Jira's highest number.
	admin.must("GET", "/api/v1/issues/PLAT-4", nil, 404, nil)
	i2 := admin.issue("PLAT-2")
	eq(t, str(i2, "summary"), "Login page", "summary")
	eq(t, str(i2, "status", "name"), "Done", "status")
	eq(t, str(i2, "type", "key"), "story", "type")
	eq(t, str(i2, "parent", "key"), "PLAT-1", "epic parent")
	eq(t, str(i2, "assignee", "id"), ada.id(), "assignee matched by email")
	eq(t, str(i2, "reporter", "displayName"), "Bob Builder", "reporter placeholder")
	eq(t, i2["storyPoints"].(float64), 5.0, "story points")
	eq(t, str(i2, "resolution"), "Done", "resolution")
	eq(t, str(i2, "sprint", "name"), "PLAT Sprint 1", "sprint")
	createdAt, _ := time.Parse(time.RFC3339, str(i2, "createdAt"))
	if !createdAt.Equal(base.UTC().Truncate(time.Hour).AddDate(0, 0, -2)) {
		t.Fatalf("created timestamp not kept: %v", createdAt)
	}
	if !strings.Contains(str(i2, "description"), "Build the **login** page") || !strings.Contains(str(i2, "description"), "![design.png](/api/v1/attachments/") {
		t.Fatalf("description: %q", i2["description"])
	}
	atts := i2["attachments"].([]any)
	eq(t, len(atts), 1, "attachments (one too large)")
	res, err := admin.http.Do(mustReq(t, e.Srv.URL+"/api/v1/attachments/"+atts[0].(obj)["id"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	eq(t, string(body), string(jiratest.DesignPNG), "attachment content")
	links := i2["links"].([]any)
	eq(t, len(links), 1, "PLAT-2 links")
	eq(t, str(links[0].(obj), "type")+" "+str(links[0].(obj), "direction")+" "+str(links[0].(obj), "issue", "key"), "blocks outward PLAT-3", "blocks link")
	releases := i2["releases"].([]any)
	eq(t, len(releases), 1, "fix versions")
	eq(t, str(releases[0].(obj), "name"), "1.0", "fix version name")

	i3 := admin.issue("PLAT-3")
	eq(t, str(i3, "priority"), "highest", "Blocker priority")
	eq(t, str(i3, "sprint", "name"), "PLAT Sprint 2", "carried-over issue in the active sprint")
	i5 := admin.issue("PLAT-5")
	eq(t, str(i5, "parent", "key"), "PLAT-2", "sub-task parent")
	eq(t, str(i5, "status", "name"), "In Progress", "sub-task status falls back by category")
	eq(t, str(i5, "sprintId"), str(i2, "sprintId"), "sub-task follows its parent's sprint")
	i6 := admin.issue("PLAT-6")
	eq(t, str(i6, "type", "key"), "task", "Spike → task")
	eq(t, str(i6, "status", "name"), "To Do", "Backlog → To Do")
	sameStrings(t, toStrings(i6["labels"]), []string{"tech-debt", "infra", "component:API"}, "labels and components")
	eq(t, str(i6, "reporter", "displayName"), "Cy Hidden", "creator when there is no reporter")
	eq(t, str(admin.issue("OPS-2")["links"].([]any)[0].(obj), "type"), "duplicates", "duplicate link")
	if l := i6["links"].([]any); len(l) != 1 || str(l[0].(obj), "issue", "key") != "OPS-1" {
		t.Fatalf("cross-project relates link: %v", l)
	}

	// Comments keep authors and times; the full list is fetched past the first page.
	var comments []obj
	admin.get("/api/v1/issues/PLAT-2/comments", &comments)
	eq(t, len(comments), 3, "comments")
	eq(t, str(comments[0], "body"), "@Ada Lovelace can you take this?", "comment body")
	eq(t, str(comments[0], "author", "displayName"), "Bob Builder", "comment author")
	eq(t, str(comments[2], "author", "displayName"), "Cy Hidden", "comment author without email")

	// Sprints: the template's empty sprint is gone; Jira's three are there with their states.
	var sprints []obj
	admin.get("/api/v1/projects/PLAT/sprints", &sprints)
	var states []string
	for _, s := range sprints {
		states = append(states, str(s, "name")+"="+str(s, "state"))
	}
	sameStrings(t, states, []string{"PLAT Sprint 2=active", "PLAT Sprint 3=future", "PLAT Sprint 1=closed"}, "sprints")

	// The closed sprint's report replays the imported history: PLAT-2 and PLAT-3 were committed
	// with 3 points each; PLAT-2 was done (at 5 points); PLAT-3 carried over.
	var rep obj
	admin.get("/api/v1/sprints/"+str(sprints[2], "id")+"/report", &rep)
	eq(t, rep["committedIssues"].(float64), 2.0, "committed issues")
	eq(t, rep["committedPoints"].(float64), 6.0, "committed points")
	eq(t, rep["completedIssues"].(float64), 1.0, "completed issues")
	eq(t, rep["completedPoints"].(float64), 5.0, "completed points")
	burndown := rep["burndown"].([]any)
	if len(burndown) < 13 || burndown[0].(obj)["scopePoints"].(float64) != 6 || burndown[len(burndown)-1].(obj)["remainingPoints"].(float64) != 3 {
		t.Fatalf("burndown: first %v last %v (%d days)", burndown[0], burndown[len(burndown)-1], len(burndown))
	}

	// Cycle time from history: In Progress (day 1, 10:00) to resolved (day 7, 12:00).
	var m obj
	admin.get("/api/v1/issues/PLAT-2/metrics", &m)
	if v, _ := m["cycleTimeHours"].(float64); v != 146 {
		r := admin.do("GET", "/api/v1/issues/PLAT-2/history", nil)
		t.Fatalf("cycle time: %v\nhistory: %s", m, r.Body)
	}
	var ct obj
	admin.get("/api/v1/projects/PLAT/reports/cycle-time?days=30", &ct)
	found := false
	for _, it := range ct["issues"].([]any) {
		if str(it.(obj), "key") == "PLAT-2" {
			found = true
			eq(t, it.(obj)["cycleHours"].(float64), 146.0, "cycle time report")
		}
	}
	if !found {
		t.Fatalf("cycle time report lacks PLAT-2: %v", ct)
	}
	var hist []obj
	admin.get("/api/v1/issues/PLAT-2/history", &hist)
	var fields []string
	for _, h := range hist {
		fields = append(fields, str(h, "field"))
	}
	sameStrings(t, fields, []string{"created", "storyPoints", "sprint", "status", "assignee", "storyPoints", "status", "status", "resolution"}, "history")
	eq(t, str(hist[3], "actor", "displayName"), "Ada Here", "history actor")

	// Placeholders: Bob can claim his by signing in with his email; Cy (no email) cannot.
	var bobActive, cyActive bool
	var cyEmail string
	if err := e.Pool.QueryRow(context.Background(), `SELECT active FROM users WHERE email = 'bob@example.com'`).Scan(&bobActive); err != nil {
		t.Fatal(err)
	}
	if err := e.Pool.QueryRow(context.Background(), `SELECT active, email FROM users WHERE display_name = 'Cy Hidden'`).Scan(&cyActive, &cyEmail); err != nil {
		t.Fatal(err)
	}
	eq(t, bobActive || cyActive, false, "placeholders are inactive")
	if !strings.HasSuffix(cyEmail, ".invalid") {
		t.Fatalf("unclaimable placeholder email: %s", cyEmail)
	}
	bob := e.login("bob@example.com", "Bob B.")
	eq(t, str(admin.issue("PLAT-2"), "reporter", "id"), bob.id(), "Bob claimed his placeholder")
	eq(t, e.count(`SELECT count(*) FROM user_placeholders p JOIN users u ON u.id = p.user_id WHERE u.email = 'bob@example.com'`), 0, "placeholder marker dropped")

	// The next issue number continues after Jira's highest.
	created := admin.createIssue("PLAT", "task", "Made here", nil)
	eq(t, str(created, "key"), "PLAT-8", "next key")

	// Re-run: nothing changes and nothing is duplicated.
	before := map[string]int{}
	tables := []string{"issues", "comments", "issue_history", "attachments", "issue_links", "sprints", "releases", "users", "projects"}
	for _, tb := range tables {
		before[tb] = e.count("SELECT count(*) FROM " + tb)
	}
	imp = w.run()
	eq(t, str(imp, "status"), "succeeded", "re-run status")
	eq(t, counters(imp)["issuesUnchanged"], 8.0, "unchanged issues skipped")
	for _, tb := range tables {
		eq(t, e.count("SELECT count(*) FROM "+tb), before[tb], "re-run rows in "+tb)
	}

	// Jira moves on: PLAT-7 is edited, PLAT-8 (taken here meanwhile) and PLAT-9 are new.
	site.Lock()
	for _, is := range site.Issues {
		if is.Key == "PLAT-7" {
			is.Fields["summary"] = "Remember me (v2)"
			is.Fields["updated"] = jiratest.Time(time.Now())
		}
	}
	for _, n := range []int{8, 9} {
		site.Issues = append(site.Issues, &jiratest.Issue{ID: fmt.Sprintf("3009%d", n), Key: fmt.Sprintf("PLAT-%d", n), Fields: map[string]any{
			"summary": fmt.Sprintf("New in Jira %d", n), "issuetype": site.Projects[0].IssueTypes[2], "status": obj{"id": "10000", "name": "To Do", "statusCategory": obj{"key": "new"}},
			"created": jiratest.Time(time.Now()), "updated": jiratest.Time(time.Now()), "labels": []string{},
		}})
	}
	site.Unlock()
	imp = w.run()
	cnt = counters(imp)
	eq(t, cnt["issuesUpdated"], 1.0, "changed issue updated")
	eq(t, cnt["issuesCreated"], 1.0, "new issue created")
	eq(t, cnt["issuesSkipped"], 1.0, "issue whose number is taken here")
	if !hasProblem(imp["progress"].(obj)["problems"], "error", "PLAT-8 already exists here") {
		t.Fatalf("number clash not reported: %v", problems(imp["progress"].(obj)["problems"]))
	}
	eq(t, str(admin.issue("PLAT-7"), "summary"), "Remember me (v2)", "updated summary")
	eq(t, str(admin.issue("PLAT-8"), "summary"), "Made here", "native issue untouched")
	eq(t, str(admin.issue("PLAT-9"), "summary"), "New in Jira 9", "new issue")
	eq(t, e.count(`SELECT count(*) FROM issue_history h JOIN issues i ON i.id = h.issue_id WHERE i.number = 7 AND h.actor_type = 'import'`), 3, "PLAT-7 history rewritten, not duplicated")

	// Credentials can be deleted after the import; the record stays.
	admin.must("DELETE", w.path("/credentials"), nil, 200, &imp)
	eq(t, imp["hasCredentials"].(bool), false, "credentials deleted")
	r = admin.do("POST", w.path("/run"), nil)
	eq(t, r.Status, 409, "no run without credentials")
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func mustReq(t *testing.T, url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// TestJiraImportDataCenter imports a Data Center site with a tiny time budget, so every step
// runs in its own continuation job.
func TestJiraImportDataCenter(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	site := jiratest.NewDataCenter(time.Now().AddDate(0, 0, -30))
	srv := site.Server()
	defer srv.Close()
	site.FixContentURLs(srv.URL)
	e.App.JiraImport.Budget = time.Nanosecond

	var imp obj
	admin.must("POST", "/api/v1/admin/jira-imports", obj{"flavor": "datacenter", "baseUrl": srv.URL + "/", "token": site.Token}, 201, &imp)
	w := &jiraWizard{t: t, e: e, admin: admin, id: imp["id"].(string)}
	cfg := obj{"projects": []obj{{"jiraId": "11000", "jiraKey": "LEG", "jiraName": "Legacy", "key": "OLD", "name": "Old stuff", "template": "", "open": false, "statuses": obj{}, "types": obj{}}},
		"priorities": obj{}, "storyPointsField": "", "people": obj{}, "componentsAsLabels": false, "attachments": true}
	admin.must("PATCH", w.path(""), obj{"config": cfg}, 200, nil)
	admin.must("POST", w.path("/scan"), nil, 202, nil)
	e.DrainJobs()
	imp = w.get()
	eq(t, str(imp, "status"), "scanned", "scan status")
	if e.JobsOfKind("jira_import") < 3 {
		t.Fatalf("expected continuation jobs, got %d", e.JobsOfKind("jira_import"))
	}
	var dry obj
	admin.must("POST", w.path("/dry-run"), nil, 200, &dry)
	if !hasProblem(dry["problems"], "info", "LEG-12 becomes OLD-12") {
		t.Fatalf("key change not explained: %v", problems(dry["problems"]))
	}
	imp = w.run()
	eq(t, str(imp, "status"), "succeeded", "import status")

	// The project got another key; the Jira key still resolves (alias).
	i2 := admin.issue("OLD-2")
	eq(t, str(admin.issue("LEG-2"), "key"), "OLD-2", "old key resolves")
	eq(t, str(i2, "parent", "key"), "OLD-1", "Epic Link → parent")
	eq(t, str(i2, "priority"), "highest", "Critical → highest")
	eq(t, str(i2, "resolution"), "Fixed", "resolution name")
	eq(t, str(i2, "assignee", "displayName"), "Eve Online", "assignee by user key")
	eq(t, i2["storyPoints"].(float64), 8.0, "story points")
	desc := str(i2, "description")
	for _, want := range []string{"## Plan", "- **Back up** first", "- run `migrate`", "```sql\nALTER TABLE t;\n```", "[the runbook](https://wiki.example.com/runbook)", "![server.log](/api/v1/attachments/"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description lacks %q:\n%s", want, desc)
		}
	}
	var comments []obj
	admin.get("/api/v1/issues/OLD-2/comments", &comments)
	eq(t, str(comments[0], "body"), "Done, see @dora *for details*.", "wiki comment")
	eq(t, str(admin.issue("OLD-3"), "parent", "key"), "OLD-2", "sub-task parent")
	var sprints []obj
	admin.get("/api/v1/projects/OLD/sprints", &sprints)
	if len(sprints) != 1 || str(sprints[0], "name") != "Legacy sprint, part 1" || str(sprints[0], "state") != "closed" || str(sprints[0], "goal") != "Ship it, finally" {
		t.Fatalf("legacy sprint strings: %v", sprints)
	}
	var m obj
	admin.get("/api/v1/issues/OLD-2/metrics", &m)
	eq(t, m["cycleTimeHours"].(float64), 87.0, "cycle time (day 1 09:00 → day 5 00:00)")
	var rels []obj
	admin.get("/api/v1/projects/OLD/releases", &rels)
	if len(rels) != 1 || str(rels[0], "state") != "archived" {
		t.Fatalf("archived version: %v", rels)
	}
	// Eve is deactivated in Jira but has an email: she gets a claimable placeholder.
	eq(t, e.count(`SELECT count(*) FROM users u JOIN user_placeholders p ON p.user_id = u.id WHERE u.email = 'eve@example.com' AND NOT u.active`), 1, "Eve's placeholder")
	eveID := str(i2, "assignee", "id")

	// SCIM provisioning Eve adopts the placeholder instead of failing on the email.
	var conn obj
	admin.must("POST", "/api/v1/admin/scim-connections", obj{"name": "Okta"}, 201, &conn)
	created := e.scim(str(conn, "token")).call("POST", "/Users", obj{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:User"}, "userName": "eve@example.com", "externalId": "00u-eve",
		"emails": []obj{{"value": "eve@example.com", "primary": true}}, "displayName": "Eve O.", "active": true,
	}, 201)
	eq(t, str(created, "id"), eveID, "SCIM adopted the placeholder")
	eq(t, e.count(`SELECT count(*) FROM user_placeholders`), 1, "only Dora's placeholder is left")
	eq(t, e.count(`SELECT count(*) FROM users WHERE email = 'eve@example.com' AND active AND source = 'scim'`), 1, "Eve active via SCIM")
}

// A placeholder (an imported person without an account) is claimed by signing in with its email
// through SSO when just-in-time provisioning is allowed; otherwise sign-in is refused. A
// deactivated account that is not a placeholder stays deactivated.
func TestPlaceholderClaimOnSignIn(t *testing.T) {
	placeholder := func(e *Env, email string) string {
		var id string
		if err := e.Pool.QueryRow(context.Background(), `INSERT INTO users (kind, email, username, display_name, org_role, source, active)
			VALUES ('human', $1, $1, 'From Jira', 'member', 'admin', false) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		e.exec(`INSERT INTO user_placeholders (user_id, source, external_id) VALUES ($1, 'jira', 'acc-1')`, id)
		return id
	}
	idp := newFakeIdP(t)
	e := oidcEnv(t, idp)
	e.login("admin@example.com", "Admin")
	id := placeholder(e, "pat@example.com")
	e.exec(`INSERT INTO users (kind, email, username, display_name, org_role, source, active) VALUES ('human', 'gone@example.com', 'gone@example.com', 'Gone', 'member', 'jit', false)`)

	c := e.anon()
	idp.signIn(t, c, "corp", jwt.MapClaims{"sub": "p-1", "email": "pat@example.com", "email_verified": true, "name": "Pat Doe"})
	var me obj
	c.get("/api/v1/me", &me)
	eq(t, str(me, "id"), id, "the placeholder became Pat's account")
	eq(t, str(me, "displayName"), "Pat Doe", "profile from the IdP")
	eq(t, e.count(`SELECT count(*) FROM user_placeholders`), 0, "marker dropped")
	loc := idp.signIn(t, e.anon(), "corp", jwt.MapClaims{"sub": "g-1", "email": "gone@example.com", "email_verified": true})
	eq(t, loc, "/login?error=deactivated", "deactivated accounts are not claimable")

	// Without just-in-time provisioning a placeholder is not an account one can sign in to.
	e2 := oidcEnv(t, idp, withEnv("WFM_JIT_PROVISIONING", "false"))
	placeholder(e2, "quinn@example.com")
	loc = idp.signIn(t, e2.anon(), "corp", jwt.MapClaims{"sub": "q-1", "email": "quinn@example.com", "email_verified": true})
	eq(t, loc, "/login?error=not_provisioned", "no claim without JIT")
	eq(t, e2.count(`SELECT count(*) FROM user_placeholders`), 1, "still a placeholder")
}
