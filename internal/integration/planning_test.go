package integration

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Sprints created without a name continue the project's numbering, and every sprint count
// (backlog, completing, the report, velocity) leaves sub-tasks to their parent.
func TestSprintNamingAndCounts(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("CNT", "scrum")
	var sprints []obj
	c.get("/api/v1/projects/CNT/sprints", &sprints)
	eq(t, len(sprints), 1, "the template's first sprint")
	eq(t, str(sprints[0], "name"), "CNT Sprint 1", "first sprint keeps the project's scheme")
	first := str(sprints[0], "id")

	// The team names its sprints "Sprint 23", "Sprint 24": the next one is "Sprint 25".
	c.must("PATCH", "/api/v1/sprints/"+first, obj{"name": "Sprint 23"}, 200, nil)
	c.must("POST", "/api/v1/projects/CNT/sprints", obj{"name": "Sprint 24"}, 201, nil)
	var next obj
	c.must("POST", "/api/v1/projects/CNT/sprints", obj{}, 201, &next)
	eq(t, str(next, "name"), "Sprint 25", "numbering continues from the latest sprint")
	// A latest name without a number falls back to the project's scheme.
	c.must("PATCH", "/api/v1/sprints/"+str(next, "id"), obj{"name": "Hardening"}, 200, nil)
	c.must("POST", "/api/v1/projects/CNT/sprints", obj{}, 201, &next)
	eq(t, str(next, "name"), "CNT Sprint 4", "fallback name")
	target := str(next, "id")

	// Two stories in the first sprint, each with a sub-task (which follows its parent).
	a := c.createIssue("CNT", "story", "A", obj{"sprintId": first, "storyPoints": 3})
	b := c.createIssue("CNT", "story", "B", obj{"sprintId": first, "storyPoints": 5})
	a1 := c.createIssue("CNT", "subtask", "A1", obj{"parentKey": str(a, "key"), "storyPoints": 1})
	b1 := c.createIssue("CNT", "subtask", "B1", obj{"parentKey": str(b, "key")})
	eq(t, str(c.issue(str(a1, "key")), "sprintId"), first, "sub-task follows its parent's sprint")

	var rep obj
	c.get("/api/v1/sprints/"+first+"/report", &rep)
	eq(t, fmt.Sprint(rep["committedIssues"], "/", rep["committedPoints"]), "2/8", "a future sprint's scope leaves sub-tasks out")
	var backlog obj
	var boards []obj
	c.get("/api/v1/projects/CNT/boards", &boards)
	c.get("/api/v1/boards/"+str(boards[0], "id")+"/backlog", &backlog)
	eq(t, len(backlog["sprints"].([]any)[0].(obj)["issues"].([]any)), 2, "backlog shows the two stories")

	start := time.Now().UTC().Format("2006-01-02")
	end := time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02")
	c.must("POST", "/api/v1/sprints/"+first+"/start", obj{"startDate": start, "endDate": end}, 200, nil)
	c.get("/api/v1/sprints/"+first+"/report", &rep)
	eq(t, fmt.Sprint(rep["committedIssues"], "/", rep["committedPoints"]), "2/8", "committed at sprint start, sub-tasks left out")

	// A and its sub-task are done; B is open with its sub-task done.
	c.transition(str(a1, "key"), "done", 200)
	c.transition(str(a, "key"), "start-work", 200)
	c.transition(str(a, "key"), "done", 200)
	c.transition(str(b1, "key"), "done", 200)
	// The complete dialog's counts.
	sameStrings(t, c.search(fmt.Sprintf("sprint = %s AND level >= 0 AND statusCategory = Done", first)), []string{str(a, "key")}, "completed issues")
	sameStrings(t, c.search(fmt.Sprintf("sprint = %s AND level >= 0 AND statusCategory != Done", first)), []string{str(b, "key")}, "open issues")
	c.get("/api/v1/sprints/"+first+"/report", &rep)
	eq(t, fmt.Sprint(rep["completedIssues"], "/", rep["completedPoints"]), "1/3", "completed so far")
	burndown := rep["burndown"].([]any)
	last := burndown[len(burndown)-1].(obj)
	eq(t, fmt.Sprint(last["scopeIssues"], "/", last["completedIssues"]), "2/1", "burndown counts the stories")

	// Completing moves B with its sub-task, done or not: sub-tasks go with their parent.
	c.must("POST", "/api/v1/sprints/"+first+"/complete", obj{"moveOpenIssuesTo": target}, 200, nil)
	eq(t, str(c.issue(str(b, "key")), "sprintId"), target, "open story moved")
	eq(t, str(c.issue(str(b1, "key")), "sprintId"), target, "its sub-task went with it")
	eq(t, str(c.issue(str(a, "key")), "sprintId"), first, "done story stays")
	eq(t, str(c.issue(str(a1, "key")), "sprintId"), first, "and its sub-task")
	c.get("/api/v1/sprints/"+first+"/report", &rep)
	eq(t, fmt.Sprint(rep["committedIssues"], "/", rep["completedIssues"], "/", rep["removedIssues"]), "2/1/0", "report after completing")
	var vel obj
	c.get("/api/v1/projects/CNT/velocity?count=3", &vel)
	s0 := vel["sprints"].([]any)[0].(obj)
	eq(t, fmt.Sprint(s0["committedIssues"], "/", s0["completedIssues"], "/", s0["committedPoints"], "/", s0["completedPoints"]), "2/1/8/3", "velocity counts the same way")
	c.get("/api/v1/sprints/"+target+"/report", &rep)
	eq(t, fmt.Sprint(rep["committedIssues"]), "1", "the next sprint holds B (its sub-task is part of it)")
}

