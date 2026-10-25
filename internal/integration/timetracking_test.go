package integration

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

const hour = 3600

// tt reads an issue's time tracking as "original/remaining/spent" in hours ("-" when unset).
func tt(issue obj) string {
	t, _ := issue["timeTracking"].(obj)
	h := func(k string) string {
		v, ok := t[k].(float64)
		if !ok {
			return "-"
		}
		return fmt.Sprint(v / hour)
	}
	return h("originalSeconds") + "/" + h("remainingSeconds") + "/" + h("spentSeconds")
}

func TestTimeTracking(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin") // org admin, so project admin everywhere
	mel := e.login("mel@example.com", "Mel")
	bob := e.login("bob@example.com", "Bob")
	vic := e.login("vic@example.com", "Vic")
	admin.createProject("TT", "scrum")
	admin.must("POST", "/api/v1/projects/TT/members", obj{"userId": vic.id(), "roleKey": "viewer"}, 201, nil)

	var settings obj
	mel.get("/api/v1/time-tracking/settings", &settings)
	eq(t, fmt.Sprint(settings["hoursPerDay"], "/", settings["daysPerWeek"]), "8/5", "default working day and week")

	// An original estimate starts the remaining estimate; both are seconds.
	issue := mel.createIssue("TT", "story", "Estimate me", obj{"originalEstimate": 8 * hour})
	key := str(issue, "key")
	eq(t, tt(issue), "8/8/0", "created with an estimate")
	plain := mel.createIssue("TT", "task", "No estimate", nil)
	eq(t, tt(plain), "-/-/0", "no estimate")
	mel.must("POST", "/api/v1/issues", obj{"projectKey": "TT", "typeKey": "task", "summary": "x", "originalEstimate": -1}, 400, nil)

	// Logging work lowers the remaining estimate, never below zero...
	logWork := func(c *Client, body obj, want int) obj {
		var w obj
		c.must("POST", "/api/v1/issues/"+key+"/worklogs", body, want, &w)
		return w
	}
	version := issue["version"].(float64)
	w1 := logWork(mel, obj{"timeSpentSeconds": 3 * hour, "comment": "Spike", "startedAt": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)}, 201)
	eq(t, w1["canEdit"], true, "own worklog is editable")
	eq(t, str(w1, "author", "email"), "mel@example.com", "author")
	issue = mel.issue(key)
	eq(t, tt(issue), "8/5/3", "auto adjust")
	if issue["version"].(float64) <= version {
		t.Fatal("logging work bumps the issue version")
	}
	w2 := logWork(mel, obj{"timeSpentSeconds": 6 * hour}, 201)
	eq(t, tt(mel.issue(key)), "8/0/9", "remaining stops at zero; spent can overrun the estimate")
	// ...unless the request sets it or leaves it.
	logWork(mel, obj{"timeSpentSeconds": hour, "remainingEstimateSeconds": 2 * hour}, 201)
	eq(t, tt(mel.issue(key)), "8/2/10", "remaining set explicitly")
	w4 := logWork(mel, obj{"timeSpentSeconds": hour, "adjustEstimate": "leave"}, 201)
	eq(t, tt(mel.issue(key)), "8/2/11", "estimate left alone")
	logWork(mel, obj{"timeSpentSeconds": 30}, 400)
	logWork(mel, obj{"timeSpentSeconds": hour, "startedAt": time.Now().Add(72 * time.Hour).Format(time.RFC3339)}, 400)
	logWork(mel, obj{"timeSpentSeconds": hour, "adjustEstimate": "sideways"}, 400)

	// Permissions: viewers cannot log work; only the author or a project admin changes a worklog.
	logWork(vic, obj{"timeSpentSeconds": hour}, 403)
	var list []obj
	bob.get("/api/v1/issues/"+key+"/worklogs", &list)
	eq(t, len(list), 4, "worklogs listed")
	eq(t, str(list[0], "comment"), "Spike", "in the order the work was done")
	eq(t, list[0]["canEdit"], false, "someone else's worklog")
	bob.must("PATCH", "/api/v1/worklogs/"+str(w1, "id"), obj{"comment": "mine now"}, 403, nil)
	bob.must("DELETE", "/api/v1/worklogs/"+str(w1, "id"), nil, 403, nil)
	vic.must("PATCH", "/api/v1/worklogs/"+str(w1, "id"), obj{"comment": "x"}, 403, nil)
	var edited obj
	admin.must("PATCH", "/api/v1/worklogs/"+str(w4, "id"), obj{"comment": "Reviewed"}, 200, &edited)
	eq(t, str(edited, "comment"), "Reviewed", "admins edit anyone's worklog")
	eq(t, tt(mel.issue(key)), "8/2/11", "a comment change moves nothing")

	// Editing the time moves the remaining estimate by the difference; deleting gives it back.
	mel.must("PATCH", "/api/v1/worklogs/"+str(w1, "id"), obj{"timeSpentSeconds": 2 * hour}, 200, &edited)
	eq(t, fmt.Sprint(edited["timeSpentSeconds"]), fmt.Sprint(2*hour), "edited time")
	eq(t, tt(mel.issue(key)), "8/3/10", "an hour less logged, an hour more remaining")
	mel.must("DELETE", "/api/v1/worklogs/"+str(w2, "id"), nil, 204, nil)
	eq(t, tt(mel.issue(key)), "8/9/4", "deleted work goes back on the estimate")
	mel.must("DELETE", "/api/v1/worklogs/"+str(w4, "id")+"?adjustEstimate=leave", nil, 204, nil)
	eq(t, tt(mel.issue(key)), "8/9/3", "deleted, estimate left")
	mel.must("DELETE", "/api/v1/worklogs/"+str(w4, "id"), nil, 404, nil)

	// Estimates are edited like any field; setting the original on an unestimated issue sets both.
	mel.must("PATCH", "/api/v1/issues/"+key, obj{"remainingEstimate": 4 * hour, "originalEstimate": 16 * hour}, 200, &issue)
	eq(t, tt(issue), "16/4/3", "patched estimates")
	mel.must("PATCH", "/api/v1/issues/"+str(plain, "key"), obj{"originalEstimate": 2 * hour}, 200, &plain)
	eq(t, tt(plain), "2/2/0", "original estimate seeds the remaining estimate")
	mel.must("PATCH", "/api/v1/issues/"+str(plain, "key"), obj{"remainingEstimate": nil}, 200, &plain)
	eq(t, tt(plain), "2/-/0", "remaining estimate cleared")
	vic.must("PATCH", "/api/v1/issues/"+key, obj{"originalEstimate": hour}, 403, nil)

	var hist []obj
	mel.get("/api/v1/issues/"+key+"/history", &hist)
	var lines []string
	for _, h := range hist {
		if f := str(h, "field"); f == "worklog" || strings.HasSuffix(f, "Estimate") {
			lines = append(lines, fmt.Sprintf("%s:%s>%s", f, str(h, "from"), str(h, "to")))
		}
	}
	sameStrings(t, lines, []string{
		"worklog:>3h", "remainingEstimate:1d>5h",
		"worklog:>6h", "remainingEstimate:5h>0m",
		"worklog:>1h", "remainingEstimate:0m>2h",
		"worklog:>1h",
		"worklog:3h>2h", "remainingEstimate:2h>3h",
		"worklog:6h>", "remainingEstimate:3h>1d 1h",
		"worklog:1h>",
		"originalEstimate:1d>2d", "remainingEstimate:1d 1h>4h",
	}, "history of estimates and worklogs")

	// JQL over estimates, time spent and worklogs.
	mel.createIssue("TT", "task", "Untouched", nil)
	sameStrings(t, mel.search("project = TT AND timeSpent > 2h"), []string{key}, "time spent")
	sameStrings(t, mel.search("project = TT AND timeSpent < 1h ORDER BY key"), []string{"TT-2", "TT-3"}, "nothing logged counts as zero")
	sameStrings(t, mel.search("project = TT AND worklogAuthor = currentUser()"), []string{key}, "worklog author")
	sameStrings(t, bob.search("project = TT AND worklogAuthor = currentUser()"), []string{}, "someone else")
	sameStrings(t, mel.search("project = TT AND worklogAuthor is EMPTY ORDER BY key"), []string{"TT-2", "TT-3"}, "no worklogs")
	sameStrings(t, mel.search("project = TT AND originalEstimate >= 1d"), []string{key}, "original estimate in days")
	sameStrings(t, mel.search("project = TT AND remainingEstimate is EMPTY ORDER BY key"), []string{"TT-2", "TT-3"}, "unestimated")
	sameStrings(t, mel.search(`project = TT AND remainingEstimate = "4h"`), []string{key}, "exact remaining")
	sameStrings(t, mel.search("project = TT AND worklogDate >= -1d"), []string{key}, "work logged recently")
	sameStrings(t, mel.search("project = TT AND worklogDate <= -3d"), []string{}, "nothing logged that long ago")
	sameStrings(t, mel.search("project = TT ORDER BY timeSpent DESC, key"), []string{key, "TT-2", "TT-3"}, "order by time spent")
	mel.must("GET", "/api/v1/search?jql="+url.QueryEscape("timeSpent > lots"), nil, 400, nil)
	var meta obj
	mel.get("/api/v1/search/fields", &meta)
	if !strings.Contains(fmt.Sprint(meta["fields"]), "worklogAuthor") || !strings.Contains(fmt.Sprint(meta["fields"]), "duration") {
		t.Fatal("search metadata lists the time tracking fields")
	}

	// Board cards can show time tracking.
	var boards []obj
	admin.get("/api/v1/projects/TT/boards", &boards)
	var board obj
	admin.must("PUT", "/api/v1/boards/"+str(boards[0], "id"), obj{"name": "Board", "type": "scrum", "cardFields": []string{"assignee", "timeTracking"}}, 200, &board)
	eq(t, fmt.Sprint(board["cardFields"]), "[assignee timeTracking]", "time tracking card field")
}

