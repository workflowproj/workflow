package tracker

import (
	"testing"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/db/dbq"
)

func TestReleaseNotes(t *testing.T) {
	p := dbq.Project{Key: "SHOP"}
	types := map[uuid.UUID]dbq.IssueType{}
	typ := func(key, name string, level int32) uuid.UUID {
		id := uuid.New()
		types[id] = dbq.IssueType{ID: id, Key: key, Name: name, Level: level}
		return id
	}
	story, bug, task := typ("story", "Story", 0), typ("bug", "Bug", 0), typ("task", "Task", 0)
	sub, epic, risk := typ("subtask", "Sub-task", -1), typ("epic", "Epic", 1), typ("risk", "Risk", 0)
	feature := typ("feature_request", "Feature", 0) // matched by name
	issues := []dbq.Issue{
		{Number: 1, TypeID: epic, Summary: "Checkout"},
		{Number: 2, TypeID: story, Summary: "Pay by card"},
		{Number: 3, TypeID: bug, Summary: "Totals off by one [rounding] <b>"},
		{Number: 4, TypeID: task, Summary: "  Faster search  "},
		{Number: 5, TypeID: sub, Summary: "Write the migration"},
		{Number: 6, TypeID: risk, Summary: "Card provider outage"},
		{Number: 7, TypeID: feature, Summary: "Gift cards"},
	}
	notes, n := ReleaseNotes(p, issues, types)
	want := "## Features\n\n- Pay by card (SHOP-2)\n- Gift cards (SHOP-7)\n\n" +
		"## Bug fixes\n\n- Totals off by one \\[rounding\\] \\<b\\> (SHOP-3)\n\n" +
		"## Improvements\n\n- Faster search (SHOP-4)\n\n" +
		"## Epics\n\n- Checkout (SHOP-1)\n\n" +
		"## Risks\n\n- Card provider outage (SHOP-6)\n"
	if notes != want {
		t.Errorf("notes:\n%s\nwant:\n%s", notes, want)
	}
	if n != 6 {
		t.Errorf("issues listed = %d, want 6 (sub-tasks left out)", n)
	}
	if notes, n := ReleaseNotes(p, nil, types); notes != "" || n != 0 {
		t.Errorf("no issues: %q %d", notes, n)
	}
}

func TestPluralName(t *testing.T) {
	for in, want := range map[string]string{
		"Epic": "Epics", "Story": "Stories", "Day": "Days", "Fix": "Fixes", "Patch": "Patches", "Service request": "Service requests", "": "Other",
	} {
		if got := pluralName(in); got != want {
			t.Errorf("pluralName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNamesRelease(t *testing.T) {
	r := dbq.Release{ID: uuid.New(), Name: "1.0"}
	other := uuid.NewString()
	s := func(v string) *string { return &v }
	cases := []struct {
		byRef      bool
		ref, value *string
		want       bool
	}{
		{true, s(other + "," + r.ID.String()), s("2.0, 1.0-renamed"), true},
		{true, s(other), s("1.0"), false}, // ids decide once they are recorded
		{true, nil, nil, false},
		{false, nil, s("1.0.1, 1.0"), true},
		{false, nil, s("1.0.1, 10"), false},
		{false, nil, nil, false},
	}
	for _, c := range cases {
		if got := namesRelease(r, c.byRef, c.ref, c.value); got != c.want {
			t.Errorf("namesRelease(%v, %v, %v) = %v", c.byRef, c.ref, c.value, got)
		}
	}
}

func TestTagLinks(t *testing.T) {
	if got := TagURL("github", "https://github.com/acme/shop", "v1.2.0"); got != "https://github.com/acme/shop/tree/v1.2.0" {
		t.Error(got)
	}
	if got := TagURL("gitlab", "https://gitlab.example.com/grp/app", "release/1"); got != "https://gitlab.example.com/grp/app/-/tags/release%2F1" {
		t.Error(got)
	}
	if got := CompareURL("github", "https://github.com/acme/shop", "v1.1.0", "v1.2.0"); got != "https://github.com/acme/shop/compare/v1.1.0...v1.2.0" {
		t.Error(got)
	}
	if got := CompareURL("gitlab", "https://gitlab.example.com/grp/app", "v1", "v2"); got != "https://gitlab.example.com/grp/app/-/compare/v1...v2" {
		t.Error(got)
	}
}
