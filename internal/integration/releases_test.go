package integration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// release creates a release and returns it.
func (c *Client) release(project string, body obj) obj {
	c.e.t.Helper()
	var r obj
	c.must("POST", "/api/v1/projects/"+project+"/releases", body, 201, &r)
	return r
}

func (c *Client) releaseDetail(id string) obj {
	c.e.t.Helper()
	var r obj
	c.get("/api/v1/releases/"+id, &r)
	return r
}

func (c *Client) move(key, status string) {
	c.e.t.Helper()
	c.must("POST", "/api/v1/issues/"+key+"/move", obj{"statusId": c.statusID(status)}, 200, nil)
}

func num(m obj, path ...string) float64 {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return -1
		}
		cur = mm[p]
	}
	f, _ := cur.(float64)
	return f
}

func warning(r obj, kind string) obj {
	for _, w := range r["warnings"].([]any) {
		if m := w.(map[string]any); m["kind"] == kind {
			return m
		}
	}
	return nil
}

func warningKeys(w obj) []string {
	var out []string
	for _, i := range w["issues"].([]any) {
		out = append(out, str(i.(map[string]any), "issue", "key"))
	}
	return out
}

func TestReleasePage(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	admin.createProject("SHOP", "kanban")
	admin.must("POST", "/api/v1/projects", obj{"key": "PRIV", "name": "Private", "template": "kanban", "access": "private"}, 201, nil)

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	rel := admin.release("SHOP", obj{"name": "3.9.0", "description": "The checkout release", "releaseDate": yesterday})
	rid := str(rel, "id")
	other := admin.release("SHOP", obj{"name": "4.0.0"})
	epic := admin.createIssue("SHOP", "epic", "Checkout", obj{"releaseIds": []string{rid}})
	admin.createIssue("SHOP", "story", "Pay by card", obj{"releaseIds": []string{rid}, "storyPoints": 3, "parentKey": str(epic, "key")})
	admin.createIssue("SHOP", "bug", "Crash *on* login", obj{"releaseIds": []string{rid}, "storyPoints": 2})
	admin.createIssue("SHOP", "task", "Tidy the logs", obj{"releaseIds": []string{rid}})
	sub := admin.createIssue("SHOP", "subtask", "Write the migration", obj{"releaseIds": []string{rid}, "parentKey": "SHOP-2"})
	admin.createIssue("SHOP", "story", "Gift cards", obj{"releaseIds": []string{rid}, "storyPoints": 5})
	for _, k := range []string{"SHOP-1", "SHOP-2", "SHOP-3", str(sub, "key")} {
		admin.move(k, "Done")
	}
	admin.move("SHOP-4", "In Progress")

	// Progress by status category, in issues and story points.
	d := admin.releaseDetail(rid)
	eq(t, str(d, "projectKey"), "SHOP", "project key")
	eq(t, d["canManage"], any(true), "admin manages releases")
	eq(t, num(d, "progress", "issues", "total"), 6, "issues")
	eq(t, num(d, "progress", "issues", "done"), 4, "done issues")
	eq(t, num(d, "progress", "issues", "inProgress"), 1, "in progress")
	eq(t, num(d, "progress", "issues", "todo"), 1, "to do")
	eq(t, num(d, "progress", "points", "total"), 10, "points")
	eq(t, num(d, "progress", "points", "done"), 5, "done points")
	eq(t, num(d, "progress", "unestimated"), 3, "unestimated")
	eq(t, mel.releaseDetail(rid)["canManage"], any(false), "members do not manage releases")

	// Releases of projects people cannot browse are not found.
	priv := admin.release("PRIV", obj{"name": "secret"})
	mel.must("GET", "/api/v1/releases/"+str(priv, "id"), nil, 404, nil)
	mel.must("GET", "/api/v1/releases/"+str(priv, "id")+"/readiness", nil, 404, nil)
	mel.must("GET", "/api/v1/releases/"+str(priv, "id")+"/burnup", nil, 404, nil)
	mel.must("GET", "/api/v1/releases/"+str(priv, "id")+"/notes/generated", nil, 404, nil)

	// Readiness: unfinished issues, open pull requests, failed pipelines and the passed date.
	var conn, repo string
	if err := e.Pool.QueryRow(t.Context(), `INSERT INTO scm_connections (provider, name, api_url, web_url, secrets_enc) VALUES ('github', 'GitHub', 'https://api.github.com', 'https://github.com', 'x') RETURNING id`).Scan(&conn); err != nil {
		t.Fatal(err)
	}
	if err := e.Pool.QueryRow(t.Context(), `INSERT INTO scm_repositories (connection_id, external_id, full_path, web_url) VALUES ($1, '1', 'acme/shop', 'https://github.com/acme/shop') RETURNING id`, conn).Scan(&repo); err != nil {
		t.Fatal(err)
	}
	link := func(number int, kind, ext, state string, ago time.Duration) {
		e.exec(`INSERT INTO dev_links (issue_id, repository_id, kind, external_id, title, url, state, updated_at)
			VALUES ((SELECT id FROM issues WHERE number = $1 AND project_id = (SELECT id FROM projects WHERE key = 'SHOP')), $2, $3, $4, $5, $6, $7, now() - $8::interval)`,
			number, repo, kind, ext, kind+" "+ext, "https://github.com/acme/shop/"+ext, state, fmt.Sprintf("%d seconds", int(ago.Seconds())))
	}
	link(2, "pull_request", "7", "open", 0)
	link(2, "pull_request", "8", "merged", 0)
	link(3, "pull_request", "9", "merged", 0)
	link(3, "pipeline", "100", "success", time.Hour)
	link(3, "pipeline", "101", "failed", 0)
	link(2, "pipeline", "102", "failed", time.Hour)
	link(2, "pipeline", "103", "success", 0)
	var ready obj
	mel.get("/api/v1/releases/"+rid+"/readiness", &ready)
	eq(t, ready["ready"], any(false), "not ready")
	sameStrings(t, warningKeys(warning(ready, "not_done")), []string{"SHOP-4", "SHOP-6"}, "not done")
	prs := warning(ready, "open_pull_requests")
	sameStrings(t, warningKeys(prs), []string{"SHOP-2"}, "open pull requests")
	eq(t, len(prs["issues"].([]any)[0].(map[string]any)["links"].([]any)), 1, "only the open PR is listed")
	sameStrings(t, warningKeys(warning(ready, "failed_pipelines")), []string{"SHOP-3"}, "the latest pipeline failed")
	if w := warning(ready, "overdue"); w == nil || !strings.Contains(str(w, "message"), "has passed") {
		t.Fatalf("overdue warning = %v", w)
	}
	var calm obj
	admin.get("/api/v1/releases/"+str(other, "id")+"/readiness", &calm)
	eq(t, calm["ready"], any(true), "an empty release with no date is ready")

	// Release notes: done issues by type, sub-tasks left out, summaries escaped.
	var gen obj
	mel.get("/api/v1/releases/"+rid+"/notes/generated", &gen)
	want := "## Features\n\n- Pay by card (SHOP-2)\n\n## Bug fixes\n\n- Crash \\*on\\* login (SHOP-3)\n\n## Epics\n\n- Checkout (SHOP-1)\n"
	eq(t, str(gen, "notes"), want, "generated notes")
	eq(t, num(gen, "issueCount"), 3, "issues listed")
	admin.move("SHOP-4", "Done")
	mel.get("/api/v1/releases/"+rid+"/notes/generated", &gen)
	if !strings.Contains(str(gen, "notes"), "## Improvements\n\n- Tidy the logs (SHOP-4)\n") {
		t.Fatalf("tasks are improvements:\n%s", str(gen, "notes"))
	}

	// Notes are saved on the release; publishing needs notes and release.manage.
	mel.must("PATCH", "/api/v1/releases/"+rid, obj{"notes": "x"}, 403, nil)
	admin.must("PATCH", "/api/v1/releases/"+rid, obj{"notesPublished": true}, 400, nil)
	var saved obj
	admin.must("PATCH", "/api/v1/releases/"+rid, obj{"notes": str(gen, "notes"), "notesPublished": true}, 200, &saved)
	eq(t, saved["notesPublished"], any(true), "published")
	eq(t, str(admin.releaseDetail(rid), "notes"), strings.TrimRight(str(gen, "notes"), "\n"), "notes stored")
	admin.must("PATCH", "/api/v1/releases/"+rid, obj{"name": "3.9.0"}, 200, &saved)
	eq(t, saved["notesPublished"], any(true), "other edits keep the notes published")

	// Release settings: what tags do (project admins change them).
	var st obj
	mel.get("/api/v1/projects/SHOP/release-settings", &st)
	eq(t, st["createFromTags"], any(false), "off by default")
	eq(t, st["releaseOnTag"], any(false), "off by default")
	mel.must("PATCH", "/api/v1/projects/SHOP/release-settings", obj{"createFromTags": true}, 403, nil)
	admin.must("PATCH", "/api/v1/projects/SHOP/release-settings", obj{"releaseOnTag": true}, 200, &st)
	eq(t, st["releaseOnTag"], any(true), "release on tag")
	eq(t, st["createFromTags"], any(false), "unchanged")

	// Shipping moves the unfinished issue and records it in its history.
	admin.must("POST", "/api/v1/releases/"+rid+"/release", obj{"moveOpenIssuesTo": str(other, "id")}, 200, nil)
	var hist []obj
	admin.get("/api/v1/issues/SHOP-6/history", &hist)
	found := false
	for _, h := range hist {
		if str(h, "field") == "fixVersions" && str(h, "from") == "3.9.0" && str(h, "to") == "4.0.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fix version history for the moved issue: %v", hist)
	}
	eq(t, str(admin.releaseDetail(rid), "state"), "released", "shipped")
}

