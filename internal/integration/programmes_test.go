package integration

import (
	"net/url"
	"strings"
	"testing"
)

// programmeSetup makes a programme (PROG) with two team projects (APP, API) and a project outside
// it (OUT). Ada creates everything.
func programmeSetup(t *testing.T) (*Env, *Client) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	ada.createProject("PROG", "programme")
	ada.createProject("APP", "scrum")
	ada.createProject("API", "kanban")
	ada.createProject("OUT", "kanban")
	ada.must("POST", "/api/v1/projects/PROG/programme/teams", obj{"projectKey": "APP"}, 201, nil)
	ada.must("POST", "/api/v1/projects/PROG/programme/teams", obj{"projectKey": "API"}, 201, nil)
	return e, ada
}

func TestProgrammeTeams(t *testing.T) {
	e, ada := programmeSetup(t)
	bob := e.login("bob@example.com", "Bob")
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "scrum", "access": "private"}, 201, nil)
	ada.createProject("DESK", "service")
	ada.createProject("PROG2", "programme")

	var teams obj
	ada.must("POST", "/api/v1/projects/PROG/programme/teams", obj{"projectKey": "SEC"}, 201, &teams)
	eq(t, len(teams["teams"].([]any)), 3, "ada sees three teams")

	// Bob browses the open programme, but not the private team: it is counted, not named.
	bob.get("/api/v1/projects/PROG/programme/teams", &teams)
	eq(t, len(teams["teams"].([]any)), 2, "bob sees two teams")
	eq(t, teams["hiddenCount"].(float64), 1, "one hidden team")

	// Only programme admins add teams, and only teams they can browse.
	bob.must("POST", "/api/v1/projects/PROG/programme/teams", obj{"projectKey": "OUT"}, 403, nil)
	ada.must("POST", "/api/v1/projects/PROG2/members", obj{"userId": bob.id(), "roleKey": "admin"}, 201, nil)
	r := bob.do("POST", "/api/v1/projects/PROG2/programme/teams", obj{"projectKey": "SEC"})
	eq(t, r.Status, 400, "a team bob can't browse")
	if !strings.Contains(string(r.Body), "no project SEC") {
		t.Errorf("hidden team error should read like a missing project: %s", r.Body)
	}
	for key, want := range map[string]string{"PROG2": "own teams", "PROG": "is a programme", "DESK": "service project", "NOPE": "no project"} {
		r := ada.do("POST", "/api/v1/projects/PROG2/programme/teams", obj{"projectKey": key})
		if r.Status != 400 || !strings.Contains(string(r.Body), want) {
			t.Errorf("adding %s: %d %s, want 400 mentioning %q", key, r.Status, r.Body, want)
		}
	}

	// Only programmes have teams.
	ada.must("GET", "/api/v1/projects/APP/programme/teams", nil, 404, nil)
	ada.must("POST", "/api/v1/projects/APP/programme/teams", obj{"projectKey": "API"}, 404, nil)

	// A team lists the programmes it belongs to that the caller can browse.
	var progs []obj
	bob.get("/api/v1/projects/APP/programmes", &progs)
	sameStrings(t, keys(progs), []string{"PROG"}, "APP's programmes")

	// Removing a team; removing it again is not found.
	var removed obj
	ada.must("DELETE", "/api/v1/projects/PROG/programme/teams/API", nil, 200, &removed)
	eq(t, removed["unlinkedEpics"].(float64), 0, "no epics to unlink")
	ada.must("DELETE", "/api/v1/projects/PROG/programme/teams/API", nil, 404, nil)
	bob.must("DELETE", "/api/v1/projects/PROG/programme/teams/APP", nil, 403, nil)
}

