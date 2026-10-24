package jiratest

import (
	"fmt"
	"strconv"
	"time"
)

// ADF helpers.

func Doc(blocks ...any) map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": blocks}
}

func Para(inline ...any) map[string]any {
	return map[string]any{"type": "paragraph", "content": inline}
}

func Text(s string, marks ...string) map[string]any {
	m := map[string]any{"type": "text", "text": s}
	if len(marks) > 0 {
		var ms []any
		for _, mk := range marks {
			ms = append(ms, map[string]any{"type": mk})
		}
		m["marks"] = ms
	}
	return m
}

func Mention(id, name string) map[string]any {
	return map[string]any{"type": "mention", "attrs": map[string]any{"id": id, "text": "@" + name}}
}

func Media(alt string) map[string]any {
	return map[string]any{"type": "mediaSingle", "content": []any{map[string]any{"type": "media", "attrs": map[string]any{"type": "file", "id": "media-" + alt, "alt": alt}}}}
}

func lvl(n int) *int { return &n }

func ref(id, key string) map[string]any { return map[string]any{"id": id, "key": key} }

func named(id, name string) map[string]any { return map[string]any{"id": id, "name": name} }

func status(s *Site, id string) map[string]any {
	for _, st := range s.Statuses {
		if st.ID == id {
			return map[string]any{"id": st.ID, "name": st.Name, "statusCategory": map[string]string{"key": st.Category}}
		}
	}
	panic("unknown status " + id)
}

func issueType(p *Project, id string) IssueType {
	for _, t := range p.IssueTypes {
		if t.ID == id {
			return t
		}
	}
	panic("unknown issue type " + id)
}

func statusItem(s *Site, from, to string) Item {
	name := func(id string) *string {
		for _, st := range s.Statuses {
			if st.ID == id {
				return Str(st.Name)
			}
		}
		return nil
	}
	return Item{Field: "status", FieldID: "status", From: Str(from), FromString: name(from), To: Str(to), ToString: name(to)}
}

// Cloud fixture field ids.
const (
	FieldStoryPoints   = "customfield_10028"
	FieldPointEstimate = "customfield_10016"
	FieldSprint        = "customfield_10020"
	FieldRank          = "customfield_10019"
	FieldStartDate     = "customfield_10015"
	FieldEpicLink      = "customfield_10101"
)

