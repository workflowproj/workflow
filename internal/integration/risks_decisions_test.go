package integration

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// issueTypes maps a project's issue types by key.
func (c *Client) issueTypes(project string) map[string]obj {
	c.e.t.Helper()
	var list []obj
	c.get("/api/v1/projects/"+project+"/issue-types", &list)
	out := map[string]obj{}
	for _, t := range list {
		out[str(t, "key")] = t
	}
	return out
}

func (c *Client) project(key string) obj {
	c.e.t.Helper()
	var p obj
	c.get("/api/v1/projects/"+key, &p)
	return p
}

// transitionWith runs a transition with screen fields, expecting a status.
func (c *Client) transitionWith(key, transitionID string, fields obj, want int) Response {
	c.e.t.Helper()
	return c.must("POST", "/api/v1/issues/"+key+"/transitions", obj{"transitionId": transitionID, "fields": fields}, want, nil)
}

func (c *Client) link(from, typ, to string, want int) {
	c.e.t.Helper()
	c.must("POST", "/api/v1/issues/"+from+"/links", obj{"type": typ, "targetKey": to}, want, nil)
}

// boardIssues lists the keys on a project's first board (every column).
func (c *Client) boardIssues(project string) []string {
	c.e.t.Helper()
	var boards []obj
	c.get("/api/v1/projects/"+project+"/boards", &boards)
	var data obj
	c.get("/api/v1/boards/"+str(boards[0], "id")+"/data", &data)
	var out []string
	for _, col := range data["columns"].([]any) {
		for _, i := range col.(obj)["issues"].([]any) {
			out = append(out, str(i.(obj), "key"))
		}
	}
	slices.Sort(out)
	return out
}