func TestCrossProjectParent(t *testing.T) {
	e, ada := programmeSetup(t)
	key := func(i obj) string { return str(i, "key") }
	init1 := key(ada.createIssue("PROG", "initiative", "Self-serve checkout", obj{"dueDate": "2026-12-18"}))
	init2 := key(ada.createIssue("PROG", "initiative", "Faster payments", nil))

	// An epic in a team may have the programme's initiative as its parent.
	epic := ada.createIssue("APP", "epic", "Card payments", obj{"parentKey": init1})
	eq(t, str(epic, "parent", "key"), init1, "epic under the initiative")
	story := key(ada.createIssue("APP", "story", "Card form", obj{"parentKey": key(epic), "storyPoints": 3}))
	apiEpic := key(ada.createIssue("API", "epic", "Payments API", obj{"parentKey": init1}))

	// Only epics, only in teams, only under initiatives.
	for _, c := range []struct{ project, typ, parent, want string }{
		{"APP", "story", init1, "same project"},
		{"OUT", "epic", init1, "isn't one of the teams"},
		{"APP", "epic", story, "cannot be the parent"},
	} {
		r := ada.do("POST", "/api/v1/issues", obj{"projectKey": c.project, "typeKey": c.typ, "summary": "x", "parentKey": c.parent})
		if r.Status != 400 || !strings.Contains(string(r.Body), c.want) {
			t.Errorf("%s %s under %s: %d %s, want 400 mentioning %q", c.project, c.typ, c.parent, r.Status, r.Body, c.want)
		}
	}

	// Moving an epic between initiatives records the programme's keys in its history.
	ada.must("PATCH", "/api/v1/issues/"+apiEpic, obj{"parentKey": init2}, 200, nil)
	var hist []obj
	ada.get("/api/v1/issues/"+apiEpic+"/history", &hist)
	found := false
	for _, h := range hist {
		if h["field"] == "parent" && h["from"] == init1 && h["to"] == init2 {
			found = true
		}
	}
	if !found {
		t.Errorf("history has no parent change %s → %s: %v", init1, init2, hist)
	}

	// The initiative lists its epics and counts the work under them.
	var detail obj
	ada.get("/api/v1/issues/"+init1, &detail)
	sameStrings(t, keys(anyObjs(detail["children"])), []string{key(epic)}, "initiative children")
	eq(t, num(detail, "progress", "total"), 1.0, "one story under the initiative")
	eq(t, num(detail, "progress", "points"), 3.0, "its points")

	// Carol can browse APP but not the private programme: the epic's parent is hidden from her,
	// she can't move the epic out from under it, and she can't use the initiative as a parent.
	ada.must("PATCH", "/api/v1/projects/PROG", obj{"access": "private"}, 200, nil)
	carol := e.login("carol@example.com", "Carol")
	carol.get("/api/v1/issues/"+key(epic), &detail)
	if detail["parent"] != nil || detail["parentHidden"] != true {
		t.Errorf("carol sees parent %v, parentHidden %v", detail["parent"], detail["parentHidden"])
	}
	var res struct {
		Issues []obj `json:"issues"`
	}
	carol.get("/api/v1/search?jql="+url.QueryEscape("key = "+key(epic)), &res)
	if len(res.Issues) != 1 || res.Issues[0]["parent"] != nil {
		t.Errorf("search shows carol the hidden parent: %v", res.Issues)
	}
	carol.must("PATCH", "/api/v1/issues/"+key(epic), obj{"parentKey": nil}, 403, nil)
	carol.must("PATCH", "/api/v1/issues/"+key(epic), obj{"summary": "Card and wallet payments"}, 200, nil)
	r := carol.do("POST", "/api/v1/issues", obj{"projectKey": "APP", "typeKey": "epic", "summary": "x", "parentKey": init1})
	if r.Status != 400 || !strings.Contains(string(r.Body), "not found") {
		t.Errorf("carol using a hidden initiative: %d %s", r.Status, r.Body)
	}
	sameStrings(t, carol.search(`initiative = `+init1), []string{}, "carol can't query by a hidden initiative")
	sameStrings(t, ada.search(`initiative = `+init1+` ORDER BY key`), []string{key(epic), story}, "initiative = finds epics and their issues")

	// Taking APP out of the programme unlinks its epics, with history.
	var removed obj
	ada.must("DELETE", "/api/v1/projects/PROG/programme/teams/APP", nil, 200, &removed)
	eq(t, removed["unlinkedEpics"].(float64), 1, "one epic unlinked")
	ada.get("/api/v1/issues/"+key(epic), &detail)
	if detail["parent"] != nil {
		t.Errorf("epic still has a parent: %v", detail["parent"])
	}
	ada.get("/api/v1/issues/"+key(epic)+"/history", &hist)
	if last := hist[len(hist)-1]; last["field"] != "parent" || last["from"] != init1 {
		t.Errorf("last history entry = %v, want the parent leaving %s", last, init1)
	}

	// Deleting the programme leaves the team epics, without their initiative.
	ada.must("DELETE", "/api/v1/projects/PROG", nil, 204, nil)
	ada.get("/api/v1/issues/"+apiEpic, &detail)
	if detail["parent"] != nil {
		t.Errorf("API epic still has a parent after the programme was deleted: %v", detail["parent"])
	}
}

