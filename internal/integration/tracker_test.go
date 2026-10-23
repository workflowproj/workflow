package integration

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProjectAccessAndRoles(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin") // first user becomes org admin
	eq(t, str(admin.Me, "orgRole"), "admin", "first user role")
	member := e.login("mel@example.com", "Mel")
	eq(t, str(member.Me, "orgRole"), "member", "second user role")

	admin.createProject("OPEN", "scrum")
	var priv obj
	admin.must("POST", "/api/v1/projects", obj{"key": "SECRET", "name": "Secret", "template": "kanban", "access": "private"}, 201, &priv)
	eq(t, str(priv, "access"), "private", "access")

	// Open projects are visible to members with the member role; private ones are hidden (404, not 403).
	var list []obj
	member.get("/api/v1/projects", &list)
	sameStrings(t, keys(list), []string{"OPEN"}, "member project list")
	member.must("GET", "/api/v1/projects/SECRET", nil, 404, nil)
	member.createIssue("OPEN", "task", "Members can create in open projects", nil)
	member.must("POST", "/api/v1/issues", obj{"projectKey": "SECRET", "typeKey": "task", "summary": "x"}, 404, nil)

	// Grant access through a group mapped to a custom role.
	var role obj
	admin.must("POST", "/api/v1/admin/roles", obj{"key": "triager", "name": "Triager", "permissions": []string{"project.browse", "issue.comment"}}, 201, &role)
	admin.must("POST", "/api/v1/admin/roles", obj{"key": "bad", "name": "Bad", "permissions": []string{"issue.create"}}, 400, nil) // browse required
	var group obj
	admin.must("POST", "/api/v1/admin/groups", obj{"displayName": "QA"}, 201, &group)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/groups/%s/members/%s", group["id"], member.id()), nil, 204, nil)
	admin.must("POST", "/api/v1/projects/SECRET/members", obj{"groupId": group["id"], "roleKey": "triager"}, 201, nil)

	var p obj
	member.get("/api/v1/projects/SECRET", &p)
	perms := fmt.Sprint(p["myPermissions"])
	if !strings.Contains(perms, "issue.comment") || strings.Contains(perms, "issue.create") {
		t.Fatalf("triager permissions = %v", perms)
	}
	issue := admin.createIssue("SECRET", "task", "Needs triage", nil)
	member.must("POST", "/api/v1/issues", obj{"projectKey": "SECRET", "typeKey": "task", "summary": "x"}, 403, nil)
	member.must("POST", "/api/v1/issues/"+str(issue, "key")+"/comments", obj{"body": "Looks like a dupe"}, 201, nil)
	member.must("PATCH", "/api/v1/issues/"+str(issue, "key"), obj{"summary": "renamed"}, 403, nil)

	// An explicit viewer membership restricts a member even on an open project.
	var members []obj
	admin.must("POST", "/api/v1/projects/OPEN/members", obj{"userId": member.id(), "roleKey": "viewer"}, 201, nil)
	admin.get("/api/v1/projects/OPEN/members", &members)
	member.must("POST", "/api/v1/issues", obj{"projectKey": "OPEN", "typeKey": "task", "summary": "x"}, 403, nil)

	// Guests see nothing but explicit memberships.
	guest := e.login("guest@example.com", "Guest")
	admin.must("PATCH", "/api/v1/admin/users/"+guest.id(), obj{"orgRole": "guest"}, 200, nil)
	guest.get("/api/v1/projects", &list)
	eq(t, len(list), 0, "guest projects")
	guest.must("POST", "/api/v1/projects", obj{"key": "GUEST", "name": "x", "template": "scrum"}, 403, nil)

	// Renaming a key keeps the old key resolving.
	admin.must("PATCH", "/api/v1/projects/OPEN", obj{"key": "WIDE"}, 200, &p)
	eq(t, str(p, "key"), "WIDE", "renamed key")
	old := admin.issue("OPEN-1")
	eq(t, str(old, "key"), "WIDE-1", "issue resolved through alias")
	admin.must("POST", "/api/v1/projects", obj{"key": "OPEN", "name": "x", "template": "scrum"}, 409, nil)

	// Organisation admin guard rails.
	admin.must("PATCH", "/api/v1/admin/users/"+str(admin.Me, "id"), obj{"orgRole": "member"}, 400, nil)
	member.must("GET", "/api/v1/admin/users", nil, 403, nil)
}