func TestRisksDecisionsSection(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")

	// Scrum, Kanban and Bug tracking start with the section on; service desks without it.
	var templates []obj
	admin.get("/api/v1/project-templates", &templates)
	on := map[string]any{}
	for _, tpl := range templates {
		on[str(tpl, "id")] = tpl["risksDecisions"]
	}
	eq(t, fmt.Sprint(on), "map[bug_tracking:true kanban:true scrum:true service:false]", "template defaults")

	for _, tpl := range []string{"scrum", "kanban", "bug_tracking"} {
		key := strings.ToUpper(strings.ReplaceAll(tpl, "_", ""))[:5]
		p := admin.createProject(key, tpl)
		eq(t, p["risksDecisions"], true, tpl+" section on")
		types := admin.issueTypes(key)
		eq(t, str(types["risk"], "kind"), "risk", tpl+" risk type")
		eq(t, str(types["decision"], "kind"), "decision", tpl+" decision type")
		eq(t, str(types["risk"], "workflowName"), "Risk", tpl+" risk workflow")
		eq(t, str(types["task"], "kind"), "work", tpl+" work types")
	}
	var noSection obj
	admin.must("POST", "/api/v1/projects", obj{"key": "PLAIN", "name": "Plain", "template": "scrum", "risksDecisions": false}, 201, &noSection)
	eq(t, noSection["risksDecisions"], false, "turned off at creation")
	if _, ok := admin.issueTypes("PLAIN")["risk"]; ok {
		t.Fatal("no risk type without the section")
	}

	// The reserved fields apply to the projects with the section.
	var fields []obj
	admin.get("/api/v1/fields", &fields)
	byKey := map[string]obj{}
	for _, f := range fields {
		byKey[str(f, "key")] = f
	}
	for _, k := range []string{"risk_likelihood", "risk_impact", "risk_score", "risk_rating", "risk_response", "risk_mitigation",
		"decision_context", "decision_options", "decision_outcome", "decision_consequences", "decision_date"} {
		f := byKey[k]
		if f == nil || f["reserved"] != true {
			t.Fatalf("field %s missing or not reserved: %v", k, f)
		}
	}
	var projects []string
	for _, k := range byKey["risk_score"]["projectKeys"].([]any) {
		projects = append(projects, k.(string))
	}
	slices.Sort(projects)
	sameStrings(t, projects, []string{"BUGTR", "KANBA", "SCRUM"}, "risk fields per project")
	eq(t, fmt.Sprint(byKey["risk_rating"]["options"]), "[Low Medium High Critical]", "rating options")
	eq(t, fmt.Sprint(byKey["decision_date"]["issueTypeKeys"]), "[decision]", "decision fields per type")
	admin.must("POST", "/api/v1/fields", obj{"key": "risk_score", "name": "Mine", "type": "number"}, 409, nil)
	admin.must("PUT", "/api/v1/fields/"+str(byKey["risk_score"], "id"), obj{"key": "risk_score", "name": "Renamed", "type": "number"}, 409, nil)
	admin.must("DELETE", "/api/v1/fields/"+str(byKey["risk_impact"], "id"), nil, 409, nil)

	// A service desk turns it on (twice: nothing is installed again), then off, keeping the issues.
	admin.createProject("HELP", "service")
	eq(t, admin.project("HELP")["risksDecisions"], false, "service desk off")
	var p obj
	admin.must("PATCH", "/api/v1/projects/HELP", obj{"risksDecisions": true}, 200, &p)
	eq(t, p["risksDecisions"], true, "turned on")
	before := len(admin.issueTypes("HELP"))
	admin.must("PATCH", "/api/v1/projects/HELP", obj{"risksDecisions": true}, 200, nil)
	eq(t, len(admin.issueTypes("HELP")), before, "turning on again installs nothing twice")
	var wfs []obj
	admin.get("/api/v1/workflows?project=HELP", &wfs)
	risky := 0
	for _, w := range wfs {
		if str(w, "name") == "Risk" || str(w, "name") == "Decision" {
			risky++
		}
	}
	eq(t, risky, 2, "one Risk and one Decision workflow")
	risk := admin.createIssue("HELP", "risk", "Printer supplier goes bust", obj{"fields": obj{"risk_likelihood": 2, "risk_impact": 3}})
	admin.must("PATCH", "/api/v1/projects/HELP", obj{"risksDecisions": false}, 200, &p)
	eq(t, p["risksDecisions"], false, "turned off")
	eq(t, str(admin.issue(str(risk, "key")), "type", "kind"), "risk", "issues stay when off")
	eq(t, str(admin.issueTypes("HELP")["risk"], "kind"), "risk", "types stay when off")
	admin.must("PATCH", "/api/v1/projects/HELP", obj{"risksDecisions": true}, 200, nil)
	eq(t, len(admin.issueTypes("HELP")), before, "turning on after off reuses the types")
	var audit []obj
	admin.get("/api/v1/admin/audit?action=project.risks_decisions", &audit)
	eq(t, len(audit), 3, "the switch is audited when it changes")

	// Two admins turning it on at once install it once.
	admin.createProject("TWIN", "service")
	statuses := make(chan int, 2)
	for range 2 {
		go func() { statuses <- admin.do("PATCH", "/api/v1/projects/TWIN", obj{"risksDecisions": true}).Status }()
	}
	eq(t, fmt.Sprint(<-statuses, <-statuses), "200 200", "concurrent switches")
	kinds := 0
	for _, it := range admin.issueTypes("TWIN") {
		if str(it, "kind") != "work" {
			kinds++
		}
	}
	eq(t, kinds, 2, "one Risk and one Decision type")

	// A work type already keyed "risk" blocks the section until it goes.
	wf := str(admin.issueTypes("PLAIN")["task"], "workflowId")
	var own obj
	admin.must("POST", "/api/v1/projects/PLAIN/issue-types", obj{"key": "risk", "name": "Risk item", "level": 0, "workflowId": wf}, 201, &own)
	eq(t, str(own, "kind"), "work", "kind defaults to work")
	r := admin.do("PATCH", "/api/v1/projects/PLAIN", obj{"risksDecisions": true})
	eq(t, r.Status, 409, "key taken")
	eq(t, str(r.JSON(), "code"), "type_conflict", "conflict code")
	admin.must("DELETE", "/api/v1/projects/PLAIN/issue-types/"+str(own, "id"), nil, 204, nil)
	admin.must("PATCH", "/api/v1/projects/PLAIN", obj{"risksDecisions": true}, 200, nil)

	// Further risk types get the risk fields; kinds are standard types.
	riskWf := str(admin.issueTypes("PLAIN")["risk"], "workflowId")
	admin.must("POST", "/api/v1/projects/PLAIN/issue-types", obj{"key": "threat", "name": "Threat", "level": 1, "kind": "risk", "workflowId": riskWf}, 400, nil)
	admin.must("POST", "/api/v1/projects/PLAIN/issue-types", obj{"key": "threat", "name": "Threat", "level": 0, "kind": "danger", "workflowId": riskWf}, 400, nil)
	admin.must("POST", "/api/v1/projects/PLAIN/issue-types", obj{"key": "threat", "name": "Threat", "level": 0, "kind": "risk", "workflowId": riskWf}, 201, nil)
	threat := admin.createIssue("PLAIN", "threat", "Credential stuffing", obj{"fields": obj{"risk_likelihood": 4, "risk_impact": 4}})
	eq(t, fmt.Sprint(threat["fields"].(obj)["risk_score"]), "16", "another risk type is scored")

	// Risk and decision statuses are not board columns, so they are not reported unmapped.
	var boards []obj
	admin.get("/api/v1/projects/KANBA/boards", &boards)
	var data obj
	admin.get("/api/v1/boards/"+str(boards[0], "id")+"/data", &data)
	eq(t, len(data["unmappedStatusIds"].([]any)), 0, "no unmapped risk statuses")
	var made obj
	admin.must("POST", "/api/v1/projects/SCRUM/boards", obj{"name": "Derived", "type": "kanban"}, 201, &made)
	for _, col := range made["columns"].([]any) {
		if n := str(col.(obj), "name"); slices.Contains([]string{"Mitigating", "Proposed", "Accepted", "Superseded"}, n) {
			t.Fatalf("derived board has a %s column", n)
		}
	}
}