func TestProgrammeOverview(t *testing.T) {
	e, ada := programmeSetup(t)
	key := func(i obj) string { return str(i, "key") }
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	ada.must("POST", "/api/v1/projects/PROG/programme/teams", obj{"projectKey": "SEC"}, 201, nil)

	init1 := key(ada.createIssue("PROG", "initiative", "Self-serve checkout", obj{"dueDate": "2026-11-30"}))
	key(ada.createIssue("PROG", "initiative", "Undated", nil))
	appEpic := key(ada.createIssue("APP", "epic", "Card payments", obj{"parentKey": init1}))
	s1 := key(ada.createIssue("APP", "story", "Card form", obj{"parentKey": appEpic, "storyPoints": 5, "dueDate": "2026-12-15"}))
	ada.createIssue("APP", "story", "Receipts", obj{"parentKey": appEpic, "storyPoints": 3})
	apiEpic := key(ada.createIssue("API", "epic", "Payments API", obj{"parentKey": init1, "dueDate": "2026-11-20"}))
	secEpic := key(ada.createIssue("SEC", "epic", "Fraud model", obj{"parentKey": init1}))
	ada.createIssue("SEC", "task", "Train it", obj{"parentKey": secEpic})
	ada.createIssue("API", "epic", "Rate limits", nil) // in a team, under no initiative
	ada.createIssue("OUT", "epic", "Elsewhere", nil)   // not in a team
	// The card form waits on unfinished work outside APP: one issue bob can see, one he can't.
	outIssue := key(ada.createIssue("OUT", "task", "PCI scan", nil))
	secIssue := key(ada.createIssue("SEC", "task", "Fraud rules", nil))
	ada.must("POST", "/api/v1/issues/"+outIssue+"/links", obj{"type": "blocks", "targetKey": s1}, 201, nil)
	ada.must("POST", "/api/v1/issues/"+secIssue+"/links", obj{"type": "blocks", "targetKey": s1}, 201, nil)
	ada.transition(s1, "start-work", 200)

	bob := e.login("bob@example.com", "Bob")
	var o obj
	bob.get("/api/v1/projects/PROG/programme", &o)
	eq(t, o["hiddenTeamCount"].(float64), 1, "SEC is hidden from bob")
	inits := anyObjs(o["initiatives"])
	eq(t, len(inits), 2, "two initiatives")
	first := inits[0]
	eq(t, str(first, "issue", "key"), init1, "rank order")
	epics := anyObjs(first["epics"])
	var epicKeys []string
	for _, ep := range epics {
		epicKeys = append(epicKeys, str(ep, "issue", "key"))
	}
	sameStrings(t, epicKeys, []string{apiEpic, appEpic}, "bob sees the epics in teams he can browse, team by team")
	eq(t, num(first, "progress", "total"), 2.0, "the hidden team's work isn't counted")
	eq(t, num(first, "progress", "inProgress"), 1.0, "one story in progress")
	eq(t, num(first, "progress", "points"), 8.0, "points")
	eq(t, str(first, "forecastDate"), "2026-12-15", "forecast is the latest due date under it")

	flags := map[string]obj{}
	for _, ep := range epics {
		for _, f := range anyObjs(ep["flags"]) {
			flags[str(ep, "issue", "key")+":"+str(f, "kind")] = f
		}
	}
	if _, ok := flags[appEpic+":late"]; !ok {
		t.Errorf("APP epic forecast 2026-12-15 is after the initiative's due date: flags %v", flags)
	}
	blocked, ok := flags[appEpic+":blocked"]
	if !ok {
		t.Fatalf("APP epic should be blocked: flags %v", flags)
	}
	sameStrings(t, anyStrings(blocked["issueKeys"]), []string{outIssue}, "visible blocker named")
	eq(t, blocked["hiddenCount"].(float64), 1, "hidden blocker counted")
	if _, ok := flags[apiEpic+":late"]; ok {
		t.Errorf("API epic is due before the initiative: %v", flags)
	}
	initFlags := map[string]bool{}
	for _, f := range anyObjs(first["flags"]) {
		initFlags[str(f, "kind")] = true
	}
	if !initFlags["late"] || !initFlags["blocked"] {
		t.Errorf("initiative flags = %v, want late and blocked", initFlags)
	}
	if !hasFlag(inits[1], "unscheduled") {
		t.Errorf("undated initiative should be unscheduled: %v", inits[1]["flags"])
	}

	unlinked := anyObjs(o["unlinkedEpics"])
	eq(t, len(unlinked), 1, "one epic under no initiative in bob's teams")
	eq(t, str(unlinked[0], "summary"), "Rate limits", "the unlinked epic")

	// Ada sees the private team's work too.
	ada.get("/api/v1/projects/PROG/programme", &o)
	eq(t, num(anyObjs(o["initiatives"])[0], "progress", "total"), 3.0, "ada counts SEC's task")

	ada.must("GET", "/api/v1/projects/APP/programme", nil, 404, nil)
}

