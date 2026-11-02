package integration

import (
	"strings"
	"testing"
)

// viewKeys lists a saved timeline's project keys, in order.
func viewKeys(v obj) []string {
	var out []string
	for _, p := range v["projects"].([]any) {
		out = append(out, p.(map[string]any)["key"].(string))
	}
	return out
}

func TestTimelineViews(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada") // the first to sign in: an org admin
	bob := e.login("bob@example.com", "Bob")
	cat := e.login("cat@example.com", "Cat")
	ada.createProject("APP", "scrum")
	ada.createProject("API", "kanban")
	ada.createProject("OPS", "kanban")
	ada.createProject("PROG", "programme")
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)

	// Create: the projects keep the order given, without repeats.
	var mine obj
	bob.must("POST", "/api/v1/timeline-views", obj{"name": "  Payments teams ", "projects": []string{"API", "APP", "API"}}, 201, &mine)
	eq(t, str(mine, "name"), "Payments teams", "name trimmed")
	sameOrder(t, viewKeys(mine), []string{"API", "APP"}, "projects in the order chosen")
	eq(t, mine["mine"], true, "owner")
	eq(t, mine["shared"], false, "private by default")
	eq(t, mine["hiddenProjects"].(float64), 0, "nothing hidden")
	id := str(mine, "id")

	// Validation.
	for name, body := range map[string]obj{
		"no name":        {"name": " ", "projects": []string{"APP"}},
		"long name":      {"name": strings.Repeat("x", 101), "projects": []string{"APP"}},
		"no projects":    {"name": "Empty", "projects": []string{}},
		"blank projects": {"name": "Blank", "projects": []string{" "}},
	} {
		if r := bob.do("POST", "/api/v1/timeline-views", body); r.Status != 400 {
			t.Errorf("%s: %d %s, want 400", name, r.Status, r.Body)
		}
	}
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "APP"
	}
	// Repeats collapse, so 51 copies of one key is one project.
	bob.must("POST", "/api/v1/timeline-views", obj{"name": "Repeats", "projects": tooMany}, 201, nil)
	bob.must("POST", "/api/v1/timeline-views", obj{"name": "Unknown", "projects": []string{"NOPE"}}, 404, nil)
	bob.must("POST", "/api/v1/timeline-views", obj{"name": "Hidden", "projects": []string{"SEC"}}, 404, nil)
	r := bob.do("POST", "/api/v1/timeline-views", obj{"name": "Programme", "projects": []string{"APP", "PROG"}})
	eq(t, r.Status, 422, "a programme has its own timeline")
	if !strings.Contains(string(r.Body), "own timeline") {
		t.Errorf("programme refusal should say why: %s", r.Body)
	}

	// Private timelines are their owner's alone.
	var list []obj
	cat.get("/api/v1/timeline-views", &list)
	eq(t, len(list), 0, "cat sees none of bob's private timelines")
	cat.must("GET", "/api/v1/timeline-views/"+id, nil, 404, nil)
	cat.must("PUT", "/api/v1/timeline-views/"+id, obj{"name": "Mine now", "projects": []string{"APP"}}, 404, nil)
	cat.must("DELETE", "/api/v1/timeline-views/"+id, nil, 404, nil)
	ada.must("GET", "/api/v1/timeline-views/"+id, nil, 404, nil)

	// Ada shares a timeline that includes a private project: others see only what they can browse.
	var shared obj
	ada.must("POST", "/api/v1/timeline-views", obj{"name": "Everything we run", "description": "Ops review", "shared": true, "projects": []string{"SEC", "OPS", "APP"}}, 201, &shared)
	sid := str(shared, "id")
	var seen obj
	bob.get("/api/v1/timeline-views/"+sid, &seen)
	sameOrder(t, viewKeys(seen), []string{"OPS", "APP"}, "bob sees the projects he can browse")
	eq(t, seen["hiddenProjects"].(float64), 1, "the private project is counted, not named")
	eq(t, seen["mine"], false, "not bob's")
	eq(t, str(seen, "owner", "displayName"), "Ada", "owner shown")
	bob.get("/api/v1/timeline-views", &list)
	sameStrings(t, names(list), []string{"Everything we run", "Payments teams", "Repeats"}, "bob's list: his own and shared")
	if strings.Contains(string(bob.do("GET", "/api/v1/timeline-views/"+sid, nil).Body), "SEC") {
		t.Errorf("the hidden project's key must not appear in bob's view")
	}

	// Only the owner, or an org admin for a shared timeline, changes or deletes it.
	bob.must("PUT", "/api/v1/timeline-views/"+sid, obj{"name": "Bob's", "projects": []string{"APP"}}, 403, nil)
	bob.must("DELETE", "/api/v1/timeline-views/"+sid, nil, 403, nil)
	bob.must("PUT", "/api/v1/timeline-views/"+id, obj{"name": "Payments", "shared": true, "projects": []string{"APP", "OPS"}}, 200, &mine)
	eq(t, mine["shared"], true, "now shared")
	cat.must("PUT", "/api/v1/timeline-views/"+id, obj{"name": "Cat's", "projects": []string{"APP"}}, 403, nil)
	var edited obj
	ada.must("PUT", "/api/v1/timeline-views/"+id, obj{"name": "Payments (reviewed)", "shared": true, "projects": []string{"OPS", "APP"}}, 200, &edited)
	sameOrder(t, viewKeys(edited), []string{"OPS", "APP"}, "an org admin can reorder a shared timeline")
	eq(t, edited["mine"], false, "still bob's")

	// Someone editing a timeline keeps the projects they can't browse, after the ones they chose.
	ada.must("PUT", "/api/v1/timeline-views/"+sid, obj{"name": "Everything we run", "shared": true, "projects": []string{"SEC", "OPS", "APP"}}, 200, nil)
	var member obj
	ada.must("POST", "/api/v1/projects/SEC/members", obj{"userId": bob.id(), "roleKey": "admin"}, 201, &member)
	ada.must("PUT", "/api/v1/timeline-views/"+id, obj{"name": "Payments", "shared": true, "projects": []string{"APP", "SEC"}}, 200, nil)
	ada.must("DELETE", "/api/v1/projects/SEC/members/"+str(member, "id"), nil, 204, nil)
	// Bob can no longer browse SEC; his edit names APP only, and SEC stays in.
	bob.must("PUT", "/api/v1/timeline-views/"+id, obj{"name": "Payments", "shared": true, "projects": []string{"APP"}}, 200, &mine)
	sameOrder(t, viewKeys(mine), []string{"APP"}, "bob sees APP")
	eq(t, mine["hiddenProjects"].(float64), 1, "SEC kept, hidden from bob")
	ada.get("/api/v1/timeline-views/"+id, &seen)
	sameOrder(t, viewKeys(seen), []string{"APP", "SEC"}, "ada still sees SEC in it")

	// A deleted project drops out of every timeline.
	ada.must("DELETE", "/api/v1/projects/OPS", nil, 204, nil)
	bob.get("/api/v1/timeline-views/"+sid, &seen)
	sameOrder(t, viewKeys(seen), []string{"APP"}, "OPS is gone")

	// Delete.
	ada.must("DELETE", "/api/v1/timeline-views/"+sid, nil, 204, nil)
	bob.must("GET", "/api/v1/timeline-views/"+sid, nil, 404, nil)
	bob.must("DELETE", "/api/v1/timeline-views/"+id, nil, 204, nil)
	bob.get("/api/v1/timeline-views", &list)
	sameStrings(t, names(list), []string{"Repeats"}, "bob's remaining timeline")
}

// names lists the "name" of each item.
func names(items []obj) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, str(i, "name"))
	}
	return out
}

// sameOrder compares two lists, order included.
func sameOrder(t *testing.T, got, want []string, what string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}
