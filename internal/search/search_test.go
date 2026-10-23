package search

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustCompile(t *testing.T, src string, env Env) *Compiled {
	t.Helper()
	c, err := CompileString(src, env, 1, []Order{{Field: "rank"}})
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	return c
}

func TestParseStructure(t *testing.T) {
	q, err := Parse(`project = PLAT AND (status in ("To Do", 'In Progress') OR NOT assignee is EMPTY) ORDER BY priority DESC, created`)
	if err != nil {
		t.Fatal(err)
	}
	and, ok := q.Where.(And)
	if !ok {
		t.Fatalf("top node %T, want And", q.Where)
	}
	if c := and.Left.(Clause); c.Field != "project" || c.Op != "=" || c.Values[0].Text != "PLAT" {
		t.Fatalf("left clause %+v", c)
	}
	or := and.Right.(Or)
	if c := or.Left.(Clause); c.Op != "in" || len(c.Values) != 2 || c.Values[1].Text != "In Progress" {
		t.Fatalf("in clause %+v", c)
	}
	if c := or.Right.(Not).X.(Clause); c.Op != "is" || c.Values[0].Kind != VEmpty {
		t.Fatalf("is empty clause %+v", c)
	}
	if len(q.OrderBy) != 2 || !q.OrderBy[0].Desc || q.OrderBy[1].Field != "created" || q.OrderBy[1].Desc {
		t.Fatalf("order by %+v", q.OrderBy)
	}
}

func TestOrderOnly(t *testing.T) {
	q, err := Parse("ORDER BY updated DESC")
	if err != nil || q.Where != nil || len(q.OrderBy) != 1 {
		t.Fatalf("got %+v, %v", q, err)
	}
	if q, err := Parse("   "); err != nil || q.Where != nil {
		t.Fatalf("empty query: %+v, %v", q, err)
	}
}

func TestSyntaxErrorsHavePositions(t *testing.T) {
	cases := map[string]int{
		`status = `:                     9,
		`status "x"`:                    7,
		`status in (a, b`:               15,
		`summary ~ "unterminated`:       10,
		`project = A ORDER created`:     18,
		`(status = x`:                   11,
		`assignee is maybe`:             12,
		`priority = High extra = stuff`: 16,
	}
	for src, pos := range cases {
		_, err := Parse(src)
		e, ok := err.(*Error)
		if !ok {
			t.Errorf("%q: expected *Error, got %v", src, err)
			continue
		}
		if e.Pos != pos {
			t.Errorf("%q: error at %d (%s), want %d", src, e.Pos, e.Msg, pos)
		}
	}
}

func TestCompileFields(t *testing.T) {
	me := uuid.New()
	env := Env{UserID: &me, Now: time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), CustomFields: []CustomField{
		{Key: "customer", Name: "Customer", Type: "select"},
		{Key: "severity_score", Name: "Severity score", Type: "number"},
		{Key: "platforms", Name: "Platforms", Type: "multiselect"},
	}}
	cases := map[string][]string{
		`assignee = currentUser()`:                     {"i.assignee_id IN ($2::uuid)"},
		`assignee is EMPTY`:                            {"i.assignee_id IS NULL"},
		`assignee in (currentUser(), EMPTY)`:           {"i.assignee_id IN ($2::uuid) OR i.assignee_id IS NULL"},
		`reporter = "ada@example.com"`:                 {"i.reporter_id IN (SELECT id FROM users WHERE email = lower($2::text)"},
		`assignee in membersOf("Platform")`:            {"group_members gm JOIN groups g"},
		`status != Done`:                               {"NOT coalesce", "statuses WHERE lower(name)"},
		`statusCategory = "In Progress"`:               {"category = $2::text"},
		`priority >= High`:                             {"array_position", ">= $2::int"},
		`labels is EMPTY`:                              {"cardinality(i.labels) = 0"},
		`sprint in openSprints()`:                      {"sprints WHERE state = $2::text"},
		`fixVersion in unreleasedVersions()`:           {"r.state = $2::text"},
		`parent = PLAT-1`:                              {"i.parent_id IN (SELECT x.id FROM issues x WHERE x.number = $2::int"},
		`key > PLAT-10`:                                {"i.number > $3::int"},
		`text ~ "login bug"`:                           {"websearch_to_tsquery"},
		`summary !~ crash`:                             {"NOT i.summary ILIKE $2::text"},
		`comment ~ "LGTM"`:                             {"comments cm"},
		`created >= -7d`:                               {"i.created_at >= $2::timestamptz"},
		`created = 2026-10-01`:                         {"i.created_at >= $2::timestamptz AND i.created_at < $3::timestamptz"},
		`due < startOfWeek("+1w")`:                     {"i.due_date < $2::date"},
		`storyPoints > 3 AND storyPoints is not EMPTY`: {"i.story_points > $2::numeric", "NOT coalesce((i.story_points IS NULL), false)"},
		`customer = Acme`:                              {"lower(i.fields->>$2::text) = lower($3::text)"},
		`"Severity score" >= 7`:                        {"(i.fields->>$2::text)::numeric >= $3::numeric"},
		`cf[platforms] = ios`:                          {"jsonb_exists"},
		`level <= 0`:                                   {"SELECT level FROM issue_types"},
		`watcher = currentUser()`:                      {"watchers w"},
		`resolution = Unresolved`:                      {"i.resolution IS NULL"},
	}
	for src, wants := range cases {
		c := mustCompile(t, src, env)
		for _, w := range wants {
			if !strings.Contains(c.Where, w) {
				t.Errorf("%q compiled to\n  %s\nmissing %q", src, c.Where, w)
			}
		}
	}
}