func TestProgrammeTimeline(t *testing.T) {
	e, ada := programmeSetup(t)
	key := func(i obj) string { return str(i, "key") }
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	ada.must("POST", "/api/v1/projects/PROG/programme/teams", obj{"projectKey": "SEC"}, 201, nil)
	init1 := key(ada.createIssue("PROG", "initiative", "Self-serve checkout", nil))
	epic := key(ada.createIssue("APP", "epic", "Card payments", obj{"parentKey": init1}))
	story := key(ada.createIssue("APP", "story", "Card form", obj{"parentKey": epic}))
	ada.createIssue("SEC", "epic", "Fraud model", obj{"parentKey": init1})
	outIssue := key(ada.createIssue("OUT", "task", "PCI scan", nil))
	secIssue := key(ada.createIssue("SEC", "task", "Fraud rules", nil))
	ada.must("POST", "/api/v1/issues/"+outIssue+"/links", obj{"type": "blocks", "targetKey": story}, 201, nil)
	ada.must("POST", "/api/v1/issues/"+story+"/links", obj{"type": "blocks", "targetKey": secIssue}, 201, nil)

	bob := e.login("bob@example.com", "Bob")
	var tl obj
	bob.get("/api/v1/timeline?programme=PROG", &tl)
	var projects []string
	for _, p := range anyObjs(tl["projects"]) {
		projects = append(projects, str(p, "key"))
	}
	sameStrings(t, projects, []string{"PROG", "API", "APP"}, "programme first, then the teams bob can browse")
	var issueKeys []string
	for _, i := range anyObjs(tl["issues"]) {
		issueKeys = append(issueKeys, str(i, "key"))
	}
	sameStrings(t, issueKeys, []string{init1, epic, story}, "initiative, epic and story")
	for _, i := range anyObjs(tl["issues"]) {
		if str(i, "key") == epic && str(i, "parent", "key") != init1 {
			t.Errorf("epic's parent = %v, want %s", i["parent"], init1)
		}
	}

	ext := anyObjs(tl["externalLinks"])
	eq(t, len(ext), 2, "two links leave the timeline")
	for _, x := range ext {
		eq(t, str(x, "issueKey"), story, "both from the story")
		switch str(x, "direction") {
		case "blockedBy":
			eq(t, str(x, "other", "key"), outIssue, "visible blocker named")
			if x["linkId"] == nil {
				t.Errorf("visible link has no id: %v", x)
			}
		case "blocks":
			if x["other"] != nil || x["linkId"] != nil {
				t.Errorf("hidden issue leaked: %v", x)
			}
			eq(t, x["open"], any(true), "still open")
		}
	}

	bob.must("GET", "/api/v1/timeline?programme=PROG&projects=APP", nil, 400, nil)
	bob.must("GET", "/api/v1/timeline?programme=APP", nil, 404, nil)
}

