package integration

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// backdate rewrites when an issue's history entries for a field happened (oldest first) and,
// optionally, when the issue was created and resolved, so reports can be tested over days.
func (e *Env) backdate(t *testing.T, key string, field string, at ...time.Time) {
	t.Helper()
	ctx := context.Background()
	for k, ts := range at {
		_, err := e.Pool.Exec(ctx, `UPDATE issue_history SET created_at = $3 WHERE id = (
			SELECT h.id FROM issue_history h JOIN issues i ON i.id = h.issue_id JOIN projects p ON p.id = i.project_id
			WHERE p.key || '-' || i.number = $1 AND h.field = $2 ORDER BY h.id OFFSET $4 LIMIT 1)`, key, field, ts, k)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (e *Env) setIssueTimes(t *testing.T, key string, created time.Time, resolved *time.Time) {
	t.Helper()
	_, err := e.Pool.Exec(context.Background(), `UPDATE issues SET created_at = $2, resolved_at = coalesce($3, resolved_at)
		WHERE id = (SELECT i.id FROM issues i JOIN projects p ON p.id = i.project_id WHERE p.key || '-' || i.number = $1)`, key, created, resolved)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSprintReportFollowsScope(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("MET", "scrum")
	var sprints []obj
	c.get("/api/v1/projects/MET/sprints", &sprints)
	sprint := str(sprints[0], "id")
	a := str(c.createIssue("MET", "story", "A", obj{"sprintId": sprint, "storyPoints": 3}), "key")
	b := str(c.createIssue("MET", "story", "B", obj{"sprintId": sprint, "storyPoints": 5}), "key")
	cc := str(c.createIssue("MET", "story", "C", obj{"storyPoints": 2}), "key")

	today := time.Now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -4)
	day := func(d int) time.Time { return start.AddDate(0, 0, d).Add(12 * time.Hour) }
	c.must("POST", "/api/v1/sprints/"+sprint+"/start", obj{"startDate": start.Format("2006-01-02"), "endDate": today.AddDate(0, 0, 9).Format("2006-01-02")}, 200, nil)
	for _, k := range []string{a, b, cc} {
		e.setIssueTimes(t, k, day(-1), nil)
	}

	// Day 1: C joins. Day 2: B leaves. Day 2-3: A is worked on and finished.
	c.must("PATCH", "/api/v1/issues/"+cc, obj{"sprintId": sprint}, 200, nil)
	c.must("PATCH", "/api/v1/issues/"+b, obj{"sprintId": nil}, 200, nil)
	c.transition(a, "start-work", 200)
	c.transition(a, "done", 200)
	e.backdate(t, cc, "sprint", day(1))
	e.backdate(t, b, "sprint", day(2))
	e.backdate(t, a, "status", day(2), day(3))
	resolved := day(3)
	e.setIssueTimes(t, a, day(-1), &resolved)

	var rep obj
	c.get("/api/v1/sprints/"+sprint+"/report", &rep)
	eq(t, fmt.Sprint(rep["committedPoints"], "/", rep["committedIssues"]), "8/2", "commitment")
	eq(t, fmt.Sprint(rep["addedPoints"], "/", rep["addedIssues"]), "2/1", "added scope")
	eq(t, fmt.Sprint(rep["removedPoints"], "/", rep["removedIssues"]), "5/1", "removed scope")
	eq(t, fmt.Sprint(rep["completedPoints"], "/", rep["completedIssues"]), "3/1", "completed")
	burn := rep["burndown"].([]any)
	eq(t, len(burn), 5, "a point per day so far")
	point := func(d int) string {
		p := burn[d].(obj)
		return fmt.Sprint(p["scopePoints"], " ", p["completedPoints"], " ", p["remainingPoints"], " ", p["scopeIssues"], " ", p["remainingIssues"])
	}
	eq(t, point(0), "8 0 8 2 2", "day 0: the commitment")
	eq(t, point(1), "10 0 10 3 3", "day 1: C added")
	eq(t, point(2), "5 0 5 2 2", "day 2: B removed")
	eq(t, point(3), "5 3 2 2 1", "day 3: A done")
	eq(t, fmt.Sprint(burn[0].(obj)["idealPoints"]), "8", "ideal starts at the commitment")

	// Story points changed mid-sprint move the scope line from that day.
	c.must("PATCH", "/api/v1/issues/"+cc, obj{"storyPoints": 4}, 200, nil)
	e.backdate(t, cc, "storyPoints", start.AddDate(0, 0, 4).Add(time.Second)) // today, before now
	c.get("/api/v1/sprints/"+sprint+"/report", &rep)
	burn = rep["burndown"].([]any)
	eq(t, point(3), "5 3 2 2 1", "earlier days keep the old estimate")
	eq(t, point(4), "7 3 4 2 1", "re-estimated")

	// Issue metrics from the same history.
	var m obj
	c.get("/api/v1/issues/"+a+"/metrics", &m)
	eq(t, fmt.Sprint(m["cycleTimeHours"]), "24", "cycle time: in progress to done")
	eq(t, fmt.Sprint(m["leadTimeHours"]), "96", "lead time: created to resolved")
	var names []string
	for _, st := range m["timeInStatus"].([]any) {
		names = append(names, str(st.(obj), "status", "name"))
	}
	sameStrings(t, names, []string{"To Do", "In Progress", "Done"}, "statuses in the order visited")
	eq(t, fmt.Sprint(m["timeInStatus"].([]any)[1].(obj)["hours"]), "24", "time in progress")
	eq(t, fmt.Sprint(m["sprints"]), "1", "sprints")
	if str(c.issue(a), "statusChangedAt") == "" {
		t.Fatal("issues carry statusChangedAt")
	}

	// Velocity after completing the sprint.
	var next obj
	c.must("POST", "/api/v1/projects/MET/sprints", obj{}, 201, &next)
	c.must("POST", "/api/v1/sprints/"+sprint+"/complete", obj{"moveOpenIssuesTo": next["id"]}, 200, nil)
	var vel obj
	c.get("/api/v1/projects/MET/velocity", &vel)
	eq(t, len(vel["sprints"].([]any)), 1, "completed sprints only")
	v := vel["sprints"].([]any)[0].(obj)
	eq(t, fmt.Sprint(v["committedPoints"], "/", v["completedPoints"], "/", v["completedIssues"]), "8/3/1", "velocity")
	eq(t, fmt.Sprint(vel["averagePoints"]), "3", "average")
	c.must("GET", "/api/v1/projects/MET/velocity?count=50", nil, 400, nil)
}

func TestMetrics(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("FLO", "kanban")
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	ada.createIssue("SEC", "task", "Hidden", nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(d int) time.Time { return today.AddDate(0, 0, d).Add(10 * time.Hour) }
	epic := str(ada.createIssue("FLO", "epic", "Checkout", nil), "key")
	t1 := str(ada.createIssue("FLO", "task", "One", obj{"parentKey": epic, "assigneeId": bob.id(), "storyPoints": 2, "labels": []string{"web"}}), "key")
	t2 := str(ada.createIssue("FLO", "task", "Two", obj{"parentKey": epic, "assigneeId": bob.id(), "dueDate": today.AddDate(0, 0, -1).Format("2006-01-02")}), "key")
	t3 := str(ada.createIssue("FLO", "task", "Three", obj{"labels": []string{"web", "api"}}), "key")
	inProgress, done := ada.statusID("In Progress"), ada.statusID("Done")
	ada.must("POST", "/api/v1/issues/"+t1+"/move", obj{"statusId": inProgress}, 200, nil)
	ada.must("POST", "/api/v1/issues/"+t1+"/move", obj{"statusId": done}, 200, nil)
	ada.must("POST", "/api/v1/issues/"+t2+"/move", obj{"statusId": inProgress}, 200, nil)
	// Starting work assigns the mover; hand both back to Bob.
	for _, k := range []string{t1, t2} {
		ada.must("PATCH", "/api/v1/issues/"+k, obj{"assigneeId": bob.id()}, 200, nil)
	}
	for _, k := range []string{epic, t1, t2, t3} {
		e.setIssueTimes(t, k, day(-5), nil)
	}
	e.setIssueTimes(t, t3, day(-2), nil)
	resolved := day(-1)
	e.setIssueTimes(t, t1, day(-5), &resolved)
	e.backdate(t, t1, "status", day(-3), day(-1))
	e.backdate(t, t2, "status", day(-2))

	// Summary.
	var sum obj
	bob.get("/api/v1/metrics/summary?projects=FLO&days=7", &sum)
	eq(t, fmt.Sprint(sum["open"], " ", sum["todo"], " ", sum["inProgress"]), "3 2 1", "open work by category")
	eq(t, fmt.Sprint(sum["created"], " ", sum["resolved"], " ", sum["overdue"], " ", sum["unassigned"]), "4 1 1 2", "created, resolved, overdue, unassigned")
	eq(t, fmt.Sprint(sum["resolvedPoints"], " ", sum["medianCycleHours"]), "2 48", "resolved points and cycle time")
	bob.get("/api/v1/metrics/summary", &sum)
	eq(t, fmt.Sprint(sum["created"]), "4", "every browsable project, not the private one")
	bob.must("GET", "/api/v1/metrics/summary?projects=SEC", nil, 404, nil)
	bob.must("GET", "/api/v1/metrics/summary?days=0", nil, 400, nil)
	bob.get("/api/v1/metrics/summary?projects=FLO&jql="+url.QueryEscape("assignee = currentUser()"), &sum)
	eq(t, fmt.Sprint(sum["open"]), "1", "JQL narrows the scope")

	// Breakdowns.
	type breakdown struct {
		Total  int   `json:"total"`
		Groups []obj `json:"groups"`
	}
	group := func(by string) map[string]string {
		var b breakdown
		ada.get("/api/v1/metrics/breakdown?projects=FLO&by="+by, &b)
		out := map[string]string{}
		for _, g := range b.Groups {
			out[str(g, "label")] = fmt.Sprint(g["count"])
		}
		return out
	}
	eq(t, fmt.Sprint(group("assignee")), "map[Bob:2 Unassigned:2]", "by assignee")
	eq(t, fmt.Sprint(group("statusCategory")), "map[Done:1 In progress:1 To do:2]", "by status category")
	eq(t, fmt.Sprint(group("label")), "map[No label:2 api:1 web:2]", "by label")
	eq(t, fmt.Sprint(group("epic")), "map[Checkout:2 No epic:2]", "by epic")
	var b breakdown
	ada.get("/api/v1/metrics/breakdown?projects=FLO&by=statusCategory", &b)
	eq(t, str(b.Groups[0], "key"), "todo", "categories in their natural order")
	ada.get("/api/v1/metrics/breakdown?projects=FLO&by=assignee", &b)
	eq(t, str(b.Groups[len(b.Groups)-1], "key"), "", "none last")
	eq(t, str(b.Groups[0], "user", "displayName"), "Bob", "assignee groups carry the user")
	ada.must("GET", "/api/v1/metrics/breakdown?by=colour", nil, 400, nil)

	// Cumulative flow: at the end of each day.
	var flow struct {
		Dates  []string `json:"dates"`
		Series []obj    `json:"series"`
	}
	ada.get("/api/v1/metrics/flow?projects=FLO&days=7", &flow)
	eq(t, len(flow.Dates), 7, "a date per day")
	counts := map[string][]any{}
	for _, s := range flow.Series {
		counts[str(s, "key")] = s["counts"].([]any)
	}
	at := func(d int) string { // d days before today
		k := len(flow.Dates) - 1 - d
		return fmt.Sprint(counts["todo"][k], " ", counts["in_progress"][k], " ", counts["done"][k])
	}
	eq(t, at(4), "3 0 0", "five days ago: three issues, all to do")
	eq(t, at(3), "2 1 0", "One started")
	eq(t, at(2), "2 2 0", "Three created, Two started")
	eq(t, at(0), "2 1 1", "One done")
	ada.get("/api/v1/metrics/flow?projects=FLO&days=7&by=status", &flow)
	eq(t, str(flow.Series[0], "category"), "todo", "statuses ordered by category")

	// Created vs resolved.
	var cr struct {
		Buckets []obj `json:"buckets"`
	}
	ada.get("/api/v1/metrics/created-resolved?projects=FLO&days=7", &cr)
	eq(t, len(cr.Buckets), 7, "daily buckets")
	last := func(i int) obj { return cr.Buckets[len(cr.Buckets)-1-i] }
	eq(t, fmt.Sprint(last(5)["created"], " ", last(2)["created"], " ", last(1)["resolved"], " ", last(0)["open"]), "3 1 1 3", "created, resolved, open")
	ada.get("/api/v1/metrics/created-resolved?projects=FLO&days=14&interval=week", &cr)
	if len(cr.Buckets) < 2 || len(cr.Buckets) > 3 {
		t.Fatalf("weekly buckets: %d", len(cr.Buckets))
	}
	if d, _ := time.Parse("2006-01-02", str(cr.Buckets[0], "start")); d.Weekday() != time.Monday {
		t.Fatalf("weeks start on Monday, got %s", d.Weekday())
	}

	// Cycle time across projects.
	var ct obj
	ada.get("/api/v1/metrics/cycle-time?days=30", &ct)
	eq(t, fmt.Sprint(ct["medianHours"]), "48", "cycle time")
	eq(t, str(ct["issues"].([]any)[0].(obj), "projectKey"), "FLO", "project key per issue")
}

func TestDashboardsAndCardFields(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("DSH", "scrum")

	gadgets := []obj{
		{"id": "burn", "type": "sprint_burndown", "width": 2, "config": obj{"projectKey": "DSH"}},
		{"id": "mine", "type": "stat", "width": 1, "title": "My open work", "config": obj{"stat": "open", "jql": "assignee = currentUser()"}},
	}
	var d obj
	ada.must("POST", "/api/v1/dashboards", obj{"name": "Team", "gadgets": gadgets}, 201, &d)
	id := str(d, "id")
	eq(t, len(d["gadgets"].([]any)), 2, "gadgets stored")
	eq(t, str(d["gadgets"].([]any)[1].(obj), "config", "jql"), "assignee = currentUser()", "gadget config round-trips")
	eq(t, d["canEdit"], true, "owner can edit")

	// Private until shared.
	bob.must("GET", "/api/v1/dashboards/"+id, nil, 404, nil)
	var list []obj
	bob.get("/api/v1/dashboards", &list)
	eq(t, len(list), 0, "not listed for others")
	ada.must("PUT", "/api/v1/dashboards/"+id, obj{"name": "Team", "shared": true, "gadgets": gadgets}, 200, nil)
	bob.get("/api/v1/dashboards", &list)
	eq(t, len(list), 1, "shared dashboards are listed")
	eq(t, list[0]["canEdit"], false, "others cannot edit")
	eq(t, str(list[0], "owner", "displayName"), "Ada", "owner shown")
	bob.must("PUT", "/api/v1/dashboards/"+id, obj{"name": "Mine now", "gadgets": gadgets}, 403, nil)
	bob.must("DELETE", "/api/v1/dashboards/"+id, nil, 403, nil)
	var copy obj
	bob.must("POST", "/api/v1/dashboards", obj{"name": "Copy of Team", "gadgets": list[0]["gadgets"]}, 201, &copy)
	bob.get("/api/v1/dashboards", &list)
	eq(t, str(list[0], "name"), "Copy of Team", "your own dashboards first")

	// Validation.
	bad := func(g obj, field string) {
		t.Helper()
		r := ada.must("POST", "/api/v1/dashboards", obj{"name": "Bad", "gadgets": []obj{g}}, 400, nil)
		if !strings.Contains(string(r.Body), field) {
			t.Fatalf("expected an error on %s, got %s", field, r.Body)
		}
	}
	bad(obj{"id": "x", "type": "pie", "width": 1, "config": obj{}}, "gadgets[0].type")
	bad(obj{"id": "x", "type": "stat", "width": 4, "config": obj{}}, "gadgets[0].width")
	bad(obj{"id": "x", "type": "breakdown", "width": 1, "config": obj{"jql": "status =="}}, "gadgets[0].config.jql")
	ada.must("POST", "/api/v1/dashboards", obj{"name": "Dup", "gadgets": []obj{gadgets[0], gadgets[0]}}, 400, nil)
	ada.must("DELETE", "/api/v1/dashboards/"+id, nil, 204, nil)
	bob.must("GET", "/api/v1/dashboards/"+id, nil, 404, nil)

	// Board card fields.
	var boards []obj
	ada.get("/api/v1/projects/DSH/boards", &boards)
	eq(t, fmt.Sprint(boards[0]["cardFields"]), "[epic labels priority dev points assignee]", "default card fields")
	board := boards[0]
	board["cardFields"] = []string{"assignee", "dueDate", "daysInColumn"}
	var updated obj
	ada.must("PUT", "/api/v1/boards/"+str(board, "id"), board, 200, &updated)
	eq(t, fmt.Sprint(updated["cardFields"]), "[assignee dueDate daysInColumn]", "card fields saved in order")
	delete(board, "cardFields")
	ada.must("PUT", "/api/v1/boards/"+str(board, "id"), board, 200, &updated)
	eq(t, fmt.Sprint(updated["cardFields"]), "[assignee dueDate daysInColumn]", "omitted card fields are kept")
	board["cardFields"] = []string{"assignee", "assignee"}
	ada.must("PUT", "/api/v1/boards/"+str(board, "id"), board, 400, nil)
	board["cardFields"] = []string{"colour"}
	ada.must("PUT", "/api/v1/boards/"+str(board, "id"), board, 400, nil)
}
