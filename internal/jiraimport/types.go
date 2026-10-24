package jiraimport

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// User is a Jira person: Cloud identifies people by accountId, Data Center by key (and name).
type User struct {
	AccountID    string `json:"accountId,omitempty"`
	Key          string `json:"key,omitempty"`
	Name         string `json:"name,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       *bool  `json:"active,omitempty"`
	AccountType  string `json:"accountType,omitempty"`
}

// ID is the stable identity of the person in their Jira.
func (u *User) ID() string {
	if u == nil {
		return ""
	}
	switch {
	case u.AccountID != "":
		return u.AccountID
	case u.Key != "":
		return u.Key
	}
	return u.Name
}

type Project struct {
	ID             string      `json:"id"`
	Key            string      `json:"key"`
	Name           string      `json:"name"`
	Description    string      `json:"description,omitempty"`
	ProjectTypeKey string      `json:"projectTypeKey,omitempty"`
	Style          string      `json:"style,omitempty"` // classic | next-gen (team-managed)
	Lead           *User       `json:"lead,omitempty"`
	IssueTypes     []IssueType `json:"issueTypes,omitempty"`
}

type IssueType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel *int   `json:"hierarchyLevel,omitempty"`
}

// Level is the hierarchy level: 1 epic, 0 standard, -1 sub-task. Data Center has no
// hierarchyLevel, so epics are recognised by name.
func (t IssueType) Level() int {
	switch {
	case t.Subtask:
		return -1
	case t.HierarchyLevel != nil:
		return min(max(*t.HierarchyLevel, -1), 1)
	case strings.EqualFold(t.Name, "epic"):
		return 1
	}
	return 0
}

type StatusCategory struct {
	Key string `json:"key"` // new | indeterminate | done
}

type Status struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	StatusCategory StatusCategory `json:"statusCategory"`
}

// Category maps Jira's status categories onto todo / in_progress / done.
func (s Status) Category() string { return categoryOf(s.StatusCategory.Key) }

func categoryOf(key string) string {
	switch key {
	case "indeterminate":
		return "in_progress"
	case "done":
		return "done"
	}
	return "todo"
}

type TypeStatuses struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Subtask  bool     `json:"subtask"`
	Statuses []Status `json:"statuses"`
}

type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Field struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
	Schema struct {
		Type   string `json:"type"`
		Custom string `json:"custom"`
	} `json:"schema"`
}

// Custom field types of Jira Software and Jira core.
const (
	sprintFieldType   = "com.pyxis.greenhopper.jira:gh-sprint"
	rankFieldType     = "com.pyxis.greenhopper.jira:gh-lexo-rank"
	epicLinkFieldType = "com.pyxis.greenhopper.jira:gh-epic-link"
)

type Version struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	Released    bool   `json:"released"`
	StartDate   string `json:"startDate"`
	ReleaseDate string `json:"releaseDate"`
}

type Board struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // scrum | kanban | simple
}

type Sprint struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	State         string   `json:"state"` // future | active | closed
	Goal          string   `json:"goal"`
	StartDate     JiraTime `json:"startDate"`
	EndDate       JiraTime `json:"endDate"`
	CompleteDate  JiraTime `json:"completeDate"`
	ActivatedDate JiraTime `json:"activatedDate"`
	OriginBoardID int64    `json:"originBoardId"`
	BoardID       int64    `json:"boardId"`
}

// Started is when the sprint started (activation time when Jira recorded it).
func (s Sprint) Started() time.Time {
	if !s.ActivatedDate.IsZero() {
		return s.ActivatedDate.Time
	}
	return s.StartDate.Time
}

// Issue is a search result; fields are decoded on demand because custom field ids vary.
type Issue struct {
	ID        string                     `json:"id"`
	Key       string                     `json:"key"`
	Fields    map[string]json.RawMessage `json:"fields"`
	Changelog *Changelog                 `json:"changelog,omitempty"`
}

type Changelog struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Histories  []History `json:"histories"`
}

type History struct {
	ID      string        `json:"id"`
	Author  *User         `json:"author,omitempty"`
	Created JiraTime      `json:"created"`
	Items   []HistoryItem `json:"items"`
}

type HistoryItem struct {
	Field      string  `json:"field"`
	FieldType  string  `json:"fieldtype"`
	FieldID    string  `json:"fieldId,omitempty"`
	From       *string `json:"from"`
	FromString *string `json:"fromString"`
	To         *string `json:"to"`
	ToString   *string `json:"toString"`
}

type Comment struct {
	ID      string          `json:"id"`
	Author  *User           `json:"author,omitempty"`
	Body    json.RawMessage `json:"body"`
	Created JiraTime        `json:"created"`
	Updated JiraTime        `json:"updated"`
}

type CommentPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Comments   []Comment `json:"comments"`
}

type Attachment struct {
	ID       string   `json:"id"`
	Filename string   `json:"filename"`
	Author   *User    `json:"author,omitempty"`
	Created  JiraTime `json:"created"`
	Size     int64    `json:"size"`
	MimeType string   `json:"mimeType"`
	Content  string   `json:"content"`
}

type IssueRef struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type IssueLink struct {
	ID   string `json:"id"`
	Type struct {
		Name    string `json:"name"`
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *IssueRef `json:"inwardIssue,omitempty"`
	OutwardIssue *IssueRef `json:"outwardIssue,omitempty"`
}

type named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Fields are the system fields the importer reads.
type Fields struct {
	Summary        string          `json:"summary"`
	Description    json.RawMessage `json:"description"`
	IssueType      IssueType       `json:"issuetype"`
	Status         Status          `json:"status"`
	Priority       *Priority       `json:"priority"`
	Assignee       *User           `json:"assignee"`
	Reporter       *User           `json:"reporter"`
	Creator        *User           `json:"creator"`
	Labels         []string        `json:"labels"`
	Components     []named         `json:"components"`
	DueDate        string          `json:"duedate"`
	Created        JiraTime        `json:"created"`
	Updated        JiraTime        `json:"updated"`
	ResolutionDate JiraTime        `json:"resolutiondate"`
	Resolution     *named          `json:"resolution"`
	Parent         *IssueRef       `json:"parent"`
	FixVersions    []named         `json:"fixVersions"`
	IssueLinks     []IssueLink     `json:"issuelinks"`
	Attachment     []Attachment    `json:"attachment"`
	Comment        *CommentPage    `json:"comment"`
}

// Decode reads the system fields.
func (i Issue) Decode() (Fields, error) {
	var f Fields
	raw, err := json.Marshal(i.Fields)
	if err != nil {
		return f, err
	}
	err = json.Unmarshal(raw, &f)
	return f, err
}

// Number is the issue number from the key (PLAT-123 → 123).
func (i Issue) Number() int {
	n, _ := strconv.Atoi(i.Key[strings.LastIndexByte(i.Key, '-')+1:])
	return n
}

// Float reads a numeric custom field.
func (i Issue) Float(field string) *float64 {
	raw, ok := i.Fields[field]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return &f
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return &v
		}
	}
	return nil
}

// String reads a text custom field (rank, epic link, a date).
func (i Issue) String(field string) string {
	raw, ok := i.Fields[field]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return cmpOr(obj.Key, obj.Value)
	}
	return ""
}

// Sprints reads the sprint field: objects (Cloud, recent Data Center) or the
// "com.atlassian.greenhopper.service.sprint.Sprint@1a2b[id=1,...]" strings of older servers.
func (i Issue) Sprints(field string) []Sprint {
	raw, ok := i.Fields[field]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []Sprint
	for _, it := range items {
		var sp Sprint
		if json.Unmarshal(it, &sp) == nil && sp.ID != 0 {
			out = append(out, sp)
			continue
		}
		var s string
		if json.Unmarshal(it, &s) == nil {
			if sp, ok := parseSprintString(s); ok {
				out = append(out, sp)
			}
		}
	}
	return out
}

var sprintKeys = regexp.MustCompile(`(?:^|,)(id|rapidViewId|state|name|goal|startDate|endDate|completeDate|activatedDate|sequence|synced|autoStartStop|incompleteIssuesDestinationId)=`)

// parseSprintString parses Data Center's legacy sprint string. Names and goals may contain
// commas, so values run until the next known key.
func parseSprintString(s string) (Sprint, bool) {
	open, end := strings.IndexByte(s, '['), strings.LastIndexByte(s, ']')
	if open < 0 || end <= open {
		return Sprint{}, false
	}
	body := s[open+1 : end]
	locs := sprintKeys.FindAllStringSubmatchIndex(body, -1)
	vals := map[string]string{}
	for k, loc := range locs {
		key := body[loc[2]:loc[3]]
		stop := len(body)
		if k+1 < len(locs) {
			stop = locs[k+1][0]
		}
		v := body[loc[1]:stop]
		if v == "<null>" {
			v = ""
		}
		vals[key] = v
	}
	id, err := strconv.ParseInt(vals["id"], 10, 64)
	if err != nil {
		return Sprint{}, false
	}
	board, _ := strconv.ParseInt(vals["rapidViewId"], 10, 64)
	sp := Sprint{ID: id, Name: vals["name"], State: strings.ToLower(vals["state"]), Goal: vals["goal"], OriginBoardID: board}
	sp.StartDate.Time, _ = parseJiraTime(vals["startDate"])
	sp.EndDate.Time, _ = parseJiraTime(vals["endDate"])
	sp.CompleteDate.Time, _ = parseJiraTime(vals["completeDate"])
	sp.ActivatedDate.Time, _ = parseJiraTime(vals["activatedDate"])
	return sp, true
}

// JiraTime parses Jira's timestamps ("2024-01-15T10:30:00.000+0000") and dates.
type JiraTime struct{ time.Time }

func (t *JiraTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil || s == "" {
		return nil
	}
	t.Time, _ = parseJiraTime(s)
	return nil
}

func (t JiraTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.UTC().Format("2006-01-02T15:04:05.000-0700"))
}

var timeLayouts = []string{
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000",
	"2006-01-02",
}

func parseJiraTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	var err error
	for _, l := range timeLayouts {
		var t time.Time
		if t, err = time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, err
}

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