func TestIssueHierarchyFieldsAndConcurrency(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("PLAT", "scrum")

	epic := c.createIssue("PLAT", "epic", "Checkout", nil)
	story := c.createIssue("PLAT", "story", "Pay by card", obj{"parentKey": epic["key"], "storyPoints": 5, "labels": []string{"payments", "web app"}})
	eq(t, fmt.Sprint(story["labels"]), "[payments web-app]", "labels normalised")
	sub := c.createIssue("PLAT", "subtask", "Card form", obj{"parentKey": story["key"]})
	eq(t, str(sub, "parent", "key"), str(story, "key"), "subtask parent")

	// Hierarchy rules: parent level = child level + 1.
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLAT", "typeKey": "subtask", "summary": "orphan"}, 400, nil)
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLAT", "typeKey": "subtask", "summary": "x", "parentKey": epic["key"]}, 400, nil)
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLAT", "typeKey": "epic", "summary": "x", "parentKey": epic["key"]}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"typeKey": "epic"}, 400, nil) // has children

	// Optimistic locking.
	v := int(story["version"].(float64))
	c.must("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"summary": "Pay by card or wallet", "version": v}, 200, nil)
	r := c.do("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"summary": "stale write", "version": v})
	eq(t, r.Status, 409, "stale version")
	eq(t, str(r.JSON(), "code"), "version_conflict", "conflict code")

	// Custom fields are validated against their definitions.
	c.must("POST", "/api/v1/fields", obj{"key": "customer", "name": "Customer", "type": "select", "options": []string{"Acme", "Globex"}}, 201, nil)
	c.must("POST", "/api/v1/fields", obj{"key": "risk", "name": "Risk score", "type": "number", "issueTypeKeys": []string{"story", "bug"}}, 201, nil)
	c.must("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"fields": obj{"customer": "Initech"}}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"fields": obj{"customer": "Acme", "risk": 7}}, 200, nil)
	c.must("PATCH", "/api/v1/issues/"+str(epic, "key"), obj{"fields": obj{"risk": 3}}, 400, nil) // not for epics
	c.must("PATCH", "/api/v1/issues/"+str(sub, "key"), obj{"fields": obj{"nope": 1}}, 400, nil)
	got := c.issue(str(story, "key"))
	eq(t, fmt.Sprint(got["fields"]), "map[customer:Acme risk:7]", "custom fields")

	// Sprints: epics cannot be scheduled, sub-tasks follow their parent.
	var sprints []obj
	c.get("/api/v1/projects/PLAT/sprints", &sprints)
	sprintID := str(sprints[0], "id")
	c.must("PATCH", "/api/v1/issues/"+str(epic, "key"), obj{"sprintId": sprintID}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"sprintId": sprintID}, 200, nil)
	eq(t, str(c.issue(str(sub, "key")), "sprintId"), sprintID, "subtask follows parent sprint")

	// Links appear on both sides with the right labels.
	bug := c.createIssue("PLAT", "bug", "Card declined", nil)
	c.must("POST", "/api/v1/issues/"+str(bug, "key")+"/links", obj{"type": "blocks", "targetKey": story["key"]}, 201, nil)
	c.must("POST", "/api/v1/issues/"+str(bug, "key")+"/links", obj{"type": "blocks", "targetKey": story["key"]}, 409, nil)
	s := c.issue(str(story, "key"))
	links := s["links"].([]any)
	eq(t, len(links), 1, "links on target")
	eq(t, str(links[0].(obj), "label"), "is blocked by", "inward label")

	// Epic progress rolls up children by status category.
	d := c.issue(str(epic, "key"))
	eq(t, fmt.Sprint(d["progress"].(obj)["total"]), "1", "epic children")

	// Watchers, comments, history.
	eq(t, d["watching"], true, "reporter watches")
	c.must("POST", "/api/v1/issues/"+str(story, "key")+"/comments", obj{"body": "Ship it"}, 201, nil)
	var hist []obj
	c.get("/api/v1/issues/"+str(story, "key")+"/history", &hist)
	fields := []string{}
	for _, h := range hist {
		fields = append(fields, str(h, "field"))
	}
	for _, want := range []string{"created", "summary", "Customer", "Risk score", "sprint"} {
		if !strings.Contains(strings.Join(fields, ","), want) {
			t.Fatalf("history %v missing %s", fields, want)
		}
	}

	// Deleting a story removes its sub-tasks; an epic's children are un-parented.
	c.must("DELETE", "/api/v1/issues/"+str(story, "key"), nil, 204, nil)
	c.must("GET", "/api/v1/issues/"+str(sub, "key"), nil, 404, nil)
}

func TestRankingAndMove(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("RNK", "kanban")
	var ids []string
	for i := 1; i <= 4; i++ {
		ids = append(ids, str(c.createIssue("RNK", "task", fmt.Sprintf("T%d", i), nil), "id"))
	}
	// Move T4 to the top, then T1 between T2 and T3.
	c.must("POST", "/api/v1/issues/RNK-4/move", obj{"beforeIssueId": ids[0]}, 200, nil)
	c.must("POST", "/api/v1/issues/RNK-1/move", obj{"afterIssueId": ids[1]}, 200, nil)
	sameStrings(t, c.search("project = RNK ORDER BY rank"), []string{"RNK-4", "RNK-2", "RNK-1", "RNK-3"}, "rank order")

	// Moving to another column runs a transition; the Kanban flow allows any-to-any.
	r := c.must("POST", "/api/v1/issues/RNK-2/move", obj{"statusId": c.statusID("In Progress")}, 200, nil)
	eq(t, str(r.JSON(), "status", "name"), "In Progress", "moved status")
	eq(t, str(r.JSON(), "assignee", "displayName"), "Ada", "start-work post-function assigned the mover")
}

func TestWorkflowConditionsScreensAndVersioning(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	member := e.login("mel@example.com", "Mel")
	admin.createProject("BUG", "bug_tracking")
	bug := member.createIssue("BUG", "bug", "Crash on save", nil)
	key := str(bug, "key")

	member.transition(key, "triage", 200)
	member.transition(key, "start-work", 200)
	member.transition(key, "ready-for-qa", 200)

	// "Pass QA" has a screen with a required resolution.
	var avail []obj
	member.get("/api/v1/issues/"+key+"/transitions", &avail)
	var pass obj
	for _, a := range avail {
		if str(a, "id") == "pass-qa" {
			pass = a
		}
	}
	if pass == nil || len(pass["screen"].([]any)) != 2 {
		t.Fatalf("pass-qa screen missing: %v", avail)
	}
	r := member.do("POST", "/api/v1/issues/"+key+"/transitions", obj{"transitionId": "pass-qa"})
	eq(t, r.Status, 422, "missing required screen field")
	eq(t, str(r.JSON(), "code"), "screen_invalid", "screen error code")
	member.must("POST", "/api/v1/issues/"+key+"/transitions", obj{"transitionId": "pass-qa", "fields": obj{"resolution": "Duplicate"}, "comment": "Dupe of BUG-9"}, 200, nil)
	got := member.issue(key)
	eq(t, str(got, "status", "name"), "Closed", "closed")
	eq(t, str(got, "resolution"), "Duplicate", "screen resolution")
	var comments []obj
	member.get("/api/v1/issues/"+key+"/comments", &comments)
	eq(t, len(comments), 1, "screen comment")

	// Drag-and-drop to a status whose transition needs a screen reports screen_required.
	other := member.createIssue("BUG", "bug", "Slow", nil)
	r = member.do("POST", "/api/v1/issues/"+str(other, "key")+"/move", obj{"statusId": member.statusID("Won't Fix")})
	eq(t, str(r.JSON(), "code"), "screen_required", "won't fix via drag")

	// Publish v2 of "Bug triage" with a condition that only admins can triage, removing "Ready for QA".
	var wfs []obj
	admin.get("/api/v1/workflows?project=BUG", &wfs)
	var wfID string
	for _, w := range wfs {
		if str(w, "name") == "Bug triage" {
			wfID = str(w, "id")
		}
	}
	v2 := `
name: Bug triage
statuses:
  - { name: Open, category: todo }
  - { name: Triaged, category: todo }
  - { name: In Progress, category: in_progress }
  - { name: Closed, category: done }
  - { name: Won't Fix, category: done }
transitions:
  - { name: Triage, from: [Open], to: Triaged, condition: 'actor.hasRole("admin")' }
  - { name: Start work, from: [Triaged], to: In Progress, postFunctions: [{ assignTo: actor }] }
  - { name: Close, from: [In Progress], to: Closed, screen: [{ field: resolution, required: true }] }
  - { name: Won't fix, to: Won't Fix }
  - { name: Reopen, from: [Closed, Won't Fix], to: Open }
`
	inQA := member.createIssue("BUG", "bug", "Stuck in QA", nil)
	member.transition(str(inQA, "key"), "triage", 200)
	member.transition(str(inQA, "key"), "start-work", 200)
	member.transition(str(inQA, "key"), "ready-for-qa", 200)

	var val obj
	admin.must("POST", "/api/v1/workflows/validate", obj{"yaml": v2, "workflowId": wfID}, 200, &val)
	eq(t, val["valid"], true, "v2 valid")
	eq(t, str(val["removedStatuses"].([]any)[0].(obj), "name"), "Ready for QA", "removed status reported")

	r = admin.do("POST", "/api/v1/workflows/"+wfID+"/versions", obj{"yaml": v2})
	eq(t, r.Status, 422, "mapping required")
	eq(t, str(r.JSON(), "code"), "status_mapping_required", "mapping code")
	member.must("POST", "/api/v1/workflows/"+wfID+"/versions", obj{"yaml": v2, "statusMapping": obj{"Ready for QA": "In Progress"}}, 403, nil)
	var d obj
	admin.must("POST", "/api/v1/workflows/"+wfID+"/versions", obj{"yaml": v2, "statusMapping": obj{"Ready for QA": "In Progress"}}, 201, &d)
	eq(t, fmt.Sprint(d["currentVersion"]), "2", "version 2")
	eq(t, d["pendingMigration"], true, "issues still on v1 until the job runs")

	// Before the migration job runs, the issue keeps v1 rules (pinned version).
	eq(t, str(member.issue(str(inQA, "key")), "status", "name"), "Ready for QA", "pinned before migration")
	e.DrainJobs()
	eq(t, str(member.issue(str(inQA, "key")), "status", "name"), "In Progress", "migrated status")

	// v2 condition: members no longer see Triage, admins do.
	fresh := member.createIssue("BUG", "bug", "New one", nil)
	member.get("/api/v1/issues/"+str(fresh, "key")+"/transitions", &avail)
	for _, a := range avail {
		if str(a, "id") == "triage" {
			t.Fatal("member should not see admin-only Triage")
		}
	}
	member.transition(str(fresh, "key"), "triage", 403)
	admin.transition(str(fresh, "key"), "triage", 200)

	// Workflows as code: applying the same document is a no-op, a change publishes v3.
	var res obj
	admin.must("PUT", "/api/v1/workflows/apply", obj{"projectKey": "BUG", "yaml": v2}, 200, &res)
	eq(t, str(res, "action"), "unchanged", "apply unchanged")
	admin.must("PUT", "/api/v1/workflows/apply", obj{"projectKey": "BUG", "yaml": strings.Replace(v2, "Start work", "Begin", 1), "dryRun": true}, 200, &res)
	eq(t, str(res, "action"), "published", "dry run reports publish")
	admin.get("/api/v1/workflows/"+wfID, &d)
	eq(t, fmt.Sprint(d["currentVersion"]), "2", "dry run did not publish")

	// Statuses are global; reusing a name with another category is rejected.
	r = admin.do("POST", "/api/v1/workflows", obj{"projectKey": "BUG", "yaml": "name: Odd\nstatuses: [{name: Closed, category: todo}]\ntransitions: []\n"})
	eq(t, r.Status, 422, "category clash")

	// Remap the task type to another workflow; statuses map through the migration job.
	var types []obj
	admin.get("/api/v1/projects/BUG/issue-types", &types)
	task := member.createIssue("BUG", "task", "Write docs", nil)
	member.must("POST", "/api/v1/issues/"+str(task, "key")+"/move", obj{"statusId": member.statusID("In Progress")}, 200, nil)
	var typeID string
	for _, ty := range types {
		if str(ty, "key") == "task" {
			typeID = str(ty, "id")
		}
	}
	r = admin.do("PATCH", "/api/v1/projects/BUG/issue-types/"+typeID, obj{"workflowId": wfID})
	eq(t, r.Status, 200, "In Progress exists in Bug triage, so no mapping needed: "+string(r.Body))
	e.DrainJobs()
	eq(t, str(member.issue(str(task, "key")), "status", "name"), "In Progress", "task kept its status")
}

func TestBoardsSprintsAndReports(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("SCR", "scrum")
	var boards []obj
	c.get("/api/v1/projects/SCR/boards", &boards)
	board := str(boards[0], "id")
	var sprints []obj
	c.get("/api/v1/projects/SCR/sprints", &sprints)
	sprint := str(sprints[0], "id")

	a := c.createIssue("SCR", "story", "A", obj{"sprintId": sprint, "storyPoints": 3})
	b := c.createIssue("SCR", "story", "B", obj{"sprintId": sprint, "storyPoints": 5})
	c.createIssue("SCR", "task", "Backlog item", nil)
	c.createIssue("SCR", "epic", "Not on boards", nil)

	// No active sprint yet: empty scrum board.
	var data obj
	c.get("/api/v1/boards/"+board+"/data", &data)
	if data["sprint"] != nil {
		t.Fatal("expected no active sprint")
	}
	var backlog obj
	c.get("/api/v1/boards/"+board+"/backlog", &backlog)
	eq(t, len(backlog["sprints"].([]any)[0].(obj)["issues"].([]any)), 2, "sprint issues")
	eq(t, len(backlog["backlog"].([]any)), 1, "backlog excludes epics and sprint issues")

	start := time.Now().UTC().Format("2006-01-02")
	end := time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02")
	c.must("POST", "/api/v1/sprints/"+sprint+"/start", obj{"startDate": start, "endDate": end, "goal": "Ship"}, 200, nil)
	var next obj
	c.must("POST", "/api/v1/projects/SCR/sprints", obj{}, 201, &next)
	c.must("POST", "/api/v1/sprints/"+str(next, "id")+"/start", obj{"startDate": start, "endDate": end}, 409, nil)

	c.get("/api/v1/boards/"+board+"/data", &data)
	cols := data["columns"].([]any)
	eq(t, str(cols[0].(obj), "name"), "To Do", "first column")
	eq(t, len(cols[0].(obj)["issues"].([]any)), 2, "active sprint issues on board")

	// Finish A, then complete the sprint moving B to the next sprint.
	c.transition(str(a, "key"), "start-work", 200)
	c.transition(str(a, "key"), "done", 200)
	var rep obj
	c.get("/api/v1/sprints/"+sprint+"/report", &rep)
	eq(t, fmt.Sprint(rep["committedPoints"]), "8", "committed points snapshot")
	eq(t, fmt.Sprint(rep["completedPoints"]), "3", "completed points")
	c.must("POST", "/api/v1/sprints/"+sprint+"/complete", obj{"moveOpenIssuesTo": next["id"]}, 200, nil)
	eq(t, str(c.issue(str(b, "key")), "sprintId"), str(next, "id"), "open issue moved to next sprint")
	c.get("/api/v1/sprints/"+sprint+"/report", &rep)
	eq(t, fmt.Sprint(rep["completedIssues"]), "1", "completed issues after close")

	// WIP limits on a Kanban board.
	c.createProject("KAN", "kanban")
	c.get("/api/v1/projects/KAN/boards", &boards)
	kb := str(boards[0], "id")
	inProgress := c.statusID("In Progress")
	for i := 0; i < 6; i++ {
		iss := c.createIssue("KAN", "task", fmt.Sprintf("K%d", i), nil)
		c.must("POST", "/api/v1/issues/"+str(iss, "key")+"/move", obj{"statusId": inProgress}, 200, nil)
	}
	c.get("/api/v1/boards/"+kb+"/data", &data)
	for _, col := range data["columns"].([]any) {
		cm := col.(obj)
		if str(cm, "name") == "In Progress" {
			eq(t, cm["overLimit"], true, "WIP limit exceeded")
			eq(t, fmt.Sprint(cm["wipLimit"]), "5", "WIP limit")
		}
	}
	// Quick filters use JQL.
	c.get("/api/v1/boards/"+kb+"/data?jql="+url.QueryEscape(`summary ~ "K1"`), &data)
	total := 0
	for _, col := range data["columns"].([]any) {
		total += len(col.(obj)["issues"].([]any))
	}
	eq(t, total, 1, "quick filter")

	// Releases.
	var rel obj
	c.must("POST", "/api/v1/projects/SCR/releases", obj{"name": "1.0"}, 201, &rel)
	c.must("PATCH", "/api/v1/issues/"+str(a, "key"), obj{"releaseIds": []string{str(rel, "id")}}, 200, nil)
	c.must("PATCH", "/api/v1/issues/"+str(b, "key"), obj{"releaseIds": []string{str(rel, "id")}}, 200, nil)
	var rels []obj
	c.get("/api/v1/projects/SCR/releases", &rels)
	eq(t, fmt.Sprint(rels[0]["issueCount"], "/", rels[0]["doneCount"]), "2/1", "release progress")
	var rel2 obj
	c.must("POST", "/api/v1/projects/SCR/releases", obj{"name": "1.1"}, 201, &rel2)
	c.must("POST", "/api/v1/releases/"+str(rel, "id")+"/release", obj{"moveOpenIssuesTo": rel2["id"]}, 200, nil)
	sameStrings(t, c.search(`fixVersion = "1.1"`), []string{str(b, "key")}, "open issue moved to next release")
	sameStrings(t, c.search(`fixVersion in releasedVersions()`), []string{str(a, "key")}, "released versions")

	// Cycle time comes from history.
	var ct obj
	c.get("/api/v1/projects/SCR/reports/cycle-time?days=7", &ct)
	eq(t, len(ct["issues"].([]any)), 1, "cycle time issues")
}

func TestJQLSearch(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("JQ", "scrum")
	ada.must("POST", "/api/v1/fields", obj{"key": "customer", "name": "Customer", "type": "select", "options": []string{"Acme", "Globex"}}, 201, nil)
	i1 := ada.createIssue("JQ", "bug", "Login fails on Safari", obj{"priority": "highest", "labels": []string{"auth"}, "assigneeId": ada.id(), "fields": obj{"customer": "Acme"}})
	i2 := ada.createIssue("JQ", "story", "Dark mode", obj{"priority": "low", "assigneeId": bob.id(), "description": "Users keep asking about night themes"})
	i3 := ada.createIssue("JQ", "task", "Upgrade Postgres", obj{"priority": "high", "dueDate": time.Now().AddDate(0, 0, 3).Format("2006-01-02")})
	ada.transition(str(i3, "key"), "start-work", 200)
	k1, k2, k3 := str(i1, "key"), str(i2, "key"), str(i3, "key")

	cases := map[string][]string{
		`project = JQ ORDER BY key`:                                        {k1, k2, k3},
		`assignee = currentUser() ORDER BY key`:                            {k1, k3}, // start work assigned Ada
		`assignee = "bob@example.com"`:                                     {k2},
		`priority >= High ORDER BY priority DESC`:                          {k1, k3},
		`type = Bug OR labels = auth`:                                      {k1},
		`text ~ "night themes"`:                                            {k2},
		`summary ~ postgres`:                                               {k3},
		`statusCategory = "In Progress"`:                                   {k3},
		`status != Done AND due <= "+7d" ORDER BY due`:                     {k3},
		`customer = Acme`:                                                  {k1},
		`key in (JQ-1, JQ-3) ORDER BY key DESC`:                            {k3, k1},
		`labels is EMPTY AND type != task ORDER BY created`:                {k2},
		`created >= startOfDay() AND resolution = Unresolved ORDER BY key`: {k1, k2, k3},
		`watcher = currentUser() AND key > JQ-1 ORDER BY key`:              {k2, k3},
	}
	for jql, want := range cases {
		sameStrings(t, ada.search(jql), want, jql)
	}
	// Bob can see the open project too; JQL results are always limited to browsable projects.
	ada.must("POST", "/api/v1/projects", obj{"key": "HID", "name": "Hidden", "template": "kanban", "access": "private"}, 201, nil)
	ada.createIssue("HID", "task", "Secret plan", nil)
	sameStrings(t, bob.search(`summary ~ "Secret"`), []string{}, "private project excluded")

	r := ada.do("GET", "/api/v1/search?jql="+url.QueryEscape(`colour = red`), nil)
	eq(t, r.Status, 400, "unknown field")
	eq(t, fmt.Sprint(r.JSON()["position"]), "0", "error position")

	// Saved filters.
	var f obj
	ada.must("POST", "/api/v1/filters", obj{"name": "My bugs", "jql": "type = Bug AND assignee = currentUser()", "shared": true}, 201, &f)
	ada.must("POST", "/api/v1/filters", obj{"name": "Broken", "jql": "type = "}, 400, nil)
	var fs []obj
	bob.get("/api/v1/filters", &fs)
	eq(t, len(fs), 1, "shared filter visible")
	bob.must("DELETE", "/api/v1/filters/"+str(f, "id"), nil, 403, nil)
}