func TestRiskScoringRegisterAndLinks(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	c.createProject("RSK", "scrum")
	today := time.Now().UTC()
	day := func(d int) string { return today.AddDate(0, 0, d).Format("2006-01-02") }

	// Likelihood × impact sets the score and rating.
	vendor := c.createIssue("RSK", "risk", "Payment provider outage", obj{
		"assigneeId": mel.id(), "dueDate": day(-2),
		"fields": obj{"risk_likelihood": 4, "risk_impact": 5, "risk_response": "Reduce", "risk_mitigation": "Second provider on standby"},
	})
	f := vendor["fields"].(obj)
	eq(t, fmt.Sprint(f["risk_score"], " ", f["risk_rating"]), "20 Critical", "score and rating")
	vk := str(vendor, "key")
	got := c.must("PATCH", "/api/v1/issues/"+vk, obj{"fields": obj{"risk_impact": 2}}, 200, nil).JSON()
	f = got["fields"].(obj)
	eq(t, fmt.Sprint(f["risk_score"], " ", f["risk_rating"]), "8 Medium", "rescored")
	var hist []obj
	c.get("/api/v1/issues/"+vk+"/history", &hist)
	var fieldsChanged []string
	for _, h := range hist {
		fieldsChanged = append(fieldsChanged, str(h, "field"))
	}
	for _, want := range []string{"Risk impact", "Risk score", "Risk rating"} {
		if !slices.Contains(fieldsChanged, want) {
			t.Fatalf("history %v lacks %s", fieldsChanged, want)
		}
	}
	for _, bad := range []obj{{"risk_likelihood": 6}, {"risk_likelihood": 2.5}, {"risk_impact": 0}, {"risk_score": 3}, {"risk_rating": "Low"}, {"risk_response": "Ignore"}} {
		c.must("PATCH", "/api/v1/issues/"+vk, obj{"fields": bad}, 400, nil)
	}
	c.must("PATCH", "/api/v1/issues/"+vk, obj{"fields": obj{"risk_impact": 5}}, 200, nil)

	breach := str(c.createIssue("RSK", "risk", "Data breach", obj{"dueDate": day(3), "fields": obj{"risk_likelihood": 2, "risk_impact": 5}}), "key")
	small := str(c.createIssue("RSK", "risk", "Office move delays", obj{"fields": obj{"risk_likelihood": 1, "risk_impact": 2}}), "key")
	vague := str(c.createIssue("RSK", "risk", "Key person leaves", obj{"fields": obj{"risk_likelihood": 3}}), "key")
	eq(t, len(c.issue(vague)["fields"].(obj)), 1, "no score without an impact")
	cleared := c.must("PATCH", "/api/v1/issues/"+small, obj{"fields": obj{"risk_impact": nil}}, 200, nil).JSON()
	if _, ok := cleared["fields"].(obj)["risk_score"]; ok {
		t.Fatal("clearing the impact clears the score")
	}
	c.must("PATCH", "/api/v1/issues/"+small, obj{"fields": obj{"risk_impact": 2}}, 200, nil)

	// Search by score, rating and kind.
	sameStrings(t, c.search(`type = risk AND "Risk score" >= 10 ORDER BY key`), []string{vk, breach}, "serious risks")
	sameStrings(t, c.search(`"Risk rating" in (High, Critical) ORDER BY key`), []string{vk, breach}, "high or critical")
	sameStrings(t, c.search(`kind = risk AND "Risk likelihood" = 3`), []string{vague}, "by likelihood")
	sameStrings(t, c.search(`project = RSK AND kind != work ORDER BY key`), []string{vk, breach, small, vague}, "not work")
	c.must("GET", "/api/v1/search?jql="+url.QueryEscape(`kind = chore`), nil, 400, nil)

	// The workflow: mitigating, then closing with a risk resolution.
	var avail []obj
	c.get("/api/v1/issues/"+small+"/transitions", &avail)
	var closeOpts string
	for _, a := range avail {
		if str(a, "id") == "close" {
			closeOpts = fmt.Sprint(a["screen"].([]any)[0].(obj)["options"])
		}
	}
	eq(t, closeOpts, "[Mitigated Accepted Occurred No longer relevant]", "close offers the risk resolutions")
	c.transition(small, "start-mitigating", 200)
	r := c.do("POST", "/api/v1/issues/"+small+"/transitions", obj{"transitionId": "close", "fields": obj{"resolution": "Done"}})
	eq(t, r.Status, 422, "Done is not a risk resolution")
	c.transitionWith(small, "close", obj{"resolution": "Mitigated"}, 200)
	closed := c.issue(small)
	eq(t, str(closed, "status", "name")+" "+str(closed, "resolution"), "Closed Mitigated", "closed as mitigated")
	sameStrings(t, c.search(`resolution = Mitigated`), []string{small}, "search by resolution")

	// Mitigating work: three tasks, two done.
	var tasks []string
	for i := 1; i <= 3; i++ {
		tk := str(c.createIssue("RSK", "task", fmt.Sprintf("Mitigation %d", i), nil), "key")
		tasks = append(tasks, tk)
	}
	c.link(tasks[0], "mitigates", vk, 201)
	c.link(tasks[1], "mitigates", vk, 201)
	c.must("POST", "/api/v1/issues/"+vk+"/links", obj{"type": "mitigates", "direction": "inward", "targetKey": tasks[2]}, 201, nil)
	for _, tk := range tasks[:2] {
		c.transition(tk, "start-work", 200)
		c.transition(tk, "done", 200)
	}
	c.link(vk, "mitigates", tasks[0], 400)        // a risk mitigates nothing
	c.link(tasks[0], "mitigates", tasks[1], 400)  // work mitigates risks only
	c.link(tasks[0], "implements", vk, 400)       // work implements decisions only
	c.link(tasks[0], "supersedes", tasks[1], 400) // decisions only
	links := c.issue(vk)["links"].([]any)
	labels := map[string]int{}
	for _, l := range links {
		labels[str(l.(obj), "label")]++
	}
	eq(t, labels["is mitigated by"], 3, "inward label on the risk")
	eq(t, str(c.issue(tasks[0])["links"].([]any)[0].(obj), "label"), "mitigates", "outward label on the work")
	sameStrings(t, c.search(`issue in linkedIssues("`+vk+`", "is mitigated by") ORDER BY key`), tasks, "mitigating work")
	sameStrings(t, c.search(`issueLinkType = "is mitigated by"`), []string{vk}, "mitigated risks")

	// The register: totals, the matrix of open risks and mitigation progress.
	var reg obj
	c.get("/api/v1/projects/RSK/risks", &reg)
	sum := reg["summary"].(obj)
	eq(t, fmt.Sprint(sum["open"], sum["highOrCritical"], sum["reviewsOverdue"], sum["unassessed"]), "3 2 1 1", "open, high or critical, overdue, unassessed")
	cells := map[string]float64{}
	for _, cl := range reg["matrix"].([]any) {
		cm := cl.(obj)
		cells[fmt.Sprint(cm["likelihood"], "x", cm["impact"])] = cm["count"].(float64)
	}
	eq(t, len(cells), 25, "every cell")
	eq(t, fmt.Sprint(cells["4x5"], cells["2x5"], cells["1x2"]), "1 1 0", "open risks by cell (closed ones left out)")
	first := reg["matrix"].([]any)[0].(obj)
	eq(t, fmt.Sprint(first["likelihood"], first["impact"], first["score"], " ", first["rating"]), "5 1 5 medium", "matrix order")
	var entry obj
	for _, x := range reg["risks"].([]any) {
		if str(x.(obj), "issue", "key") == vk {
			entry = x.(obj)
		}
	}
	m := entry["mitigation"].(obj)
	eq(t, fmt.Sprint(m["done"], " of ", m["total"]), "2 of 3", "mitigating work done")
	eq(t, fmt.Sprint(entry["score"], " ", entry["rating"], " ", entry["response"]), "20 critical Reduce", "typed assessment")
	eq(t, str(entry, "issue", "assignee", "email"), "mel@example.com", "risk owner")
	eq(t, len(reg["risks"].([]any)), 4, "every risk")
	eq(t, reg["truncated"], false, "not truncated")
	mel.get("/api/v1/projects/RSK/risks", &reg) // anyone who can browse the project
	ghost := e.login("ghost@example.com", "Ghost")
	c.must("PATCH", "/api/v1/admin/users/"+ghost.id(), obj{"orgRole": "guest"}, 200, nil)
	ghost.must("GET", "/api/v1/projects/RSK/risks", nil, 404, nil)

	// Risk owners hear about their risks like any assignee.
	e.DrainJobs()
	var notes struct {
		Items []obj `json:"items"`
	}
	mel.get("/api/v1/notifications", &notes)
	found := false
	for _, n := range notes.Items {
		found = found || str(n, "issueKey") == vk
	}
	if !found {
		t.Fatalf("no notification about %s for its owner: %v", vk, notes.Items)
	}
}