// NewCloud is a Jira Cloud site with two projects:
//
//	PLAT (scrum): an epic, stories, a bug carried over between sprints, a sub-task, a
//	"Spike" (an issue type with no counterpart), an issue in the active sprint; a closed, an
//	active and a future sprint; two versions; comments (paged), attachments (one too large),
//	links and a changelog. PLAT-4 does not exist (deleted in Jira).
//	OPS (kanban): two issues linked as duplicates, one related to PLAT-6.
//
// People: Ada (her email is known here), Bob (email visible, no account here yet), Cy (email
// hidden) and an app user. Times are relative to base (the start of the closed sprint).
func NewCloud(base time.Time) *Site {
	base = base.UTC().Truncate(time.Hour)
	at := func(days, hours int) time.Time { return base.Add(time.Duration(days*24+hours) * time.Hour) }
	s := &Site{
		Flavor: Cloud, Email: "importer@example.com", Token: "cloud-api-token",
		PageSize: 2, ChangelogInSearch: 1, CommentsInSearch: 1,
		Content: map[string][]byte{},
		Users: []User{
			{AccountID: "acc-ada", DisplayName: "Ada Lovelace", EmailAddress: "ada@example.com", Active: true, AccountType: "atlassian"},
			{AccountID: "acc-bob", DisplayName: "Bob Builder", EmailAddress: "bob@example.com", Active: true, AccountType: "atlassian"},
			{AccountID: "acc-cy", DisplayName: "Cy Hidden", Active: true, AccountType: "atlassian"},
			{AccountID: "acc-app", DisplayName: "Automation for Jira", Active: true, AccountType: "app"},
		},
		Statuses: []Status{
			{ID: "10000", Name: "To Do", Category: "new"},
			{ID: "10001", Name: "In Progress", Category: "indeterminate"},
			{ID: "10002", Name: "In Review", Category: "indeterminate"},
			{ID: "10003", Name: "Done", Category: "done"},
			{ID: "10004", Name: "Backlog", Category: "new"},
		},
		Priorities: []Priority{{ID: "1", Name: "Highest"}, {ID: "2", Name: "High"}, {ID: "3", Name: "Medium"}, {ID: "4", Name: "Low"}, {ID: "5", Name: "Lowest"}, {ID: "6", Name: "Blocker"}},
		Fields: []Field{
			{ID: "summary", Name: "Summary", Type: "string"},
			{ID: FieldStoryPoints, Name: "Story Points", Custom: true, Type: "number", Schema: "com.atlassian.jira.plugin.system.customfieldtypes:float"},
			{ID: FieldPointEstimate, Name: "Story point estimate", Custom: true, Type: "number", Schema: "com.pyxis.greenhopper.jira:jsw-story-points"},
			{ID: FieldSprint, Name: "Sprint", Custom: true, Type: "array", Schema: "com.pyxis.greenhopper.jira:gh-sprint"},
			{ID: FieldRank, Name: "Rank", Custom: true, Type: "any", Schema: "com.pyxis.greenhopper.jira:gh-lexo-rank"},
			{ID: FieldStartDate, Name: "Start date", Custom: true, Type: "date", Schema: "com.atlassian.jira.plugin.system.customfieldtypes:datepicker"},
		},
		Boards: []Board{{ID: 1, Name: "PLAT board", Type: "scrum", ProjectKey: "PLAT"}, {ID: 2, Name: "OPS board", Type: "kanban", ProjectKey: "OPS"}},
		Sprints: []Sprint{
			{ID: 31, Board: 1, Name: "PLAT Sprint 1", State: "closed", Goal: "Foundations", Start: at(0, 9), End: at(13, 17), CompleteDate: at(13, 17)},
			{ID: 32, Board: 1, Name: "PLAT Sprint 2", State: "active", Goal: "Polish", Start: at(14, 9), End: at(27, 17)},
			{ID: 33, Board: 1, Name: "PLAT Sprint 3", State: "future"},
		},
	}
	plat := &Project{ID: "10000", Key: "PLAT", Name: "Platform", Description: "The platform team", IssueTypes: []IssueType{
		{ID: "10001", Name: "Epic", HierarchyLevel: lvl(1)},
		{ID: "10002", Name: "Story", HierarchyLevel: lvl(0)},
		{ID: "10003", Name: "Task", HierarchyLevel: lvl(0)},
		{ID: "10004", Name: "Bug", HierarchyLevel: lvl(0)},
		{ID: "10005", Name: "Subtask", Subtask: true, HierarchyLevel: lvl(-1)},
		{ID: "10006", Name: "Spike", HierarchyLevel: lvl(0)},
	}, Versions: []Version{
		{ID: "20001", Name: "1.0", Released: true, ReleaseDate: at(13, 0).Format("2006-01-02"), Description: "First release"},
		{ID: "20002", Name: "1.1"},
	}}
	ops := &Project{ID: "10010", Key: "OPS", Name: "Operations", IssueTypes: []IssueType{
		{ID: "10003", Name: "Task", HierarchyLevel: lvl(0)},
		{ID: "10004", Name: "Bug", HierarchyLevel: lvl(0)},
	}}
	s.Projects = []*Project{plat, ops}

	issue := func(p *Project, num int, typeID, statusID, summary string, created time.Time, f map[string]any) *Issue {
		is := &Issue{ID: strconv.Itoa(30000 + len(s.Issues)), Key: fmt.Sprintf("%s-%d", p.Key, num), Fields: map[string]any{
			"summary": summary, "issuetype": issueType(p, typeID), "status": status(s, statusID), "priority": named("3", "Medium"),
			"labels": []string{}, "components": []any{}, "created": Time(created), "updated": Time(created),
			"reporter": s.UserJSON("acc-ada"), "creator": s.UserJSON("acc-ada"), "fixVersions": []any{}, "issuelinks": []any{},
			"attachment": []any{}, FieldRank: fmt.Sprintf("0|i%05d:", 100-num),
		}}
		for k, v := range f {
			is.Fields[k] = v
		}
		s.Issues = append(s.Issues, is)
		return is
	}

	// PLAT-1: an epic in progress.
	issue(plat, 1, "10001", "10001", "Authentication", at(-5, 0), map[string]any{FieldRank: "0|i00010:"})

	// PLAT-2: a story done in the closed sprint; points raised after the sprint started.
	s.Content["40001"] = []byte("\x89PNG fake image bytes")
	p2 := issue(plat, 2, "10002", "10003", "Login page", at(-2, 0), map[string]any{
		"description": Doc(Para(Text("Build the "), Text("login", "strong"), Text(" page. See the mock-up:")), Media("design.png")),
		"parent":      ref("30000", "PLAT-1"), "assignee": s.UserJSON("acc-ada"), "reporter": s.UserJSON("acc-bob"),
		"resolution": named("1", "Done"), "resolutiondate": Time(at(7, 12)), "updated": Time(at(7, 12)),
		FieldStoryPoints: 5.0, FieldSprint: s.SprintField(31), "fixVersions": []any{named("20001", "1.0")},
		"issuelinks": []any{map[string]any{"id": "50001", "type": map[string]string{"name": "Blocks", "inward": "is blocked by", "outward": "blocks"}, "outwardIssue": ref("30002", "PLAT-3")}},
		"attachment": []any{
			map[string]any{"id": "40001", "filename": "design.png", "author": s.UserJSON("acc-bob"), "created": Time(at(-1, 0)), "size": len(s.Content["40001"]), "mimeType": "image/png", "content": "{base}/rest/api/3/attachment/content/40001"},
			map[string]any{"id": "40002", "filename": "recording.mov", "author": s.UserJSON("acc-bob"), "created": Time(at(-1, 0)), "size": 3 << 20, "mimeType": "video/quicktime", "content": "{base}/rest/api/3/attachment/content/40002"},
		},
		"duedate": at(10, 0).Format("2006-01-02"), FieldStartDate: at(0, 0).Format("2006-01-02"),
	})
	p2.Changelog = []History{
		{ID: "100", Author: "acc-bob", Created: at(0, -2), Items: []Item{{Field: "Story Points", FieldType: "custom", FieldID: FieldStoryPoints, ToString: Str("3")}}},
		{ID: "101", Author: "acc-bob", Created: at(0, -1), Items: []Item{{Field: "Sprint", FieldType: "custom", FieldID: FieldSprint, To: Str("31"), ToString: Str("PLAT Sprint 1")}}},
		{ID: "102", Author: "acc-ada", Created: at(1, 10), Items: []Item{statusItem(s, "10000", "10001"),
			{Field: "assignee", FieldID: "assignee", To: Str("acc-ada"), ToString: Str("Ada Lovelace")}}},
		{ID: "103", Author: "acc-bob", Created: at(2, 10), Items: []Item{{Field: "Story Points", FieldType: "custom", FieldID: FieldStoryPoints, From: nil, FromString: Str("3"), ToString: Str("5")}}},
		{ID: "104", Author: "acc-ada", Created: at(4, 15), Items: []Item{statusItem(s, "10001", "10002")}},
		{ID: "105", Author: "acc-app", Created: at(7, 12), Items: []Item{statusItem(s, "10002", "10003"),
			{Field: "resolution", FieldID: "resolution", To: Str("10000"), ToString: Str("Done")}}},
	}
	p2.Comments = []Comment{
		{ID: "60001", Author: "acc-bob", Body: Doc(Para(Mention("acc-ada", "Ada Lovelace"), Text(" can you take this?"))), Created: at(0, 8)},
		{ID: "60002", Author: "acc-ada", Body: Doc(Para(Text("On it."))), Created: at(1, 9), Updated: at(1, 11)},
		{ID: "60003", Author: "acc-cy", Body: Doc(Para(Text("Looks "), Text("great", "em"), Text("."))), Created: at(7, 13)},
	}

	// PLAT-3: a bug carried over from sprint 1 to sprint 2, still in progress.
	p3 := issue(plat, 3, "10004", "10001", "Crash on logout", at(-1, 0), map[string]any{
		"priority": named("6", "Blocker"), "assignee": s.UserJSON("acc-bob"), FieldStoryPoints: 3.0, FieldSprint: s.SprintField(31, 32),
		"updated": Time(at(13, 18)),
	})
	p3.Changelog = []History{
		{ID: "110", Author: "acc-bob", Created: at(0, -1), Items: []Item{{Field: "Sprint", FieldType: "custom", FieldID: FieldSprint, To: Str("31"), ToString: Str("PLAT Sprint 1")}}},
		{ID: "111", Author: "acc-bob", Created: at(9, 9), Items: []Item{statusItem(s, "10000", "10001")}},
		{ID: "112", Author: "acc-app", Created: at(13, 17).Add(5 * time.Second), Items: []Item{{Field: "Sprint", FieldType: "custom", FieldID: FieldSprint, From: Str("31"), FromString: Str("PLAT Sprint 1"), To: Str("31, 32"), ToString: Str("PLAT Sprint 1, PLAT Sprint 2")}}},
	}

	// PLAT-5: a sub-task of PLAT-2, in review (the sub-task workflow has no In Review).
	p5 := issue(plat, 5, "10005", "10002", "Write login tests", at(1, 0), map[string]any{
		"parent": ref(p2.ID, "PLAT-2"), "assignee": s.UserJSON("acc-ada"), FieldSprint: s.SprintField(31), "updated": Time(at(3, 0)),
	})
	p5.Changelog = []History{{ID: "120", Author: "acc-ada", Created: at(3, 0), Items: []Item{statusItem(s, "10000", "10002")}}}

	// PLAT-6: a "Spike" in the backlog with labels, a component and a fix version.
	issue(plat, 6, "10006", "10004", "Evaluate session storage", at(2, 0), map[string]any{
		"labels": []string{"tech debt", "infra"}, "components": []any{named("70001", "API")}, "fixVersions": []any{named("20002", "1.1")},
		"description": Doc(Para(Text("Compare "), Text("Redis", "code"), Text(" and Postgres."))),
		"issuelinks":  []any{map[string]any{"id": "50002", "type": map[string]string{"name": "Relates", "inward": "relates to", "outward": "relates to"}, "outwardIssue": ref("30006", "OPS-1")}},
		"reporter":    nil, "creator": s.UserJSON("acc-cy"),
	})

	// PLAT-7: a story in the active sprint, in review, assigned to Cy (email hidden).
	p7 := issue(plat, 7, "10002", "10002", "Remember me", at(14, 10), map[string]any{
		"assignee": s.UserJSON("acc-cy"), FieldStoryPoints: 2.0, FieldSprint: s.SprintField(32), "updated": Time(at(16, 0)),
	})
	p7.Changelog = []History{
		{ID: "130", Author: "acc-cy", Created: at(15, 9), Items: []Item{statusItem(s, "10000", "10001")}},
		{ID: "131", Author: "acc-cy", Created: at(16, 0), Items: []Item{statusItem(s, "10001", "10002")}},
	}

	// OPS: two issues, OPS-2 duplicates OPS-1.
	issue(ops, 1, "10003", "10003", "Rotate certificates", at(-3, 0), map[string]any{
		"resolution": named("1", "Done"), "resolutiondate": Time(at(1, 0)), "updated": Time(at(1, 0)),
		"reporter": s.UserJSON("acc-app"), "creator": s.UserJSON("acc-app"),
		"issuelinks": []any{map[string]any{"id": "50002", "type": map[string]string{"name": "Relates", "inward": "relates to", "outward": "relates to"}, "inwardIssue": ref("30004", "PLAT-6")}},
	})
	issue(ops, 2, "10004", "10000", "Certificates expired again", at(5, 0), map[string]any{
		"issuelinks": []any{map[string]any{"id": "50003", "type": map[string]string{"name": "Duplicate", "inward": "is duplicated by", "outward": "duplicates"}, "outwardIssue": ref("30006", "OPS-1")}},
	})
	return s
}