func TestReleaseBurnup(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("BURN", "kanban")
	day0 := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -5)
	rel := admin.release("BURN", obj{"name": "1.0", "startDate": day0.Format("2006-01-02")})
	rid := str(rel, "id")

	// Nothing to replay: the release's current issues, each from its creation.
	var b obj
	admin.get("/api/v1/releases/"+rid+"/burnup", &b)
	eq(t, str(b, "scopeSource"), "current", "no history yet")
	eq(t, len(b["days"].([]any)), 6, "a point per day from the start date to today")

	admin.createIssue("BURN", "story", "A", obj{"releaseIds": []string{rid}, "storyPoints": 3})
	admin.createIssue("BURN", "story", "B", obj{"storyPoints": 5})
	admin.createIssue("BURN", "story", "C", obj{"storyPoints": 8})
	admin.must("PATCH", "/api/v1/issues/BURN-3", obj{"releaseIds": []string{rid}}, 200, nil)
	admin.must("PATCH", "/api/v1/issues/BURN-2", obj{"releaseIds": []string{rid}}, 200, nil)
	admin.must("PATCH", "/api/v1/issues/BURN-3", obj{"releaseIds": []string{}}, 200, nil)
	admin.move("BURN-1", "Done")

	// Spread the history over the days of the release.
	at := func(days, hours int) time.Time { return day0.AddDate(0, 0, days).Add(time.Duration(hours) * time.Hour) }
	e.exec(`UPDATE issues SET created_at = $1 WHERE project_id = (SELECT id FROM projects WHERE key = 'BURN')`, at(0, 1))
	history := func(number int, field string, nth int, when time.Time) {
		e.exec(`UPDATE issue_history SET created_at = $4 WHERE id = (
			SELECT h.id FROM issue_history h JOIN issues i ON i.id = h.issue_id
			WHERE i.number = $1 AND i.project_id = (SELECT id FROM projects WHERE key = 'BURN') AND h.field = $2
			ORDER BY h.id OFFSET $3 LIMIT 1)`, number, field, nth, when)
	}
	history(3, "fixVersions", 0, at(1, 12)) // C joins on day 1
	history(2, "fixVersions", 0, at(2, 12)) // B joins on day 2
	history(3, "fixVersions", 1, at(3, 12)) // C leaves on day 3
	history(1, "status", 0, at(4, 12))      // A is done on day 4

	admin.get("/api/v1/releases/"+rid+"/burnup", &b)
	eq(t, str(b, "scopeSource"), "history", "fix version history replayed")
	eq(t, str(b, "from"), day0.Format("2006-01-02"), "from the start date")
	var got []string
	for _, d := range b["days"].([]any) {
		m := d.(map[string]any)
		got = append(got, fmt.Sprintf("%v/%v %v/%v", m["doneIssues"], m["scopeIssues"], m["donePoints"], m["scopePoints"]))
	}
	sameStrings(t, got, []string{"0/1 0/3", "0/2 0/11", "0/3 0/16", "0/2 0/8", "1/2 3/8", "1/2 3/8"}, "burnup")

	// Renaming the release keeps its history (changes record release ids).
	admin.must("PATCH", "/api/v1/releases/"+rid, obj{"name": "1.0-final"}, 200, nil)
	var renamed obj
	admin.get("/api/v1/releases/"+rid+"/burnup", &renamed)
	raw1, _ := json.Marshal(b["days"])
	raw2, _ := json.Marshal(renamed["days"])
	eq(t, string(raw2), string(raw1), "burnup after a rename")
}