func TestRisksAndDecisionsStayOutOfPlanning(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("PLN", "scrum")
	var sprints []obj
	c.get("/api/v1/projects/PLN/sprints", &sprints)
	sprint := str(sprints[0], "id")
	epic := str(c.createIssue("PLN", "epic", "Checkout", nil), "key")
	story := str(c.createIssue("PLN", "story", "Pay by card", obj{"sprintId": sprint, "storyPoints": 3, "parentKey": epic}), "key")
	backlog := str(c.createIssue("PLN", "task", "Write the runbook", nil), "key")
	risk := str(c.createIssue("PLN", "risk", "Card scheme fees rise", obj{"fields": obj{"risk_likelihood": 3, "risk_impact": 3}}), "key")
	decision := str(c.createIssue("PLN", "decision", "Use one payment provider", nil), "key")

	// Never in a sprint, never in the hierarchy.
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLN", "typeKey": "risk", "summary": "x", "sprintId": sprint}, 400, nil)
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLN", "typeKey": "decision", "summary": "x", "sprintId": sprint}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+risk, obj{"sprintId": sprint}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+decision, obj{"sprintId": sprint}, 400, nil)
	c.must("POST", "/api/v1/issues/"+risk+"/move", obj{"sprintId": sprint}, 400, nil)
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLN", "typeKey": "risk", "summary": "x", "parentKey": epic}, 400, nil)
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLN", "typeKey": "subtask", "summary": "x", "parentKey": risk}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+decision, obj{"parentKey": epic}, 400, nil)

	// Nor in a release: release progress, readiness and notes count work only.
	var rel obj
	c.must("POST", "/api/v1/projects/PLN/releases", obj{"name": "1.0"}, 201, &rel)
	c.must("POST", "/api/v1/issues", obj{"projectKey": "PLN", "typeKey": "risk", "summary": "x", "releaseIds": []string{str(rel, "id")}}, 400, nil)
	c.must("PATCH", "/api/v1/issues/"+decision, obj{"releaseIds": []string{str(rel, "id")}}, 400, nil)

	// A story that turns out to be a risk leaves its sprint, its epic and its release.
	turned := str(c.createIssue("PLN", "story", "Fraud rules may block real customers", obj{"sprintId": sprint, "parentKey": epic, "releaseIds": []string{str(rel, "id")}}), "key")
	got := c.must("PATCH", "/api/v1/issues/"+turned, obj{"typeKey": "risk"}, 200, nil).JSON()
	if got["sprintId"] != nil || got["parent"] != nil {
		t.Fatalf("a risk keeps no sprint or parent: %v %v", got["sprintId"], got["parent"])
	}
	if rels, ok := got["releases"].([]any); !ok || len(rels) != 0 {
		t.Fatalf("a risk keeps no release: %v", got["releases"])
	}
	c.must("PATCH", "/api/v1/issues/"+story, obj{"typeKey": "risk"}, 200, nil)
	c.must("PATCH", "/api/v1/issues/"+story, obj{"typeKey": "story", "sprintId": sprint, "parentKey": epic}, 200, nil)
	c.createIssue("PLN", "subtask", "Card form", obj{"parentKey": story})
	c.must("PATCH", "/api/v1/issues/"+story, obj{"typeKey": "decision"}, 400, nil) // it has a sub-task

	// The backlog and the boards show work only.
	var boards []obj
	c.get("/api/v1/projects/PLN/boards", &boards)
	board := str(boards[0], "id")
	var bl obj
	c.get("/api/v1/boards/"+board+"/backlog", &bl)
	var inBacklog []string
	for _, i := range bl["backlog"].([]any) {
		inBacklog = append(inBacklog, str(i.(obj), "key"))
	}
	sameStrings(t, inBacklog, []string{backlog}, "backlog")
	c.get("/api/v1/boards/"+board+"/backlog?jql="+url.QueryEscape("kind != work"), &bl)
	eq(t, len(bl["backlog"].([]any)), 0, "not even when asked for")
	start := time.Now().UTC().AddDate(0, 0, -3)
	c.must("POST", "/api/v1/sprints/"+sprint+"/start", obj{"startDate": start.Format("2006-01-02"), "endDate": start.AddDate(0, 0, 14).Format("2006-01-02")}, 200, nil)
	onBoard := c.boardIssues("PLN")
	if slices.Contains(onBoard, risk) || slices.Contains(onBoard, decision) || slices.Contains(onBoard, turned) {
		t.Fatalf("scrum board shows %v", onBoard)
	}
	c.createProject("KAN", "kanban")
	kanbanRisk := str(c.createIssue("KAN", "risk", "Supplier lead times", nil), "key")
	kanbanTask := str(c.createIssue("KAN", "task", "Order stock", nil), "key")
	sameStrings(t, c.boardIssues("KAN"), []string{kanbanTask}, "kanban board")
	c.createProject("BUG", "bug_tracking")
	c.createIssue("BUG", "risk", "Crash reports go unread", nil)
	bug := str(c.createIssue("BUG", "bug", "Crash", nil), "key")
	sameStrings(t, c.boardIssues("BUG"), []string{bug}, "triage board (its Open column shares the risk status)")

	// The sprint's commitment, report and velocity count work only.
	var rep obj
	c.get("/api/v1/sprints/"+sprint+"/report", &rep)
	// The story and its sub-task.
	eq(t, fmt.Sprint(rep["committedIssues"], " ", rep["committedPoints"]), "2 3", "sprint commitment")
	c.transition(story, "start-work", 200)
	c.must("POST", "/api/v1/sprints/"+sprint+"/complete", obj{}, 200, nil)
	var vel obj
	c.get("/api/v1/projects/PLN/velocity", &vel)
	v := vel["sprints"].([]any)[0].(obj)
	eq(t, fmt.Sprint(v["committedIssues"]), "2", "velocity")

	// The timeline draws work only.
	var tl obj
	c.get("/api/v1/timeline?projects=PLN,KAN", &tl)
	for _, i := range tl["issues"].([]any) {
		if k := str(i.(obj), "type", "kind"); k != "work" {
			t.Fatalf("timeline shows a %s: %v", k, str(i.(obj), "key"))
		}
	}

	// Delivery metrics count work only: a resolved risk and a decision change nothing.
	c.transitionWith(risk, "close", obj{"resolution": "Accepted"}, 200)
	c.transition(decision, "accept", 200)
	var sum obj
	c.get("/api/v1/metrics/summary?projects=PLN&days=7", &sum)
	// Open work: the epic, the story, the sub-task and the backlog task; resolved: none.
	eq(t, fmt.Sprint(sum["open"], " ", sum["created"], " ", sum["resolved"]), "4 4 0", "summary")
	var bd obj
	c.get("/api/v1/metrics/breakdown?projects=PLN&by=type", &bd)
	for _, g := range bd["groups"].([]any) {
		if k := str(g.(obj), "key"); k == "risk" || k == "decision" {
			t.Fatalf("breakdown counts %s", k)
		}
	}
	eq(t, fmt.Sprint(bd["total"]), "4", "breakdown total")
	c.get("/api/v1/metrics/breakdown?projects=PLN&by=type&jql="+url.QueryEscape("kind = risk"), &bd)
	eq(t, fmt.Sprint(bd["total"]), "0", "metrics never count risks")
	var flow obj
	c.get("/api/v1/metrics/flow?projects=PLN&days=7", &flow)
	last := 0.0
	for _, s := range flow["series"].([]any) {
		counts := s.(obj)["counts"].([]any)
		last += counts[len(counts)-1].(float64)
	}
	eq(t, last, 4.0, "cumulative flow")
	var cr obj
	c.get("/api/v1/metrics/created-resolved?projects=PLN&days=7", &cr)
	created, resolved := 0.0, 0.0
	for _, b := range cr["buckets"].([]any) {
		created += b.(obj)["created"].(float64)
		resolved += b.(obj)["resolved"].(float64)
	}
	eq(t, fmt.Sprint(created, " ", resolved), "4 0", "created vs resolved")
	var ct obj
	c.get("/api/v1/projects/PLN/reports/cycle-time?days=30", &ct)
	eq(t, len(ct["issues"].([]any)), 0, "cycle time")

	// They stay in search and the project's issue list.
	sameStrings(t, c.search(`project = PLN AND type in (risk, decision) ORDER BY key`), []string{risk, decision, turned}, "search")
	sameStrings(t, c.search(`project = KAN ORDER BY key`), []string{kanbanRisk, kanbanTask}, "issue list")

	// Service desk queues leave a desk's risks out.
	c.createProject("DESK", "service")
	c.must("PATCH", "/api/v1/projects/DESK", obj{"risksDecisions": true}, 200, nil)
	deskRisk := str(c.createIssue("DESK", "risk", "Ticket volume doubles", nil), "key")
	request := str(c.createIssue("DESK", "service_request", "New laptop", nil), "key")
	var queues []obj
	c.get("/api/v1/projects/DESK/queues", &queues)
	for _, q := range queues {
		if str(q, "name") != "All open" {
			continue
		}
		eq(t, fmt.Sprint(q["count"]), "1", "queue count")
		var qi obj
		c.get("/api/v1/queues/"+str(q, "id")+"/issues", &qi)
		eq(t, str(qi["items"].([]any)[0].(obj), "issue", "key"), request, "queue items")
	}
	var slas int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM issue_slas s JOIN issues i ON i.id = s.issue_id JOIN projects p ON p.id = i.project_id
		WHERE p.key || '-' || i.number = $1`, deskRisk).Scan(&slas); err != nil {
		t.Fatal(err)
	}
	eq(t, slas, 0, "no SLA clocks on a risk")
}

func TestDecisionLogAndSupersede(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("ADR", "kanban")
	today := time.Now().UTC().Format("2006-01-02")

	old := str(c.createIssue("ADR", "decision", "Store sessions in PostgreSQL", obj{"fields": obj{
		"decision_context": "Sessions live in memory and are lost on restart.", "decision_options": "Redis; PostgreSQL",
		"decision_outcome": "PostgreSQL: one fewer service to run.", "decision_consequences": "More database load.",
	}}), "key")
	rejected := str(c.createIssue("ADR", "decision", "Write our own queue", nil), "key")
	c.transition(rejected, "reject", 200)
	dated := str(c.createIssue("ADR", "decision", "Adopt OpenTelemetry", obj{"fields": obj{"decision_date": "2026-01-15"}}), "key")
	c.transition(dated, "accept", 200)

	// Accepting dates the decision and records why it is resolved.
	c.transition(old, "accept", 200)
	got := c.issue(old)
	eq(t, str(got, "status", "name")+" "+str(got, "resolution"), "Accepted Accepted", "accepted")
	eq(t, str(got["fields"].(obj), "decision_date"), today, "decided on today")
	eq(t, str(c.issue(dated)["fields"].(obj), "decision_date"), "2026-01-15", "a date set beforehand stays")
	eq(t, str(c.issue(rejected), "resolution"), "Rejected", "rejected")

	// Superseding needs a newer decision; the Supersede transition waits for the link.
	var avail []obj
	c.get("/api/v1/issues/"+old+"/transitions", &avail)
	for _, a := range avail {
		if str(a, "id") == "supersede" {
			t.Fatal("Supersede is offered before anything supersedes the decision")
		}
	}
	newer := str(c.createIssue("ADR", "decision", "Store sessions in Redis", nil), "key")
	task := str(c.createIssue("ADR", "task", "Add Redis to the chart", nil), "key")
	c.must("POST", "/api/v1/issues/"+old+"/supersede", obj{"supersededByKey": task}, 400, nil)
	c.must("POST", "/api/v1/issues/"+old+"/supersede", obj{"supersededByKey": old}, 400, nil)
	r := c.do("POST", "/api/v1/issues/"+newer+"/supersede", obj{"supersededByKey": old})
	eq(t, r.Status, 422, "a proposed decision is not superseded")
	if links := c.issue(newer)["links"].([]any); len(links) != 0 {
		t.Fatal("a refused supersede leaves no link")
	}
	sup := c.must("POST", "/api/v1/issues/"+old+"/supersede", obj{"supersededByKey": newer, "comment": "Redis is already in the stack."}, 200, nil).JSON()
	eq(t, str(sup, "status", "name")+" "+str(sup, "resolution"), "Superseded Superseded", "superseded")
	eq(t, str(sup["links"].([]any)[0].(obj), "label"), "is superseded by", "link on the old decision")
	eq(t, str(c.issue(newer)["links"].([]any)[0].(obj), "label"), "supersedes", "link on the new decision")
	var comments []obj
	c.get("/api/v1/issues/"+old+"/comments", &comments)
	eq(t, str(comments[0], "body"), "Redis is already in the stack.", "comment with the transition")
	sameStrings(t, c.search(`issue in linkedIssues("`+old+`", "is superseded by")`), []string{newer}, "what supersedes it")

	// Work implements decisions.
	c.link(task, "implements", newer, 201)
	c.link(newer, "implements", task, 400)
	c.transition(task, "done", 200)

	// The log: newest first, dates, the supersede chain and the implementing work.
	var log obj
	c.get("/api/v1/projects/ADR/decisions", &log)
	byKey := map[string]obj{}
	var order []string
	for _, d := range log["decisions"].([]any) {
		byKey[str(d.(obj), "issue", "key")] = d.(obj)
		order = append(order, str(d.(obj), "issue", "key"))
	}
	sameStrings(t, order, []string{newer, dated, rejected, old}, "newest first")
	eq(t, str(byKey[old], "decidedOn"), today, "decided on")
	eq(t, str(byKey[old]["supersededBy"].([]any)[0].(obj), "key"), newer, "superseded by")
	eq(t, str(byKey[newer]["supersedes"].([]any)[0].(obj), "key"), old, "supersedes")
	impl := byKey[newer]["implementation"].(obj)
	eq(t, fmt.Sprint(impl["done"], " of ", impl["total"]), "1 of 1", "implemented")
	if byKey[newer]["decidedOn"] != nil {
		t.Fatal("a proposed decision has no date")
	}

	// Linking by hand makes Supersede available in the workflow too.
	newest := str(c.createIssue("ADR", "decision", "Drop OpenTelemetry", nil), "key")
	c.link(newest, "supersedes", dated, 201)
	c.transition(dated, "supersede", 200)
}

func TestRiskReviewReminderTemplate(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	bob := e.login("bob@example.com", "Bob")
	admin.createProject("REV", "scrum")
	day := func(d int) string { return time.Now().UTC().AddDate(0, 0, d).Format("2006-01-02") }
	due := str(admin.createIssue("REV", "risk", "Licence renewal lapses", obj{"assigneeId": mel.id(), "dueDate": day(3)}), "key")
	admin.createIssue("REV", "risk", "Office lease ends", obj{"assigneeId": bob.id(), "dueDate": day(30)})
	closed := str(admin.createIssue("REV", "risk", "Old risk", obj{"assigneeId": bob.id(), "dueDate": day(1)}), "key")
	admin.transitionWith(closed, "close", obj{"resolution": "No longer relevant"}, 200)
	admin.createIssue("REV", "task", "Due work", obj{"assigneeId": bob.id(), "dueDate": day(2)})
	e.DrainJobs()
	for _, u := range []*Client{mel, bob} {
		u.must("POST", "/api/v1/notifications/read", obj{}, 204, nil)
	}

	// The rule the "Remind risk owners about reviews due this week" template creates.
	var rule obj
	admin.must("POST", "/api/v1/projects/REV/automation-rules", obj{
		"name": "Remind risk owners about reviews due this week", "enabled": true,
		"trigger": obj{"type": "schedule", "cron": "0 8 * * 1", "jql": `kind = risk AND statusCategory != Done AND due <= 7d AND assignee is not EMPTY`},
		"actions": []obj{{"type": "notify", "recipients": "assignee"}},
	}, 201, &rule)
	e.exec(`UPDATE automation_rules SET next_run_at = now() - interval '1 minute' WHERE id = $1`, str(rule, "id"))
	if err := e.App.RunJob(context.Background(), "automation_schedule", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	e.DrainJobs()
	var notes struct {
		Items  []obj `json:"items"`
		Unread int   `json:"unread"`
	}
	mel.get("/api/v1/notifications?unread=true", &notes)
	eq(t, notes.Unread, 1, "the owner of a review due this week")
	eq(t, str(notes.Items[0], "issueKey"), due, "about that risk")
	bob.get("/api/v1/notifications?unread=true", &notes)
	eq(t, notes.Unread, 0, "nobody else")
}

// Risks and decisions never reveal other projects' issues, their fields stay on their own types,
// and help desk request types can't create them.
func TestRisksAndDecisionsAcrossProjects(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	member := e.login("mel@example.com", "Mel")
	admin.createProject("OPEN", "kanban")
	admin.must("POST", "/api/v1/projects", obj{"key": "SECRET", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)

	// The decision log leaves out a superseding decision in a project the viewer can't browse.
	old := str(admin.createIssue("OPEN", "decision", "Use vendor X", nil), "key")
	admin.transition(old, "accept", 200)
	newer := str(admin.createIssue("SECRET", "decision", "Acquire vendor Y", nil), "key")
	admin.must("POST", "/api/v1/issues/"+old+"/supersede", obj{"supersededByKey": newer}, 200, nil)
	supersededBy := func(c *Client) []any {
		var log obj
		c.get("/api/v1/projects/OPEN/decisions", &log)
		for _, d := range log["decisions"].([]any) {
			if str(d.(obj), "issue", "key") == old {
				return d.(obj)["supersededBy"].([]any)
			}
		}
		t.Fatalf("%s not in the log", old)
		return nil
	}
	eq(t, len(supersededBy(admin)), 1, "the admin sees what supersedes it")
	eq(t, len(supersededBy(member)), 0, "a member doesn't see a private project's decision")

	// The register counts only mitigating work the viewer can browse.
	risk := str(admin.createIssue("OPEN", "risk", "Outage", obj{"fields": obj{"risk_likelihood": 3, "risk_impact": 3}}), "key")
	admin.link(str(admin.createIssue("SECRET", "task", "Secret mitigation", nil), "key"), "mitigates", risk, 201)
	mitigation := func(c *Client) obj {
		var reg obj
		c.get("/api/v1/projects/OPEN/risks", &reg)
		for _, r := range reg["risks"].([]any) {
			if str(r.(obj), "issue", "key") == risk {
				m, _ := r.(obj)["mitigation"].(obj)
				return m
			}
		}
		t.Fatalf("%s not in the register", risk)
		return nil
	}
	eq(t, fmt.Sprint(mitigation(admin)["total"]), "1", "the admin counts the private work")
	if m := mitigation(member); m != nil && fmt.Sprint(m["total"]) != "0" {
		t.Fatalf("a member's register counts private work: %v", m)
	}

	// A risk type keyed like another project's story doesn't give stories the risk fields.
	admin.createProject("SCR", "scrum")
	admin.createProject("BUGP", "bug_tracking")
	admin.must("POST", "/api/v1/projects/BUGP/issue-types", obj{"key": "story", "name": "Story risk", "level": 0, "kind": "risk", "workflowId": str(admin.issueTypes("BUGP")["risk"], "workflowId")}, 201, nil)
	admin.must("POST", "/api/v1/issues", obj{"projectKey": "SCR", "typeKey": "story", "summary": "A story", "fields": obj{"risk_score": 25, "risk_rating": "Critical", "risk_likelihood": 9}}, 400, nil)

	// Request types create work only.
	admin.createProject("DESK", "service")
	admin.must("PATCH", "/api/v1/projects/DESK", obj{"risksDecisions": true}, 200, nil)
	admin.must("POST", "/api/v1/projects/DESK/request-types", obj{"name": "Report a risk", "issueTypeKey": "risk"}, 400, nil)
}

// A link can be made from either end, so whoever can edit either end can remove it: a risk's
// owner unlinks mitigating work in a project where they only view.
func TestUnlinkFromEitherEnd(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	mel := e.login("mel@example.com", "Mel")
	outsider := e.login("ola@example.com", "Ola")
	admin.createProject("OPEN", "kanban")
	admin.createProject("OPS", "kanban")
	admin.must("POST", "/api/v1/projects/OPS/members", obj{"userId": mel.id(), "roleKey": "viewer"}, 201, nil)
	admin.must("POST", "/api/v1/projects/OPEN/members", obj{"userId": outsider.id(), "roleKey": "viewer"}, 201, nil)
	admin.must("POST", "/api/v1/projects/OPS/members", obj{"userId": outsider.id(), "roleKey": "viewer"}, 201, nil)
	risk := str(admin.createIssue("OPEN", "risk", "Outage", obj{"fields": obj{"risk_likelihood": 2, "risk_impact": 4}}), "key")
	work := str(admin.createIssue("OPS", "task", "Add a standby", nil), "key")
	links := func() []any {
		var reg obj
		mel.get("/api/v1/issues/"+risk, &reg)
		return reg["links"].([]any)
	}
	for _, unlinker := range []*Client{outsider, mel} {
		admin.link(work, "mitigates", risk, 201)
		id := str(links()[0].(obj), "id")
		if unlinker == outsider {
			outsider.must("DELETE", "/api/v1/issue-links/"+id, nil, 403, nil) // views both ends, edits neither
			admin.must("DELETE", "/api/v1/issue-links/"+id, nil, 204, nil)
			continue
		}
		mel.must("DELETE", "/api/v1/issue-links/"+id, nil, 204, nil) // edits the risk, only views the work
		eq(t, len(links()), 0, "unlinked from the risk's end")
	}
}
