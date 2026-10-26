package integration

import (
	"fmt"
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