func TestReleaseNotesInTheHelpCentre(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	serviceDesk(t, e, "OTHER")
	pat := e.login("pat@example.com", "Pat")
	mel := e.login("mel@example.com", "Mel")
	admin.must("POST", "/api/v1/projects/HELP/members", obj{"userId": pat.id(), "roleKey": "admin"}, 201, nil)
	admin.createProject("APP", "kanban")
	admin.must("POST", "/api/v1/projects", obj{"key": "SECRET", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	admin.createIssue("APP", "story", "Internal codename Falcon", nil)

	published := admin.release("APP", obj{"name": "3.9.0", "releaseDate": "2026-10-01", "notes": "## Features\n\n- Pay by card (APP-1)", "notesPublished": true})
	admin.release("APP", obj{"name": "4.0.0", "notes": "## Features\n\n- Not yet announced", "notesPublished": false})
	admin.release("SECRET", obj{"name": "1.0", "notes": "Secret notes", "notesPublished": true})
	admin.must("PATCH", "/api/v1/issues/APP-1", obj{"releaseIds": []string{str(published, "id")}}, 200, nil)

	cust := admin.addCustomer("HELP", "cara@example.com", "Cara")
	outsider := admin.addCustomer("OTHER", "olga@example.com", "Olga")
	var notes []obj
	cust.get("/api/v1/portal/projects/HELP/whats-new", &notes)
	eq(t, len(notes), 0, "nothing until the desk chooses projects")
	var desk obj
	cust.get("/api/v1/portal/projects/HELP", &desk)
	eq(t, desk["whatsNew"], any(false), "no What's new yet")

	// Choosing projects: desk admins only, software projects they can browse.
	agent.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"releaseNotesProjectKeys": []string{"APP"}}, 403, nil)
	pat.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"releaseNotesProjectKeys": []string{"SECRET"}}, 400, nil)
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"releaseNotesProjectKeys": []string{"OTHER"}}, 400, nil)
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"releaseNotesProjectKeys": []string{"NOPE"}}, 400, nil)
	var sd obj
	pat.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"releaseNotesProjectKeys": []string{"APP"}}, 200, &sd)
	projects := sd["releaseNotesProjects"].([]any)
	eq(t, len(projects), 1, "one project")
	eq(t, str(projects[0].(map[string]any), "key"), "APP", "APP chosen")

	// Customers see the published notes of the chosen projects, and only the notes.
	cust.get("/api/v1/portal/projects/HELP/whats-new", &notes)
	eq(t, len(notes), 1, "only published notes of chosen projects")
	n := notes[0]
	eq(t, str(n, "name"), "3.9.0", "release name")
	eq(t, str(n, "product"), "APP project", "product name")
	eq(t, str(n, "date"), "2026-10-01", "release date")
	eq(t, str(n, "notes"), "## Features\n\n- Pay by card (APP-1)", "notes text")
	var fields []string
	for k := range n {
		fields = append(fields, k)
	}
	slices.Sort(fields)
	sameStrings(t, fields, []string{"date", "id", "name", "notes", "product"}, "nothing but the notes")
	raw := cust.do("GET", "/api/v1/portal/projects/HELP/whats-new", nil)
	for _, leak := range []string{"Falcon", "Not yet announced", "Secret notes", "SECRET"} {
		if strings.Contains(string(raw.Body), leak) {
			t.Fatalf("What's new leaks %q: %s", leak, raw.Body)
		}
	}
	cust.get("/api/v1/portal/projects/HELP", &desk)
	eq(t, desk["whatsNew"], any(true), "the desk has news")

	// Nobody else reads them: other desks' customers, people who may not raise requests there.
	outsider.must("GET", "/api/v1/portal/projects/HELP/whats-new", nil, 404, nil)
	mel.must("GET", "/api/v1/portal/projects/HELP/whats-new", nil, 404, nil)
	e.anon().must("GET", "/api/v1/portal/projects/HELP/whats-new", nil, 401, nil)
	var empty []obj
	outsider.get("/api/v1/portal/projects/OTHER/whats-new", &empty)
	eq(t, len(empty), 0, "the other desk chose nothing")
	// Customers never reach the release API itself.
	cust.must("GET", "/api/v1/releases/"+str(published, "id"), nil, 403, nil)
	cust.must("GET", "/api/v1/releases/"+str(published, "id")+"/notes/generated", nil, 403, nil)
	cust.must("GET", "/api/v1/projects/APP/releases", nil, 403, nil)

	// Unpublishing takes them off at once.
	admin.must("PATCH", "/api/v1/releases/"+str(published, "id"), obj{"notesPublished": false}, 200, nil)
	cust.get("/api/v1/portal/projects/HELP/whats-new", &notes)
	eq(t, len(notes), 0, "unpublished")

	// Removing the project from the desk takes its notes off too.
	admin.must("PATCH", "/api/v1/releases/"+str(published, "id"), obj{"notesPublished": true}, 200, nil)
	cust.get("/api/v1/portal/projects/HELP/whats-new", &notes)
	eq(t, len(notes), 1, "published again")
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"releaseNotesProjectKeys": []string{}}, 200, nil)
	cust.get("/api/v1/portal/projects/HELP/whats-new", &notes)
	eq(t, len(notes), 0, "project removed from the desk")
}