// A published version that maps a removed status to a new one (a rename, say) keeps boards
// showing that work: the column that showed the old status shows the new one too.
func TestBoardsFollowStatusMappings(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("REN", "scrum")
	admin.createProject("ALT", "scrum")
	var wfs []obj
	admin.get("/api/v1/workflows?project=REN", &wfs)
	var wfID string
	for _, w := range wfs {
		if str(w, "name") == "Software development" && str(w, "projectKey") == "REN" {
			wfID = str(w, "id")
		}
	}
	if wfID == "" {
		t.Fatalf("workflow not found in %v", wfs)
	}
	var wf obj
	admin.get("/api/v1/workflows/"+wfID, &wf)
	v1 := str(wf, "version", "yaml")

	// An issue in In Review needs the mapping.
	story := admin.createIssue("REN", "story", "Review me", nil)
	admin.transition(str(story, "key"), "start-work", 200)
	admin.transition(str(story, "key"), "submit-for-review", 200)
	renamed := strings.ReplaceAll(v1, "In Review", "Code Review")
	r := admin.do("POST", "/api/v1/workflows/"+wfID+"/versions", obj{"yaml": renamed})
	eq(t, r.Status, 422, "mapping required")
	fields := r.JSON()["errors"].([]any)
	eq(t, str(fields[0].(obj), "message"), `1 issue is in "In Review", which this version removes`, "pluralised message")
	admin.must("POST", "/api/v1/workflows/"+wfID+"/versions", obj{"yaml": renamed, "statusMapping": obj{"In Review": "Code Review"}}, 201, nil)

	board := func(project string) (cols map[string][]string, unmapped int, cards map[string][]string) {
		var boards []obj
		admin.get("/api/v1/projects/"+project+"/boards", &boards)
		var data obj
		admin.get("/api/v1/boards/"+str(boards[0], "id")+"/data", &data)
		names := map[string]string{}
		var sts []obj
		admin.get("/api/v1/statuses", &sts)
		for _, s := range sts {
			names[str(s, "id")] = str(s, "name")
		}
		cols, cards = map[string][]string{}, map[string][]string{}
		for _, raw := range boards[0]["columns"].([]any) {
			col := raw.(obj)
			for _, id := range col["statusIds"].([]any) {
				cols[str(col, "name")] = append(cols[str(col, "name")], names[id.(string)])
			}
		}
		for _, raw := range data["columns"].([]any) {
			col := raw.(obj)
			for _, i := range col["issues"].([]any) {
				cards[str(col, "name")] = append(cards[str(col, "name")], str(i.(obj), "key"))
			}
		}
		return cols, len(data["unmappedStatusIds"].([]any)), cards
	}
	cols, unmapped, _ := board("REN")
	sameStrings(t, cols["In Review"], []string{"In Review", "Code Review"}, "the renamed status shows in the same column")
	eq(t, unmapped, 0, "nothing unmapped")
	e.DrainJobs()
	// The board is a Scrum board: put the story in the active sprint to see it.
	var sprints []obj
	admin.get("/api/v1/projects/REN/sprints", &sprints)
	admin.must("PATCH", "/api/v1/issues/"+str(story, "key"), obj{"sprintId": str(sprints[0], "id")}, 200, nil)
	admin.must("POST", "/api/v1/sprints/"+str(sprints[0], "id")+"/start", obj{"startDate": time.Now().UTC().Format("2006-01-02"), "endDate": time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")}, 200, nil)
	_, _, cards := board("REN")
	sameStrings(t, cards["In Review"], []string{str(story, "key")}, "the migrated issue is on the board, in the same column")
	alt, _, _ := board("ALT")
	sameStrings(t, alt["In Review"], []string{"In Review"}, "boards of projects not using the workflow are untouched")

	// A rename with no issues in the status still moves the board (the editor sends renames).
	admin.get("/api/v1/workflows/"+wfID, &wf)
	again := strings.ReplaceAll(str(wf, "version", "yaml"), "Code Review", "Peer Review")
	admin.must("POST", "/api/v1/workflows/"+wfID+"/versions", obj{"yaml": again, "statusMapping": obj{"Code Review": "Peer Review"}}, 201, nil)
	e.DrainJobs()
	cols, unmapped, _ = board("REN")
	sameStrings(t, cols["In Review"], []string{"In Review", "Code Review", "Peer Review"}, "followed again")
	eq(t, unmapped, 0, "nothing unmapped after the second rename")

	// Changing an issue type's workflow with a mapping: the board follows it too, except where
	// the new status has a column already.
	qa := "name: QA flow\nstatuses:\n  - { name: Open, category: todo }\n  - { name: In Progress, category: in_progress }\n  - { name: QA, category: in_progress }\n  - { name: Done, category: done }\ntransitions:\n  - { name: Start, from: [Open], to: In Progress }\n  - { name: Test, from: [In Progress], to: QA }\n  - { name: Pass, from: [QA], to: Done }\n"
	var created obj
	admin.must("POST", "/api/v1/workflows", obj{"projectKey": "ALT", "yaml": qa}, 201, &created)
	var altBoards []obj
	admin.get("/api/v1/projects/ALT/boards", &altBoards)
	columns := altBoards[0]["columns"].([]any)
	columns = append(columns, obj{"name": "Testing", "statusIds": []string{admin.statusID("QA")}})
	admin.must("PUT", "/api/v1/boards/"+str(altBoards[0], "id"), obj{"name": str(altBoards[0], "name"), "type": "scrum", "columns": columns}, 200, nil)
	bug := admin.createIssue("ALT", "bug", "Flaky", nil)
	admin.transition(str(bug, "key"), "start-work", 200)
	admin.transition(str(bug, "key"), "submit-for-review", 200)
	admin.createIssue("ALT", "bug", "Waiting", nil)
	admin.createIssue("ALT", "bug", "Also waiting", nil)
	var types []obj
	admin.get("/api/v1/projects/ALT/issue-types", &types)
	bugType := types[slices.IndexFunc(types, func(t obj) bool { return str(t, "key") == "bug" })]
	r = admin.do("PATCH", "/api/v1/projects/ALT/issue-types/"+str(bugType, "id"), obj{"workflowId": str(created, "id")})
	eq(t, r.Status, 422, "mapping required for the type")
	msgs := map[string]string{}
	for _, f := range r.JSON()["errors"].([]any) {
		msgs[str(f.(obj), "field")] = str(f.(obj), "message")
	}
	eq(t, msgs["To Do"], `2 issues are in "To Do"`, "plural")
	eq(t, msgs["In Review"], `1 issue is in "In Review"`, "singular")
	admin.must("PATCH", "/api/v1/projects/ALT/issue-types/"+str(bugType, "id"), obj{"workflowId": str(created, "id"), "statusMapping": obj{"To Do": "Open", "In Review": "QA"}}, 200, nil)
	e.DrainJobs()
	alt, unmapped, _ = board("ALT")
	sameStrings(t, alt["To Do"], []string{"To Do", "Open"}, "Open shows where To Do did")
	sameStrings(t, alt["In Review"], []string{"In Review"}, "QA has its own column already")
	sameStrings(t, alt["Testing"], []string{"QA"}, "and stays there")
	eq(t, unmapped, 0, "nothing unmapped after the type change")
}