// NewDataCenter is a Jira Data Center site with one scrum project, LEG: wiki-markup text,
// user keys, the legacy sprint strings, an Epic Link custom field and a closed sprint.
func NewDataCenter(base time.Time) *Site {
	base = base.UTC().Truncate(time.Hour)
	at := func(days, hours int) time.Time { return base.Add(time.Duration(days*24+hours) * time.Hour) }
	s := &Site{
		Flavor: DataCenter, Token: "dc-personal-access-token", PageSize: 2, Content: map[string][]byte{},
		Users: []User{
			{Key: "JIRAUSER10100", Name: "dora", DisplayName: "Dora Explorer", EmailAddress: "dora@example.com", Active: true},
			{Key: "eve", Name: "eve", DisplayName: "Eve Online", EmailAddress: "eve@example.com", Active: false},
		},
		Statuses: []Status{
			{ID: "1", Name: "Open", Category: "new"},
			{ID: "3", Name: "In Progress", Category: "indeterminate"},
			{ID: "10001", Name: "Done", Category: "done"},
		},
		Priorities: []Priority{{ID: "2", Name: "Critical"}, {ID: "3", Name: "Major"}, {ID: "4", Name: "Minor"}},
		Fields: []Field{
			{ID: "customfield_10002", Name: "Story Points", Custom: true, Type: "number", Schema: "com.atlassian.jira.plugin.system.customfieldtypes:float"},
			{ID: "customfield_10000", Name: "Sprint", Custom: true, Type: "array", Schema: "com.pyxis.greenhopper.jira:gh-sprint"},
			{ID: "customfield_10005", Name: "Rank", Custom: true, Type: "any", Schema: "com.pyxis.greenhopper.jira:gh-lexo-rank"},
			{ID: FieldEpicLink, Name: "Epic Link", Custom: true, Type: "any", Schema: "com.pyxis.greenhopper.jira:gh-epic-link"},
		},
		Boards:  []Board{{ID: 7, Name: "LEG board", Type: "scrum", ProjectKey: "LEG"}},
		Sprints: []Sprint{{ID: 70, Board: 7, Name: "Legacy sprint, part 1", State: "closed", Goal: "Ship it, finally", Start: at(0, 9), End: at(13, 17), CompleteDate: at(13, 17)}},
	}
	leg := &Project{ID: "11000", Key: "LEG", Name: "Legacy", IssueTypes: []IssueType{
		{ID: "6", Name: "Epic"}, {ID: "7", Name: "Story"}, {ID: "5", Name: "Sub-task", Subtask: true},
	}, Versions: []Version{{ID: "21001", Name: "2023.4", Released: true, Archived: true, ReleaseDate: at(-30, 0).Format("2006-01-02")}}}
	s.Projects = []*Project{leg}
	user := func(key string) any { return s.UserJSON(key) }
	add := func(num int, typeID, statusID, summary string, created time.Time, f map[string]any) *Issue {
		is := &Issue{ID: strconv.Itoa(41000 + num), Key: fmt.Sprintf("LEG-%d", num), Fields: map[string]any{
			"summary": summary, "issuetype": issueType(leg, typeID), "status": status(s, statusID), "priority": named("3", "Major"),
			"labels": []string{}, "components": []any{}, "created": Time(created), "updated": Time(created), "reporter": user("JIRAUSER10100"),
			"fixVersions": []any{}, "issuelinks": []any{}, "attachment": []any{}, "customfield_10005": fmt.Sprintf("0|i%05d:", num),
		}}
		for k, v := range f {
			is.Fields[k] = v
		}
		s.Issues = append(s.Issues, is)
		return is
	}
	add(1, "6", "3", "Modernise the stack", at(-2, 0), nil)
	s.Content["90001"] = []byte("log line 1\nlog line 2\n")
	l2 := add(2, "7", "10001", "Upgrade the database", at(-1, 0), map[string]any{
		"description": "h2. Plan\n* *Back up* first\n* run {{migrate}}\n{code:sql}\nALTER TABLE t;\n{code}\nSee [the runbook|https://wiki.example.com/runbook] and !server.log!",
		FieldEpicLink: "LEG-1", "assignee": user("eve"), "customfield_10002": 8.0, "customfield_10000": s.SprintField(70),
		"resolution": named("1", "Fixed"), "resolutiondate": Time(at(5, 0)), "updated": Time(at(5, 0)),
		"fixVersions": []any{named("21001", "2023.4")}, "priority": named("2", "Critical"),
		"attachment": []any{map[string]any{"id": "90001", "filename": "server.log", "author": user("eve"), "created": Time(at(1, 0)), "size": len(s.Content["90001"]), "mimeType": "text/plain", "content": "{base}/secure/attachment/90001/server.log"}},
	})
	l2.Changelog = []History{
		{ID: "500", Author: "JIRAUSER10100", Created: at(1, 9), Items: []Item{statusItem(s, "1", "3"), {Field: "assignee", From: nil, To: Str("eve"), ToString: Str("Eve Online")}}},
		{ID: "501", Author: "eve", Created: at(5, 0), Items: []Item{statusItem(s, "3", "10001"), {Field: "resolution", To: Str("1"), ToString: Str("Fixed")}}},
	}
	l2.Comments = []Comment{{ID: "800", Author: "eve", Body: "Done, see [~dora] _for details_.", Created: at(5, 1)}}
	add(3, "5", "1", "Check replicas", at(1, 0), map[string]any{"parent": ref("41002", "LEG-2"), "customfield_10000": s.SprintField(70)})
	return s
}

// FixContentURLs points attachment content URLs at the running server.
func (s *Site) FixContentURLs(base string) {
	for _, is := range s.Issues {
		list, _ := is.Fields["attachment"].([]any)
		for _, a := range list {
			if m, ok := a.(map[string]any); ok {
				if c, ok := m["content"].(string); ok && len(c) > 6 && c[:6] == "{base}" {
					m["content"] = base + c[6:]
				}
			}
		}
	}
}