// githubRepo connects a fake GitHub App with one repository (acme/shop) linked to the project.
func githubRepo(t *testing.T, e *Env, admin *Client, project string, routes map[string]any) (*fakeSCM, string, string) {
	t.Helper()
	routes["POST /app/installations/77/access_tokens"] = obj{"token": "ghs_installation", "expires_at": time.Now().Add(time.Hour)}
	routes["GET /app/installations"] = []obj{{"id": 77}}
	routes["GET /installation/repositories"] = obj{"repositories": []obj{shopRepo}}
	gh := newFakeSCM(t, routes)
	var conn obj
	admin.must("POST", "/api/v1/admin/scm-connections", obj{
		"provider": "github", "name": "GitHub", "apiUrl": gh.URL, "appId": 4242, "privateKey": rsaPEM(t), "webhookSecret": "s3cret",
	}, 201, &conn)
	hook := "/hooks/github/" + str(conn, "id")
	install := obj{"action": "created", "installation": obj{"id": 77}, "repositories": []obj{{"id": 101, "full_name": "acme/shop"}}}
	e.deliver(hook, install, githubHeaders("s3cret", "installation", "install", install)...)
	e.DrainJobs()
	var repos []obj
	admin.get("/api/v1/scm/repositories", &repos)
	repoID := str(repos[0], "id")
	admin.must("POST", "/api/v1/projects/"+project+"/repositories", obj{"repositoryId": repoID}, 201, nil)
	return gh, hook, repoID
}

var shopRepo = obj{"id": 101, "full_name": "acme/shop", "html_url": "https://github.com/acme/shop", "default_branch": "main"}