func TestJQLHierarchy(t *testing.T) {
	_, ada := programmeSetup(t)
	key := func(i obj) string { return str(i, "key") }
	init1 := key(ada.createIssue("PROG", "initiative", "Self-serve checkout", nil))
	epic := key(ada.createIssue("APP", "epic", "Card payments", obj{"parentKey": init1}))
	story := key(ada.createIssue("APP", "story", "Card form", obj{"parentKey": epic}))
	sub := key(ada.createIssue("APP", "subtask", "Validation", obj{"parentKey": story}))
	loneEpic := key(ada.createIssue("API", "epic", "Rate limits", nil))

	sameStrings(t, ada.search(`initiative = `+init1+` ORDER BY key`), []string{epic, story, sub}, "initiative = everything under it")
	sameStrings(t, ada.search(`issue in descendantsOf("`+epic+`") ORDER BY key`), []string{story, sub}, "descendantsOf an epic")
	sameStrings(t, ada.search(`project in (APP, API) AND initiative is EMPTY`), []string{loneEpic}, "not under an initiative")
	sameStrings(t, ada.search(`project in programmeProjects("PROG") AND level = 1 ORDER BY key`), []string{loneEpic, epic}, "epics in the programme's projects")
	// An epic under an initiative has no epic of its own; epic = only reaches down from epics.
	sameStrings(t, ada.search(`epic = `+init1), []string{}, "an initiative is not an epic")
	sameStrings(t, ada.search(`project = APP AND epic is EMPTY`), []string{epic}, "the epic itself has no epic")
	// The epic breakdown groups by epics only: the epic itself isn't grouped under its initiative.
	var b struct {
		Groups []obj `json:"groups"`
	}
	ada.get("/api/v1/metrics/breakdown?projects=APP&by=epic", &b)
	groups := map[string]float64{}
	for _, g := range b.Groups {
		groups[str(g, "label")] = g["count"].(float64)
	}
	if groups["Card payments"] != 2 || groups["No epic"] != 1 || len(groups) != 2 {
		t.Errorf("breakdown by epic = %v, want Card payments: 2 (story and sub-task), No epic: 1 (the epic)", groups)
	}
	ada.must("GET", "/api/v1/search?jql="+url.QueryEscape(`issue in descendantsOf()`), nil, 400, nil)
	ada.must("GET", "/api/v1/search?jql="+url.QueryEscape(`project in teamsOf("PROG")`), nil, 400, nil)
}

func anyObjs(v any) []obj {
	list, _ := v.([]any)
	out := make([]obj, 0, len(list))
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, x.(string))
	}
	return out
}

func hasFlag(item obj, kind string) bool {
	for _, f := range anyObjs(item["flags"]) {
		if f["kind"] == kind {
			return true
		}
	}
	return false
}

// A team's rules act in the team: "roll up to the parent" on an epic doesn't reach its initiative
// in the programme, and the programme's rules don't reach the team's epics.
func TestAutomationStaysInItsProject(t *testing.T) {
	e, ada := programmeSetup(t)
	key := func(i obj) string { return str(i, "key") }
	rule := func(project string, body obj) obj {
		var r obj
		ada.must("POST", "/api/v1/projects/"+project+"/automation-rules", body, 201, &r)
		return r
	}
	teamRule := rule("APP", obj{"name": "Dates cover the children", "trigger": obj{"type": "issue.field_changed", "field": "startDate, dueDate, parent"},
		"actions": []obj{{"type": "rollup_dates", "target": "parent"}}})
	rule("PROG", obj{"name": "Comment on the epics", "trigger": obj{"type": "issue.transitioned", "toCategory": "in_progress"},
		"actions": []obj{{"type": "comment", "target": "children", "body": "Initiative started"}}})

	init1 := key(ada.createIssue("PROG", "initiative", "Self-serve checkout", obj{"startDate": "2026-12-01", "dueDate": "2026-12-31"}))
	epic := key(ada.createIssue("APP", "epic", "Card payments", obj{"parentKey": init1}))
	story := key(ada.createIssue("APP", "story", "Card form", obj{"parentKey": epic}))
	e.DrainJobs()
	ada.must("PATCH", "/api/v1/issues/"+story, obj{"startDate": "2026-11-02", "dueDate": "2026-11-13"}, 200, nil)
	e.DrainJobs()
	ada.must("PATCH", "/api/v1/issues/"+epic, obj{"dueDate": "2027-01-15"}, 200, nil)
	e.DrainJobs()

	got := ada.issue(epic)
	eq(t, str(got, "startDate"), "2026-11-02", "the epic's dates roll up from its story")
	got = ada.issue(init1)
	eq(t, str(got, "startDate")+"/"+str(got, "dueDate"), "2026-12-01/2026-12-31", "the initiative keeps its own dates")
	found := false
	for _, r := range runs(ada, str(teamRule, "id")) {
		if strings.Contains(str(r, "message"), "skipped 1 parent in another project") {
			found = true
		}
	}
	if !found {
		t.Errorf("no run says the initiative was skipped: %v", runs(ada, str(teamRule, "id")))
	}

	ada.transition(init1, "start", 200)
	e.DrainJobs()
	var comments []obj
	ada.get("/api/v1/issues/"+epic+"/comments", &comments)
	eq(t, len(comments), 0, "the programme's rule doesn't comment on team epics")
}