func TestTimesheet(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("TS", "kanban")
	ada.createProject("OTH", "kanban")
	a := str(ada.createIssue("TS", "task", "A", nil), "key")
	b := str(ada.createIssue("TS", "task", "B", nil), "key")
	o := str(ada.createIssue("OTH", "task", "Other", nil), "key")
	ada.must("PATCH", "/api/v1/issues/"+a, obj{"assigneeId": bob.id()}, 200, nil)

	day := func(d int, h int) string {
		return time.Date(2026, 10, 19+d, h, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	log := func(c *Client, key string, hours float64, started string) {
		c.must("POST", "/api/v1/issues/"+key+"/worklogs", obj{"timeSpentSeconds": int(hours * hour), "startedAt": started}, 201, nil)
	}
	log(ada, a, 2, day(0, 9))
	log(ada, b, 1.5, day(0, 14))
	log(ada, a, 3, day(2, 10))
	log(bob, a, 4, day(1, 23)) // 23:00 UTC on Tuesday is Wednesday in Tokyo
	log(ada, o, 5, day(1, 9))

	get := func(query string) obj {
		var ts obj
		ada.get("/api/v1/worklogs?"+query, &ts)
		return ts
	}
	ts := get("from=2026-10-19&to=2026-10-25&projects=TS")
	eq(t, len(ts["dates"].([]any)), 7, "a column per day")
	eq(t, fmt.Sprint(ts["total"]), fmt.Sprint(int(10.5*hour)), "total")
	eq(t, fmt.Sprint(ts["dayTotals"].([]any)[:3]), fmt.Sprintf("[%d %d %d]", int(3.5*hour), 4*hour, 3*hour), "per day")
	people := ts["people"].([]any)
	eq(t, len(people), 2, "people who logged time")
	first := people[0].(obj)
	eq(t, str(first, "user", "displayName"), "Ada", "most time first")
	eq(t, fmt.Sprint(first["total"]), fmt.Sprint(int(6.5*hour)), "Ada's total")
	eq(t, fmt.Sprint(first["days"].([]any)[:3]), fmt.Sprintf("[%d 0 %d]", int(3.5*hour), 3*hour), "Ada per day")
	issues := first["issues"].([]any)
	eq(t, str(issues[0].(obj), "issue", "key"), a, "issues by time logged")
	eq(t, fmt.Sprint(issues[0].(obj)["total"]), fmt.Sprint(5*hour), "per issue total")

	// Days follow the requested time zone.
	ts = get("from=2026-10-19&to=2026-10-25&projects=TS&tz=Asia/Tokyo")
	bobRow := ts["people"].([]any)[1].(obj)
	eq(t, fmt.Sprint(bobRow["days"].([]any)[:3]), fmt.Sprintf("[0 0 %d]", 4*hour), "Bob's late work lands on Wednesday in Tokyo")

	// Every browsable project by default; one person's work with user.
	eq(t, fmt.Sprint(get("from=2026-10-19&to=2026-10-25")["total"]), fmt.Sprint(int(15.5*hour)), "all projects")
	only := get("from=2026-10-19&to=2026-10-25&user=" + bob.id())
	eq(t, len(only["people"].([]any)), 1, "one person")
	eq(t, fmt.Sprint(only["total"]), fmt.Sprint(4*hour), "Bob's work")
	eq(t, fmt.Sprint(get("from=2026-10-22&to=2026-10-22&projects=TS")["total"]), "0", "an empty day")

	ada.must("GET", "/api/v1/worklogs?from=2026-10-25&to=2026-10-19", nil, 400, nil)
	ada.must("GET", "/api/v1/worklogs?from=2025-01-01&to=2026-10-19", nil, 400, nil)
	ada.must("GET", "/api/v1/worklogs?from=2026-10-19&to=2026-10-25&tz=Mars/Olympus", nil, 400, nil)
	ada.must("GET", "/api/v1/worklogs?from=2026-10-19&to=2026-10-25&projects=NOPE", nil, 404, nil)

	// People only see work on issues they can browse.
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	log(ada, str(ada.createIssue("SEC", "task", "Hidden", nil), "key"), 1, day(0, 9))
	var bobView obj
	bob.get("/api/v1/worklogs?from=2026-10-19&to=2026-10-25", &bobView)
	eq(t, fmt.Sprint(bobView["total"]), fmt.Sprint(int(15.5*hour)), "private work left out")
}

func TestSprintReportInHours(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("HRS", "scrum")
	var sprints []obj
	c.get("/api/v1/projects/HRS/sprints", &sprints)
	sprint := str(sprints[0], "id")
	a := str(c.createIssue("HRS", "story", "A", obj{"sprintId": sprint, "originalEstimate": 10 * hour}), "key")
	b := str(c.createIssue("HRS", "story", "B", obj{"sprintId": sprint, "originalEstimate": 6 * hour}), "key")
	cc := str(c.createIssue("HRS", "story", "C", obj{"originalEstimate": 4 * hour}), "key")

	today := time.Now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -3)
	day := func(d int) time.Time { return start.AddDate(0, 0, d).Add(12 * time.Hour) }
	c.must("POST", "/api/v1/sprints/"+sprint+"/start", obj{"startDate": start.Format("2006-01-02"), "endDate": today.AddDate(0, 0, 7).Format("2006-01-02")}, 200, nil)
	for _, k := range []string{a, b, cc} {
		e.setIssueTimes(t, k, day(-1), nil)
	}
	// Day 1: 4h logged on A. Day 2: C joins and B is done.
	c.must("POST", "/api/v1/issues/"+a+"/worklogs", obj{"timeSpentSeconds": 4 * hour, "startedAt": day(1).Format(time.RFC3339)}, 201, nil)
	e.backdate(t, a, "remainingEstimate", day(1))
	e.exec("UPDATE worklogs SET created_at = $1", day(1)) // logged that day too
	// Work logged today for yesterday counts today, when the estimate moved with it.
	c.must("POST", "/api/v1/issues/"+a+"/worklogs", obj{"timeSpentSeconds": hour, "startedAt": day(2).Format(time.RFC3339)}, 201, nil)
	c.must("PATCH", "/api/v1/issues/"+cc, obj{"sprintId": sprint}, 200, nil)
	e.backdate(t, cc, "sprint", day(2))
	c.transition(b, "start-work", 200)
	c.transition(b, "done", 200)
	e.backdate(t, b, "status", day(2), day(2))

	var rep obj
	c.get("/api/v1/sprints/"+sprint+"/report", &rep)
	h := func(v any) string { return fmt.Sprint(v.(float64) / hour) }
	eq(t, h(rep["committedSeconds"]), "16", "remaining estimate at the start")
	eq(t, h(rep["completedSeconds"]), "5", "time logged in the sprint")
	eq(t, h(rep["addedSeconds"]), "4", "added scope in hours")
	eq(t, h(rep["removedSeconds"]), "0", "nothing removed")
	burn := rep["burndown"].([]any)
	point := func(d int) string {
		p := burn[d].(obj)
		return h(p["remainingSeconds"]) + " " + h(p["completedSeconds"]) + " " + h(p["scopeSeconds"])
	}
	eq(t, point(0), "16 0 16", "day 0: the commitment")
	eq(t, point(1), "12 4 16", "day 1: 4h logged on A")
	eq(t, point(2), "10 4 14", "day 2: C added, B done")
	eq(t, point(3), "9 5 14", "today: an hour logged (for yesterday), the estimate down with it")
	eq(t, h(burn[0].(obj)["idealSeconds"]), "16", "the guideline starts at the commitment")
}

func TestMCPLogWork(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	ada.createProject("AI", "kanban")
	key := str(ada.createIssue("AI", "task", "Teach the assistant to log time", obj{"originalEstimate": 8 * hour}), "key")
	var tok obj
	ada.must("POST", "/api/v1/me/tokens", obj{"name": "assistant", "scopes": []string{"read", "write"}, "expiresInDays": 7}, 201, &tok)
	cs := e.mcpSession(t, str(tok, "token"))

	out := mustCall(t, cs, "log_work", obj{"key": key, "timeSpent": "1h 30m", "started": "2026-10-20", "comment": "Pairing"})
	wantContains(t, out, "Logged 1h 30m on "+key, "2026-10-20 12:00", "estimated 1d · logged 1h 30m · remaining 6h 30m")
	out = mustCall(t, cs, "log_work", obj{"key": key, "timeSpent": "1d", "remainingEstimate": "2h"})
	wantContains(t, out, "logged 1d 1h 30m · remaining 2h", "over the estimate by 1h 30m")
	mustCall(t, cs, "log_work", obj{"key": key, "timeSpent": "30m", "leaveEstimate": true})
	wantContains(t, mustCall(t, cs, "get_issue", obj{"key": key}), "Time tracking: estimated 1d · logged 1d 2h · remaining 2h")
	if out, isErr := call(t, cs, "log_work", obj{"key": key, "timeSpent": "a while"}); !isErr || !strings.Contains(out, "timeSpent") {
		t.Fatalf("bad duration: %s", out)
	}
	var list []obj
	ada.get("/api/v1/issues/"+key+"/worklogs", &list)
	eq(t, len(list), 3, "worklogs from the assistant")
	eq(t, str(list[0], "comment"), "Pairing", "comment")
	eq(t, str(list[0], "author", "email"), "ada@example.com", "logged as the token owner")
}

func TestWorkingDayFromConfiguration(t *testing.T) {
	e := newEnv(t, withEnv("WFM_TIME_HOURS_PER_DAY", "6"), withEnv("WFM_TIME_DAYS_PER_WEEK", "4"))
	c := e.login("ada@example.com", "Ada")
	c.createProject("CFG", "kanban")
	var settings obj
	c.get("/api/v1/time-tracking/settings", &settings)
	eq(t, fmt.Sprint(settings["hoursPerDay"], "/", settings["daysPerWeek"]), "6/4", "configured units")
	key := str(c.createIssue("CFG", "task", "Six hours", obj{"originalEstimate": 6 * hour}), "key")
	sameStrings(t, c.search("project = CFG AND originalEstimate = 1d"), []string{key}, "1d is a 6-hour day")
	sameStrings(t, c.search("project = CFG AND originalEstimate < 1w"), []string{key}, "1w is four days")
	c.must("PATCH", "/api/v1/issues/"+key, obj{"originalEstimate": 30 * hour}, 200, nil)
	var hist []obj
	c.get("/api/v1/issues/"+key+"/history", &hist)
	last := hist[len(hist)-1]
	eq(t, str(last, "from")+" > "+str(last, "to"), "1d > 1w 1d", "history uses the working day and week")
}