func releaseNamed(c *Client, project, name string) obj {
	c.e.t.Helper()
	var rs []obj
	c.get("/api/v1/projects/"+project+"/releases", &rs)
	for _, r := range rs {
		if str(r, "name") == name {
			return r
		}
	}
	return nil
}

func TestReleaseTags(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	admin.createProject("SHOP", "kanban")
	_, hook, repoID := githubRepo(t, e, admin, "SHOP", map[string]any{})
	prev := admin.release("SHOP", obj{"name": "3.8.0"})
	rel := admin.release("SHOP", obj{"name": "3.9.0"})

	// Linking by hand: a repository connected to the project and a valid tag name.
	mel.must("PATCH", "/api/v1/releases/"+str(prev, "id"), obj{"tagRepositoryId": repoID, "tagName": "v3.8.0"}, 403, nil)
	admin.must("PATCH", "/api/v1/releases/"+str(prev, "id"), obj{"tagRepositoryId": repoID, "tagName": "bad tag"}, 400, nil)
	admin.must("PATCH", "/api/v1/releases/"+str(prev, "id"), obj{"tagName": "v3.8.0"}, 400, nil) // no repository yet
	var linked obj
	admin.must("PATCH", "/api/v1/releases/"+str(prev, "id"), obj{"tagRepositoryId": repoID, "tagName": "v3.8.0"}, 200, &linked)
	eq(t, str(linked, "tagName"), "v3.8.0", "linked by hand")

	// A pushed tag links the release named like it (without its leading v).
	tag := obj{"ref": "v3.9.0", "ref_type": "tag", "master_branch": "main", "installation": obj{"id": 77}, "repository": shopRepo}
	eq(t, e.deliver(hook, tag, githubHeaders("s3cret", "create", "t-1", tag)...).Status, 202, "create tag")
	e.DrainJobs()
	d := admin.releaseDetail(str(rel, "id"))
	eq(t, str(d, "tag", "name"), "v3.9.0", "tag linked")
	eq(t, str(d, "tag", "url"), "https://github.com/acme/shop/tree/v3.9.0", "tag page")
	eq(t, str(d, "tag", "previousTag"), "v3.8.0", "previous release's tag")
	eq(t, str(d, "tag", "compareUrl"), "https://github.com/acme/shop/compare/v3.8.0...v3.9.0", "compare link")
	eq(t, str(d, "state"), "unreleased", "releasing on tags is off by default")
	// The push of the same tag fills in its commit.
	push := obj{"ref": "refs/tags/v3.9.0", "created": true, "after": "tagobject", "head_commit": obj{"id": "c0ffee"}, "installation": obj{"id": 77}, "repository": shopRepo, "commits": []obj{}}
	e.deliver(hook, push, githubHeaders("s3cret", "push", "t-2", push)...)
	e.DrainJobs()
	eq(t, str(admin.releaseDetail(str(rel, "id")), "tag", "sha"), "c0ffee", "tag commit from the push")

	// Tags naming no release are ignored unless the project creates releases from tags.
	unknown := obj{"ref": "v5.0.0", "ref_type": "tag", "installation": obj{"id": 77}, "repository": shopRepo}
	e.deliver(hook, unknown, githubHeaders("s3cret", "create", "t-3", unknown)...)
	e.DrainJobs()
	if releaseNamed(admin, "SHOP", "v5.0.0") != nil {
		t.Fatal("a release was created with the setting off")
	}
	admin.must("PATCH", "/api/v1/projects/SHOP/release-settings", obj{"createFromTags": true, "releaseOnTag": true}, 200, nil)
	next := obj{"ref": "refs/tags/v4.0.0", "created": true, "after": "abc400", "installation": obj{"id": 77}, "repository": shopRepo, "commits": []obj{}}
	e.deliver(hook, next, githubHeaders("s3cret", "push", "t-4", next)...)
	e.DrainJobs()
	created := releaseNamed(admin, "SHOP", "v4.0.0")
	if created == nil {
		t.Fatal("no release created from the tag")
	}
	eq(t, str(created, "tagName"), "v4.0.0", "created release is linked")
	eq(t, str(created, "state"), "released", "released on its tag")
	eq(t, str(admin.releaseDetail(str(created, "id")), "tag", "sha"), "abc400", "sha of a lightweight tag")
	// A release already linked to another tag keeps its link.
	again := obj{"ref": "3.8.0", "ref_type": "tag", "installation": obj{"id": 77}, "repository": shopRepo}
	e.deliver(hook, again, githubHeaders("s3cret", "create", "t-5", again)...)
	e.DrainJobs()
	eq(t, str(releaseNamed(admin, "SHOP", "3.8.0"), "tagName"), "v3.8.0", "manual link kept")

	// Unlinking: clearing either half of the tag.
	for _, body := range []obj{{"tagRepositoryId": nil, "tagName": nil}, {"tagName": nil}, {"tagRepositoryId": nil}} {
		admin.must("PATCH", "/api/v1/releases/"+str(prev, "id"), obj{"tagRepositoryId": repoID, "tagName": "v3.8.0"}, 200, nil)
		var unlinked obj
		admin.must("PATCH", "/api/v1/releases/"+str(prev, "id"), body, 200, &unlinked)
		if _, ok := unlinked["tagName"]; ok {
			t.Fatalf("%v did not unlink the tag: %v", body, unlinked)
		}
	}
}

