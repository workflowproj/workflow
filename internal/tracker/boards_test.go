package tracker

import (
	"testing"

	"github.com/workflowproj/workflow/internal/db/dbq"
)

func TestNextSprintName(t *testing.T) {
	sprints := func(names ...string) []dbq.Sprint {
		out := make([]dbq.Sprint, len(names))
		for i, n := range names {
			out[i] = dbq.Sprint{Name: n, Position: int32(i + 1)}
		}
		return out
	}
	cases := []struct {
		name    string
		sprints []dbq.Sprint
		want    string
	}{
		{"first sprint", nil, "PLAT Sprint 1"},
		{"continues the latest number", sprints("PLAT Sprint 1", "Sprint 23", "Sprint 24"), "Sprint 25"},
		{"latest by position, not by list order", []dbq.Sprint{{Name: "Sprint 24", Position: 3}, {Name: "Sprint 23", Position: 2}}, "Sprint 25"},
		{"keeps zero padding", sprints("Team S09"), "Team S10"},
		{"a name that is only a number", sprints("41"), "42"},
		{"skips names already taken", sprints("Sprint 25", "Sprint 24"), "Sprint 26"},
		{"no number: the project's default", sprints("Kick-off", "Discovery"), "PLAT Sprint 3"},
		{"default skips taken names", sprints("PLAT Sprint 2", "Discovery"), "PLAT Sprint 3"},
		{"too long to be a counter", sprints("Build 20261026123456"), "PLAT Sprint 2"},
	}
	for _, c := range cases {
		if got := nextSprintName("PLAT", c.sprints); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