func TestDates(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC) // a Wednesday
	c := mustCompile(t, `created >= startOfWeek() AND updated > -2d AND resolved < "2026-10-01 09:30"`, Env{Now: now})
	if got := c.Args[0].(time.Time); !got.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("startOfWeek = %v", got)
	}
	if got := c.Args[1].(time.Time); !got.Equal(now.AddDate(0, 0, -2)) {
		t.Errorf("-2d = %v", got)
	}
	if got := c.Args[2].(time.Time); !got.Equal(time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("datetime = %v", got)
	}
	c = mustCompile(t, `due = 2026-12-24`, Env{Now: now})
	if c.Args[0] != "2026-12-24" {
		t.Errorf("date-only column arg = %v", c.Args[0])
	}
}

func TestSemanticErrors(t *testing.T) {
	for _, src := range []string{
		`colour = red`,
		`priority = urgent`,
		`created > yesterday`,
		`summary > x`,
		`project is EMPTY`,
		`sprint in brokenSprints()`,
		`parent = notakey`,
		`statusCategory = weird`,
		`ORDER BY colour`,
		`text = foo`,
	} {
		if _, err := CompileString(src, Env{}, 0, nil); err == nil {
			t.Errorf("%q should fail to compile", src)
		} else if _, ok := err.(*Error); !ok {
			t.Errorf("%q: expected *Error, got %T", src, err)
		}
	}
}

func TestValuesAreParameters(t *testing.T) {
	c := mustCompile(t, `summary ~ "'; DROP TABLE issues; --" AND labels = "x' OR 1=1"`, Env{})
	if strings.Contains(c.Where, "DROP") || strings.Contains(c.Where, "1=1") {
		t.Fatalf("value leaked into SQL: %s", c.Where)
	}
	if len(c.Args) != 2 {
		t.Fatalf("args = %v", c.Args)
	}
	if c.Args[0] != "%'; DROP TABLE issues; --%" {
		t.Errorf("like pattern = %q", c.Args[0])
	}
}

func TestOrderBy(t *testing.T) {
	c := mustCompile(t, `ORDER BY priority DESC, key`, Env{})
	if !strings.HasPrefix(c.OrderBy, "array_position") || !strings.Contains(c.OrderBy, "DESC NULLS LAST") || !strings.HasSuffix(c.OrderBy, "i.id") {
		t.Errorf("order by = %s", c.OrderBy)
	}
	c = mustCompile(t, `project = X`, Env{})
	if c.OrderBy != "i.rank ASC NULLS LAST, i.id" {
		t.Errorf("default order = %s", c.OrderBy)
	}
}