func TestGitLabTagsAndDeployments(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("APP", "kanban")
	admin.createIssue("APP", "task", "Login page", nil)
	admin.createIssue("APP", "task", "Logout", nil)
	rel := admin.release("APP", obj{"name": "v2.0"})

	project := obj{"id": 42, "path_with_namespace": "grp/app", "web_url": "https://gitlab.example.com/grp/app", "default_branch": "main"}
	gl := newFakeSCM(t, map[string]any{
		"GET /api/v4/projects":                              []obj{project},
		"GET /api/v4/projects/42/hooks":                     []obj{},
		"GET /api/v4/projects/42/repository/commits/aaa111": obj{"id": "aaa111", "message": "APP-1 login form", "author_name": "Bob", "author_email": "bob@example.com"},
		"GET /api/v4/projects/42/repository/compare": obj{"commits": []obj{
			{"id": "bbb222", "message": "Fix logout (app-2)", "author_name": "Bob"},
			{"id": "ccc333", "message": "Bump version"},
		}},
	})
	var conn obj
	admin.must("POST", "/api/v1/admin/scm-connections", obj{
		"provider": "gitlab", "name": "GitLab", "apiUrl": gl.URL, "webUrl": "https://gitlab.example.com", "token": "glpat-test", "webhookSecret": "gl-secret", "manageHooks": true,
	}, 201, &conn)
	e.DrainJobs()
	var repos []obj
	admin.get("/api/v1/scm/repositories", &repos)
	admin.must("POST", "/api/v1/projects/APP/repositories", obj{"repositoryId": repos[0]["id"]}, 201, nil)
	e.DrainJobs()
	hooks := gl.find("POST", "/api/v4/projects/42/hooks")
	eq(t, len(hooks), 1, "hook created")
	eq(t, hooks[0].Body["tag_push_events"], any(true), "managed hooks subscribe to tag pushes")
	eq(t, hooks[0].Body["deployment_events"], any(true), "managed hooks subscribe to deployments")

	hook := "/hooks/gitlab/" + str(conn, "id")
	glh := func(event, id string) []string {
		return []string{"X-Gitlab-Token", "gl-secret", "X-Gitlab-Event", event, "X-Gitlab-Event-UUID", id}
	}
	// A tag push links the release named "v" + tag; GitLab sends the commit.
	tagPush := obj{"object_kind": "tag_push", "ref": "refs/tags/2.0", "before": strings.Repeat("0", 40), "after": "tagobj", "checkout_sha": "ccc333", "project": project}
	eq(t, e.deliver(hook, tagPush, glh("Tag Push Hook", "g-1")...).Status, 202, "tag push")
	e.DrainJobs()
	d := admin.releaseDetail(str(rel, "id"))
	eq(t, str(d, "tag", "name"), "2.0", "tag linked to v2.0")
	eq(t, str(d, "tag", "sha"), "ccc333", "checkout sha")
	eq(t, str(d, "tag", "url"), "https://gitlab.example.com/grp/app/-/tags/2.0", "GitLab tag page")

	deploy := func(id int, env, ref, sha, status, at string) obj {
		return obj{"object_kind": "deployment", "status": status, "status_changed_at": at, "deployment_id": id, "environment": env,
			"deployable_url": fmt.Sprintf("https://gitlab.example.com/grp/app/-/jobs/%d", id), "project": project, "short_sha": sha[:6],
			"commit_url": "https://gitlab.example.com/grp/app/-/commit/" + sha, "ref": ref, "user": obj{"username": "bob"}}
	}
	// The first deployment to production has its head commit's issues; the next one the commits since.
	e.deliver(hook, deploy(1, "production", "main", "aaa111", "running", "2026-10-20 09:00:00 UTC"), glh("Deployment Hook", "g-2")...)
	e.deliver(hook, deploy(1, "production", "main", "aaa111", "success", "2026-10-20 09:05:00 UTC"), glh("Deployment Hook", "g-3")...)
	e.DrainJobs()
	eq(t, len(gl.find("GET", "/api/v4/projects/42/repository/commits/aaa111")), 1, "head commit read")
	deps := admin.issue("APP-1")["deployments"].([]any)
	eq(t, len(deps), 1, "APP-1 deployed")
	dep := deps[0].(map[string]any)
	eq(t, str(dep, "environment"), "production", "environment")
	eq(t, str(dep, "status"), "success", "status")
	eq(t, str(dep, "url"), "https://gitlab.example.com/grp/app/-/jobs/1", "deployment URL")
	eq(t, str(dep, "deployer"), "bob", "deployer")
	eq(t, str(dep, "sha"), "aaa111", "full sha from the commit URL")
	eq(t, len(admin.issue("APP-2")["deployments"].([]any)), 0, "APP-2 not yet")

	e.deliver(hook, deploy(2, "production", "2.0", "ccc333", "success", "2026-10-21 09:05:00 UTC"), glh("Deployment Hook", "g-4")...)
	e.DrainJobs()
	cmp := gl.find("GET", "/api/v4/projects/42/repository/compare")
	eq(t, len(cmp), 1, "compared with the previous deployment")
	eq(t, len(admin.issue("APP-2")["deployments"].([]any)), 1, "APP-2 deployed by the second deployment")
	// The release's tag reached production.
	d = admin.releaseDetail(str(rel, "id"))
	envs := d["environments"].([]any)
	if len(envs) != 1 || str(envs[0].(map[string]any), "environment") != "production" || str(envs[0].(map[string]any), "via") != "tag" {
		t.Fatalf("environments = %v", envs)
	}
}

