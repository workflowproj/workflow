package search

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/timetrack"
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
		`deployedTo = production`:                      {"deployment_issues di", "d.status = 'success'", "lower(d.environment) = lower($2::text)"},
		`deployedTo is EMPTY`:                          {"NOT EXISTS (SELECT 1 FROM deployment_issues di"},
		`parent = PLAT-1`:                              {"i.parent_id IN (SELECT x.id FROM issues x WHERE x.number = $2::int"},
		`key > PLAT-10`:                                {"i.number > $3::int"},
		`text ~ "login bug"`:                           {"websearch_to_tsquery('simple', $2::text)", "issue_search_document_english(i.summary, i.description) @@ websearch_to_tsquery('english', $2::text)"},
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
		`kind in (risk, Decision)`:                     {"issue_types WHERE kind = $2::text", "kind = $3::text"},
		`issueLinkType = "is mitigated by"`:            {"l.target_id = i.id AND l.link_type = $2::text"},
		`issue in linkedIssues("ADR-1", supersedes)`:   {"SELECT l.target_id FROM issue_links l WHERE l.link_type = $4::text AND l.source_id IN"},
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

func TestKindsAndLinkTypes(t *testing.T) {
	for _, src := range []string{`kind = chore`, `kind > risk`, `issueLinkType = mitigated`} {
		if _, err := CompileString(src, Env{}, 1, nil); err == nil {
			t.Errorf("%q compiled", src)
		}
	}
	for phrase, want := range map[string]linkDescription{
		"mitigates": {"mitigates", "source"}, "is mitigated by": {"mitigates", "target"},
		"implements": {"implements", "source"}, "is implemented by": {"implements", "target"},
		"supersedes": {"supersedes", "source"}, "is superseded by": {"supersedes", "target"},
	} {
		if got := linkDescriptions[normalize(phrase)]; got != want {
			t.Errorf("%q = %+v, want %+v", phrase, got, want)
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

func TestTimeTrackingFields(t *testing.T) {
	me := uuid.New()
	env := Env{UserID: &me, Now: time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)}
	cases := map[string][]string{
		`originalEstimate > 2h`:              {"i.original_estimate_seconds > $2::bigint"},
		`remainingEstimate is EMPTY`:         {"i.remaining_estimate_seconds IS NULL"},
		`timeestimate = "1d 4h"`:             {"i.remaining_estimate_seconds = $2::bigint"},
		`timeSpent >= 30`:                    {"coalesce((SELECT sum(w.duration_seconds) FROM worklogs w WHERE w.issue_id = i.id), 0) >= $2::bigint"},
		`timeSpent is not EMPTY`:             {"NOT coalesce(((SELECT sum(w.duration_seconds)", "IS NULL)"},
		`worklogAuthor = currentUser()`:      {"EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_id = i.id AND w.author_id IN ($2::uuid))"},
		`worklogAuthor is EMPTY`:             {"NOT EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_id = i.id)"},
		`worklogAuthor != "ada@example.com"`: {"NOT coalesce", "w.author_id IN (SELECT id FROM users"},
		`worklogDate >= -7d`:                 {"EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_id = i.id AND w.started_at >= $2::timestamptz)"},
		`worklogDate = 2026-10-20`:           {"w.started_at >= $2::timestamptz AND w.started_at < $3::timestamptz"},
		`worklogDate != 2026-10-20`:          {"(NOT EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_id = i.id AND"},
		`worklogDate is EMPTY`:               {"NOT EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_id = i.id)"},
	}
	for src, wants := range cases {
		c := mustCompile(t, src, env)
		for _, w := range wants {
			if !strings.Contains(c.Where, w) {
				t.Errorf("%q compiled to\n  %s\nmissing %q", src, c.Where, w)
			}
		}
	}
	// Durations use the working day: 1d is 8 hours unless configured otherwise.
	c := mustCompile(t, `originalEstimate >= 1d AND remainingEstimate < "1w 2d" AND timeSpent > 90`, env)
	if c.Args[0] != int64(8*3600) || c.Args[1] != int64(56*3600) || c.Args[2] != int64(90*60) {
		t.Errorf("duration args = %v", c.Args)
	}
	c = mustCompile(t, `originalEstimate = 1d`, Env{Units: timetrack.Units{HoursPerDay: 6, DaysPerWeek: 4}})
	if c.Args[0] != int64(6*3600) {
		t.Errorf("6-hour day: %v", c.Args)
	}
	c = mustCompile(t, `ORDER BY timeSpent DESC, remainingEstimate`, env)
	if !strings.HasPrefix(c.OrderBy, "(SELECT sum(w.duration_seconds)") || !strings.Contains(c.OrderBy, "i.remaining_estimate_seconds ASC") {
		t.Errorf("order by = %s", c.OrderBy)
	}
	for _, src := range []string{`timeSpent > lots`, `originalEstimate = currentUser()`, `worklogDate > soon`} {
		if _, err := CompileString(src, env, 0, nil); err == nil {
			t.Errorf("%q should fail to compile", src)
		}
	}
}

// Names needn't be unique: a key wins, and a name two fields share asks for the key.
func TestCustomFieldNamesAndKeys(t *testing.T) {
	env := Env{Now: time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), CustomFields: []CustomField{
		{Key: "risk", Name: "Risk score", Type: "number"},
		{Key: "risk_score", Name: "Risk score", Type: "number"},
		{Key: "team", Name: "risk", Type: "text"},
	}}
	if _, err := CompileString(`"Risk score" > 3`, env, 0, nil); err == nil || !strings.Contains(err.Error(), "cf[risk] or cf[risk_score]") {
		t.Fatalf("ambiguous name: err = %v", err)
	}
	for src, key := range map[string]string{`cf[risk_score] > 3`: "risk_score", `risk_score > 3`: "risk_score", `risk > 3`: "risk"} {
		c := mustCompile(t, src, env)
		if !slices.Contains(c.Args, any(key)) {
			t.Errorf("%q: args %v lack the key %q", src, c.Args, key)
		}
	}
}