func TestGitHubDeployments(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("SHOP", "kanban")
	for _, s := range []string{"Cart", "Totals", "Logs", "Risky change", "Not deployed"} {
		admin.createIssue("SHOP", "task", s, nil)
	}
	commit := func(sha, msg string) obj {
		return obj{"sha": sha, "html_url": "https://github.com/acme/shop/commit/" + sha, "commit": obj{"message": msg, "author": obj{"name": "Ada", "email": "ada@example.com"}}}
	}
	gh, hook, _ := githubRepo(t, e, admin, "SHOP", map[string]any{
		"GET /repos/acme/shop/commits/sha1":        commit("sha1", "Add the cart"),
		"GET /repos/acme/shop/commits/sha3":        commit("sha3", "Tidy up (shop-3)"),
		"GET /repos/acme/shop/commits/sha6":        commit("sha6", "SHOP-4 make it safe"),
		"GET /repos/acme/shop/compare/sha1...sha3": obj{"total_commits": 2, "commits": []obj{commit("sha2", "SHOP-2 totals"), commit("sha3", "Tidy up (shop-3)")}},
		"GET /repos/acme/shop/compare/sha3...sha4": obj{"total_commits": 1, "commits": []obj{commit("sha4", "SHOP-4 risky")}},
	})
	// sha1 is already linked to SHOP-1 by a push, although its message names no issue.
	push := obj{"ref": "refs/heads/SHOP-1-cart", "installation": obj{"id": 77}, "repository": shopRepo, "commits": []obj{{"id": "sha1", "message": "SHOP-1 add the cart", "distinct": true, "timestamp": time.Now().Format(time.RFC3339), "author": obj{"name": "Ada", "email": "ada@example.com"}}}}
	e.deliver(hook, push, githubHeaders("s3cret", "push", "p-1", push)...)
	e.DrainJobs()

	// Rules: production deployments finish issues; failed ones label them. Off rules do nothing.
	admin.must("POST", "/api/v1/projects/SHOP/automation-rules", obj{
		"name": "Move issues to Done when they reach production", "trigger": obj{"type": "deployment.completed", "environment": "Production", "status": "success"},
		"actions": []obj{{"type": "transition", "category": "done"}, {"type": "comment", "body": "Deployed to {{event.environment}} by {{event.deployer}}"}},
	}, 201, nil)
	admin.must("POST", "/api/v1/projects/SHOP/automation-rules", obj{
		"name": "Flag failed deployments", "trigger": obj{"type": "deployment.completed", "status": "failed"},
		"actions": []obj{{"type": "add_label", "label": "deploy-failed"}},
	}, 201, nil)
	admin.must("POST", "/api/v1/projects/SHOP/automation-rules", obj{"name": "bad", "trigger": obj{"type": "deployment.completed", "status": "meh"}, "actions": []obj{{"type": "add_label", "label": "x"}}}, 400, nil)
	rcv := newReceiver(t)
	admin.must("POST", "/api/v1/webhooks", obj{"projectKey": "SHOP", "name": "Deploys", "url": rcv.URL, "events": []string{"deployment.completed"}}, 201, nil)

	n := 0
	status := func(id int, env, sha, state, at string) {
		n++
		p := obj{"action": "created", "installation": obj{"id": 77}, "repository": shopRepo,
			"deployment_status": obj{"state": state, "environment": env, "log_url": fmt.Sprintf("https://github.com/acme/shop/actions/runs/%d", id), "environment_url": "https://shop.example.com", "created_at": at},
			"deployment":        obj{"id": id, "sha": sha, "ref": "main", "environment": env, "creator": obj{"login": "deploy-bot"}}}
		eq(t, e.deliver(hook, p, githubHeaders("s3cret", "deployment_status", fmt.Sprintf("ds-%d", n), p)...).Status, 202, "deployment status")
		e.DrainJobs()
	}
	status(1, "production", "sha1", "in_progress", "2026-10-20T09:00:00Z")
	eq(t, len(admin.issue("SHOP-1")["deployments"].([]any)), 0, "nothing until it completes")
	status(1, "production", "sha1", "success", "2026-10-20T09:05:00Z")
	// An older status arriving late does not undo the outcome.
	status(1, "production", "sha1", "in_progress", "2026-10-20T09:01:00Z")
	eq(t, len(gh.find("GET", "/repos/acme/shop/commits/sha1")), 1, "first deployment: the head commit only")
	got := admin.issue("SHOP-1")
	eq(t, str(got, "status", "name"), "Done", "automation moved SHOP-1 to Done")
	dep := got["deployments"].([]any)[0].(map[string]any)
	eq(t, str(dep, "status"), "success", "still successful")
	eq(t, str(dep, "url"), "https://github.com/acme/shop/actions/runs/1", "log URL")
	eq(t, str(dep, "repository", "fullPath"), "acme/shop", "repository")

	status(2, "staging", "sha3", "success", "2026-10-20T10:00:00Z") // first to staging: sha3 only
	status(3, "production", "sha3", "success", "2026-10-21T09:00:00Z")
	eq(t, len(gh.find("GET", "/repos/acme/shop/compare/sha1...sha3")), 1, "compared with the previous production deployment")
	for _, k := range []string{"SHOP-2", "SHOP-3"} {
		eq(t, str(admin.issue(k), "status", "name"), "Done", k+" reached production")
	}
	sameStrings(t, admin.search(`project = SHOP AND deployedTo = production ORDER BY key`), []string{"SHOP-1", "SHOP-2", "SHOP-3"}, "deployedTo production")
	sameStrings(t, admin.search(`project = SHOP AND deployedTo = staging`), []string{"SHOP-3"}, "deployedTo staging")
	sameStrings(t, admin.search(`project = SHOP AND deployedTo is EMPTY ORDER BY key`), []string{"SHOP-4", "SHOP-5"}, "never deployed")

	// A failed deployment: the issues get the failure, the success rule does not run.
	status(4, "production", "sha4", "failure", "2026-10-22T09:00:00Z")
	got = admin.issue("SHOP-4")
	eq(t, str(got, "status", "name"), "Backlog", "a failed deployment does not finish the issue")
	eq(t, fmt.Sprint(got["labels"]), "[deploy-failed]", "failure rule ran")
	eq(t, str(got["deployments"].([]any)[0].(map[string]any), "status"), "failed", "failed deployment shown")
	// When the comparison is impossible (the base is gone), the head commit is used.
	status(5, "production", "sha6", "success", "2026-10-23T09:00:00Z")
	eq(t, len(gh.find("GET", "/repos/acme/shop/commits/sha6")), 1, "fell back to the head commit")
	got = admin.issue("SHOP-4")
	eq(t, str(got, "status", "name"), "Done", "SHOP-4 reached production in the end")
	eq(t, str(got["deployments"].([]any)[0].(map[string]any), "status"), "success", "the success wins over the earlier failure")

	// A redeploy of the same commit, and a redelivered status, raise nothing new.
	runsBefore := countRows(e, `SELECT count(*) FROM automation_runs`)
	status(6, "production", "sha6", "success", "2026-10-24T09:00:00Z")
	status(1, "production", "sha1", "success", "2026-10-20T09:05:00Z")
	eq(t, countRows(e, `SELECT count(*) FROM automation_runs`), runsBefore, "no new automation runs")

	var comments []obj
	admin.get("/api/v1/issues/SHOP-2/comments", &comments)
	eq(t, str(comments[0], "body"), "Deployed to production by deploy-bot", "event data in templates")
	var events []string
	for _, ev := range rcv.events() {
		if ev == "deployment.completed" {
			events = append(events, ev)
		}
	}
	// SHOP-1, SHOP-3 (staging), SHOP-2, SHOP-3 (production), SHOP-4 (failed), SHOP-4 (fallback).
	eq(t, len(events), 6, "a webhook delivery per issue and outcome")

	// Board cards say where an issue is deployed.
	var boards []obj
	admin.get("/api/v1/projects/SHOP/boards", &boards)
	var data obj
	admin.get("/api/v1/boards/"+str(boards[0], "id")+"/data", &data)
	found := false
	for _, col := range data["columns"].([]any) {
		for _, card := range col.(map[string]any)["issues"].([]any) {
			c := card.(map[string]any)
			if str(c, "key") == "SHOP-3" {
				found = fmt.Sprint(c["dev"].(map[string]any)["deployedTo"]) == "[production staging]"
			}
		}
	}
	if !found {
		t.Fatal("SHOP-3's card does not list its environments")
	}

	// A release reaches an environment once every one of its done issues did.
	rel := admin.release("SHOP", obj{"name": "1.0"})
	for _, k := range []string{"SHOP-1", "SHOP-2", "SHOP-5"} {
		admin.must("PATCH", "/api/v1/issues/"+k, obj{"releaseIds": []string{str(rel, "id")}}, 200, nil)
	}
	envs := admin.releaseDetail(str(rel, "id"))["environments"].([]any)
	if len(envs) != 1 || str(envs[0].(map[string]any), "environment") != "production" || str(envs[0].(map[string]any), "via") != "issues" {
		t.Fatalf("environments = %v", envs)
	}
}

func countRows(e *Env, sql string) int {
	e.t.Helper()
	var n int
	if err := e.Pool.QueryRow(e.t.Context(), sql).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
