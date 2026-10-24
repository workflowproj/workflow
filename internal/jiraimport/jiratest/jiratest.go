// Package jiratest is an in-memory Jira for tests: Jira Cloud (REST v3, ADF, account ids,
// token pagination) or Jira Data Center (REST v2, wiki markup, user keys, startAt pagination),
// with the agile API (boards, sprints), paged changelogs and comments, attachments, rate
// limiting and fault injection.
package jiratest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Cloud      = "cloud"
	DataCenter = "datacenter"
)

// Site is a Jira site. Tests build one with NewCloud / NewDataCenter (or by hand) and may
// change it between imports.
type Site struct {
	Flavor string
	Email  string
	Token  string

	Projects   []*Project
	Users      []User
	Statuses   []Status
	Priorities []Priority
	Fields     []Field
	Boards     []Board
	Sprints    []Sprint
	Issues     []*Issue
	// Attachment content by attachment id.
	Content map[string][]byte

	// PageSize caps every page, to exercise pagination.
	PageSize int
	// ChangelogInSearch is how many changelog entries search results embed (the rest must be
	// fetched from the changelog endpoint); CommentsInSearch likewise for comments.
	ChangelogInSearch int
	CommentsInSearch  int

	// Throttle answers this many upcoming search requests with 429 and Retry-After.
	Throttle int
	mu       sync.Mutex
	searches int
	// Outage: searches after failAfter fail with 500, failTimes times.
	failAfter, failTimes int
	// Log of requests (method and path) for assertions.
	Requests []string
	baseURL  string
}

type User struct {
	AccountID    string `json:"accountId,omitempty"`
	Key          string `json:"key,omitempty"`
	Name         string `json:"name,omitempty"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active"`
	AccountType  string `json:"accountType,omitempty"`
}

func (u User) id() string {
	if u.AccountID != "" {
		return u.AccountID
	}
	return u.Key
}

type Status struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"-"` // new | indeterminate | done
}

func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": s.ID, "name": s.Name, "statusCategory": map[string]string{"key": s.Category}})
}

type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Field struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
	Type   string `json:"-"`
	Schema string `json:"-"`
}

func (f Field) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": f.ID, "name": f.Name, "custom": f.Custom, "schema": map[string]string{"type": f.Type, "custom": f.Schema}})
}

type IssueType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel *int   `json:"hierarchyLevel,omitempty"`
}

type Version struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Released    bool   `json:"released"`
	Archived    bool   `json:"archived"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	StartDate   string `json:"startDate,omitempty"`
}

type Project struct {
	ID          string
	Key         string
	Name        string
	Description string
	Type        string
	IssueTypes  []IssueType
	Versions    []Version
}

type Board struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ProjectKey string `json:"-"`
}

type Sprint struct {
	ID           int64
	Board        int64
	Name         string
	State        string
	Goal         string
	Start        time.Time
	End          time.Time
	CompleteDate time.Time
}

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (s Sprint) json() map[string]any {
	m := map[string]any{"id": s.ID, "name": s.Name, "state": s.State, "goal": s.Goal, "originBoardId": s.Board, "boardId": s.Board}
	if !s.Start.IsZero() {
		m["startDate"], m["activatedDate"] = ts(s.Start), ts(s.Start)
	}
	if !s.End.IsZero() {
		m["endDate"] = ts(s.End)
	}
	if !s.CompleteDate.IsZero() {
		m["completeDate"] = ts(s.CompleteDate)
	}
	return m
}

// legacy is the Data Center sprint field string.
func (s Sprint) legacy() string {
	null := func(v string) string {
		if v == "" {
			return "<null>"
		}
		return v
	}
	return fmt.Sprintf("com.atlassian.greenhopper.service.sprint.Sprint@1a2b3c[id=%d,rapidViewId=%d,state=%s,name=%s,goal=%s,startDate=%s,endDate=%s,completeDate=%s,sequence=%d]",
		s.ID, s.Board, strings.ToUpper(s.State), s.Name, s.Goal, null(ts(s.Start)), null(ts(s.End)), null(ts(s.CompleteDate)), s.ID)
}

// Issue is an issue; Fields holds the JSON values of system and custom fields by field id.
type Issue struct {
	ID        string
	Key       string
	Fields    map[string]any
	Changelog []History
	Comments  []Comment
}

type History struct {
	ID      string
	Author  string // user id
	Created time.Time
	Items   []Item
}

type Item struct {
	Field, FieldType, FieldID string
	From, FromString          *string
	To, ToString              *string
}

type Comment struct {
	ID      string
	Author  string
	Body    any // ADF document (Cloud) or wiki string (Data Center)
	Created time.Time
	Updated time.Time
}

// Str is a helper for optional strings in changelog items.
func Str(s string) *string { return &s }

// Time formats a timestamp the way Jira does.
func Time(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000-0700") }

func (s *Site) user(id string) *User {
	for i := range s.Users {
		u := &s.Users[i]
		if u.AccountID == id || u.Key == id || u.Name == id {
			return u
		}
	}
	return nil
}

// UserJSON is how issues and comments embed a person.
func (s *Site) UserJSON(id string) any {
	u := s.user(id)
	if u == nil {
		return nil
	}
	return *u
}

func (s *Site) project(keyOrID string) *Project {
	for _, p := range s.Projects {
		if p.Key == keyOrID || p.ID == keyOrID {
			return p
		}
	}
	return nil
}

func (s *Site) issue(idOrKey string) *Issue {
	for _, i := range s.Issues {
		if i.ID == idOrKey || i.Key == idOrKey {
			return i
		}
	}
	return nil
}

func (s *Site) sprint(id int64) *Sprint {
	for i := range s.Sprints {
		if s.Sprints[i].ID == id {
			return &s.Sprints[i]
		}
	}
	return nil
}

// SprintField renders the sprint field value of sprints with these ids.
func (s *Site) SprintField(ids ...int64) []any {
	var out []any
	for _, id := range ids {
		sp := s.sprint(id)
		if sp == nil {
			continue
		}
		if s.Flavor == Cloud {
			out = append(out, sp.json())
		} else {
			out = append(out, sp.legacy())
		}
	}
	return out
}

// Server serves the site; close it when done.
func (s *Site) Server() *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	s.baseURL = srv.URL
	return srv
}

// FailSearches lets the next `after` searches succeed, then fails `times` searches with 500.
func (s *Site) FailSearches(after, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failAfter, s.failTimes = s.searches+after, times
}

// Lock guards changes to a site while a server is running.
func (s *Site) Lock()   { s.mu.Lock() }
func (s *Site) Unlock() { s.mu.Unlock() }

func (s *Site) api() string {
	if s.Flavor == Cloud {
		return "/rest/api/3"
	}
	return "/rest/api/2"
}

func (s *Site) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if s.Flavor == Cloud {
		return h == "Basic "+base64.StdEncoding.EncodeToString([]byte(s.Email+":"+s.Token))
	}
	return h == "Bearer "+s.Token
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jiraError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"errorMessages": []string{msg}, "errors": map[string]string{}})
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func (s *Site) pageSize(requested int) int {
	n := requested
	if n <= 0 {
		n = 50
	}
	if s.PageSize > 0 {
		n = min(n, s.PageSize)
	}
	return n
}

// page slices a list for startAt/maxResults pagination.
func page[T any](list []T, start, size int) ([]T, bool) {
	if start >= len(list) {
		return []T{}, true
	}
	end := min(start+size, len(list))
	return list[start:end], end >= len(list)
}

var (
	rxProjectStatuses = regexp.MustCompile(`^/rest/api/[23]/project/([^/]+)/statuses$`)
	rxProjectVersions = regexp.MustCompile(`^/rest/api/[23]/project/([^/]+)/versions$`)
	rxProject         = regexp.MustCompile(`^/rest/api/[23]/project/([^/]+)$`)
	rxChangelog       = regexp.MustCompile(`^/rest/api/3/issue/([^/]+)/changelog$`)
	rxComments        = regexp.MustCompile(`^/rest/api/[23]/issue/([^/]+)/comment$`)
	rxIssue           = regexp.MustCompile(`^/rest/api/2/issue/([^/]+)$`)
	rxBoardSprints    = regexp.MustCompile(`^/rest/agile/1.0/board/(\d+)/sprint$`)
	rxSprint          = regexp.MustCompile(`^/rest/agile/1.0/sprint/(\d+)$`)
	rxCloudContent    = regexp.MustCompile(`^/rest/api/3/attachment/content/([^/]+)$`)
	rxDCContent       = regexp.MustCompile(`^/secure/attachment/([^/]+)/`)
)

func (s *Site) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, r.Method+" "+r.URL.Path)
	p := r.URL.Path
	q := r.URL.Query()
	if p == "/rest/api/2/serverInfo" || p == "/rest/api/3/serverInfo" {
		dt := "Cloud"
		if s.Flavor != Cloud {
			dt = "Server"
		}
		writeJSON(w, 200, map[string]any{"baseUrl": s.baseURL, "version": "1001.0.0", "deploymentType": dt, "serverTitle": "Fake Jira"})
		return
	}
	if !s.authorized(r) {
		jiraError(w, 401, "You are not authenticated.")
		return
	}
	if strings.HasPrefix(p, "/rest/api/") && !strings.HasPrefix(p, s.api()) {
		jiraError(w, 404, "wrong API version for this site")
		return
	}
	switch {
	case p == s.api()+"/myself":
		if s.Flavor == Cloud {
			writeJSON(w, 200, map[string]any{"accountId": "admin", "displayName": "Jira Admin", "emailAddress": s.Email})
		} else {
			writeJSON(w, 200, map[string]any{"key": "admin", "name": "admin", "displayName": "Jira Admin", "emailAddress": "admin@example.com"})
		}
	case p == s.api()+"/user":
		id := cmpOr(q.Get("accountId"), q.Get("key"), q.Get("username"))
		if u := s.user(id); u != nil {
			writeJSON(w, 200, u)
			return
		}
		jiraError(w, 404, "user not found")
	case p == "/rest/api/3/project/search":
		start, size := atoi(q.Get("startAt"), 0), s.pageSize(atoi(q.Get("maxResults"), 50))
		list, last := page(s.Projects, start, size)
		values := []any{}
		for _, pr := range list {
			values = append(values, s.projectJSON(pr, false))
		}
		writeJSON(w, 200, map[string]any{"values": values, "startAt": start, "maxResults": size, "total": len(s.Projects), "isLast": last})
	case p == "/rest/api/2/project":
		values := []any{}
		for _, pr := range s.Projects {
			values = append(values, s.projectJSON(pr, false))
		}
		writeJSON(w, 200, values)
	case rxProjectStatuses.MatchString(p):
		pr := s.project(rxProjectStatuses.FindStringSubmatch(p)[1])
		if pr == nil {
			jiraError(w, 404, "No project could be found")
			return
		}
		out := []any{}
		for _, it := range pr.IssueTypes {
			out = append(out, map[string]any{"id": it.ID, "name": it.Name, "subtask": it.Subtask, "statuses": s.Statuses})
		}
		writeJSON(w, 200, out)
	case rxProjectVersions.MatchString(p):
		pr := s.project(rxProjectVersions.FindStringSubmatch(p)[1])
		if pr == nil {
			jiraError(w, 404, "No project could be found")
			return
		}
		writeJSON(w, 200, append([]Version{}, pr.Versions...))
	case rxProject.MatchString(p):
		pr := s.project(rxProject.FindStringSubmatch(p)[1])
		if pr == nil {
			jiraError(w, 404, "No project could be found")
			return
		}
		writeJSON(w, 200, s.projectJSON(pr, true))
	case p == s.api()+"/status":
		writeJSON(w, 200, s.Statuses)
	case p == s.api()+"/priority":
		writeJSON(w, 200, s.Priorities)
	case p == s.api()+"/field":
		writeJSON(w, 200, s.Fields)
	case p == "/rest/agile/1.0/board":
		var list []Board
		for _, b := range s.Boards {
			if b.ProjectKey == q.Get("projectKeyOrId") || q.Get("projectKeyOrId") == "" {
				list = append(list, b)
			}
		}
		start, size := atoi(q.Get("startAt"), 0), s.pageSize(atoi(q.Get("maxResults"), 50))
		values, last := page(list, start, size)
		writeJSON(w, 200, map[string]any{"values": values, "startAt": start, "maxResults": size, "isLast": last})
	case rxBoardSprints.MatchString(p):
		id, _ := strconv.ParseInt(rxBoardSprints.FindStringSubmatch(p)[1], 10, 64)
		idx := slices.IndexFunc(s.Boards, func(b Board) bool { return b.ID == id })
		if idx < 0 {
			jiraError(w, 404, "board not found")
			return
		}
		if s.Boards[idx].Type != "scrum" {
			jiraError(w, 400, "The board does not support sprints")
			return
		}
		var list []any
		for _, sp := range s.Sprints {
			if sp.Board == id {
				list = append(list, sp.json())
			}
		}
		start, size := atoi(q.Get("startAt"), 0), s.pageSize(atoi(q.Get("maxResults"), 50))
		values, last := page(list, start, size)
		writeJSON(w, 200, map[string]any{"values": values, "startAt": start, "maxResults": size, "isLast": last})
	case rxSprint.MatchString(p):
		id, _ := strconv.ParseInt(rxSprint.FindStringSubmatch(p)[1], 10, 64)
		if sp := s.sprint(id); sp != nil {
			writeJSON(w, 200, sp.json())
			return
		}
		jiraError(w, 404, "sprint not found")
	case p == "/rest/api/3/search/jql" || p == "/rest/api/2/search":
		s.search(w, r)
	case p == "/rest/api/3/search/approximate-count":
		var body struct {
			JQL string `json:"jql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		list, err := s.query(body.JQL)
		if err != nil {
			jiraError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]int{"count": len(list)})
	case rxChangelog.MatchString(p):
		is := s.issue(rxChangelog.FindStringSubmatch(p)[1])
		if is == nil {
			jiraError(w, 404, "Issue does not exist")
			return
		}
		start, size := atoi(q.Get("startAt"), 0), s.pageSize(atoi(q.Get("maxResults"), 100))
		values, last := page(s.histories(is), start, size)
		writeJSON(w, 200, map[string]any{"values": values, "startAt": start, "maxResults": size, "total": len(is.Changelog), "isLast": last})
	case rxComments.MatchString(p):
		is := s.issue(rxComments.FindStringSubmatch(p)[1])
		if is == nil {
			jiraError(w, 404, "Issue does not exist")
			return
		}
		start, size := atoi(q.Get("startAt"), 0), s.pageSize(atoi(q.Get("maxResults"), 50))
		values, _ := page(s.comments(is), start, size)
		writeJSON(w, 200, map[string]any{"comments": values, "startAt": start, "maxResults": size, "total": len(is.Comments)})
	case rxIssue.MatchString(p):
		is := s.issue(rxIssue.FindStringSubmatch(p)[1])
		if is == nil {
			jiraError(w, 404, "Issue does not exist")
			return
		}
		h := s.histories(is)
		writeJSON(w, 200, map[string]any{"id": is.ID, "key": is.Key, "fields": map[string]any{},
			"changelog": map[string]any{"startAt": 0, "maxResults": len(h), "total": len(h), "histories": h}})
	case rxCloudContent.MatchString(p) && s.Flavor == Cloud:
		s.content(w, rxCloudContent.FindStringSubmatch(p)[1])
	case rxDCContent.MatchString(p) && s.Flavor != Cloud:
		s.content(w, rxDCContent.FindStringSubmatch(p)[1])
	default:
		jiraError(w, 404, "no such endpoint in the fake: "+p)
	}
}

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Site) content(w http.ResponseWriter, id string) {
	b, ok := s.Content[id]
	if !ok {
		jiraError(w, 404, "attachment not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(b)
}

func (s *Site) projectJSON(p *Project, full bool) map[string]any {
	m := map[string]any{"id": p.ID, "key": p.Key, "name": p.Name, "projectTypeKey": cmpOr(p.Type, "software"), "style": "classic"}
	if full {
		m["issueTypes"] = p.IssueTypes
		m["description"] = p.Description
	}
	return m
}

func (s *Site) histories(is *Issue) []any {
	out := []any{}
	for _, h := range is.Changelog {
		items := []any{}
		for _, it := range h.Items {
			m := map[string]any{"field": it.Field, "fieldtype": cmpOr(it.FieldType, "jira"), "from": it.From, "fromString": it.FromString, "to": it.To, "toString": it.ToString}
			if s.Flavor == Cloud && it.FieldID != "" {
				m["fieldId"] = it.FieldID
			}
			items = append(items, m)
		}
		out = append(out, map[string]any{"id": h.ID, "author": s.UserJSON(h.Author), "created": Time(h.Created), "items": items})
	}
	return out
}

func (s *Site) comments(is *Issue) []any {
	out := []any{}
	for _, c := range is.Comments {
		upd := c.Updated
		if upd.IsZero() {
			upd = c.Created
		}
		out = append(out, map[string]any{"id": c.ID, "author": s.UserJSON(c.Author), "body": c.Body, "created": Time(c.Created), "updated": Time(upd)})
	}
	return out
}

var (
	rxJQL      = regexp.MustCompile(`^project = "?([A-Za-z0-9_]+)"?(?: AND key > "?([A-Za-z0-9_]+)-(\d+)"?)? ORDER BY key ASC$`)
	errBadJQL  = fmt.Errorf("the fake only understands: project = \"KEY\" [AND key > \"KEY-N\"] ORDER BY key ASC")
	keyNumber  = func(key string) int { return atoi(key[strings.LastIndexByte(key, '-')+1:], 0) }
	keyProject = func(key string) string { return key[:strings.LastIndexByte(key, '-')] }
)

func (s *Site) query(jql string) ([]*Issue, error) {
	m := rxJQL.FindStringSubmatch(strings.TrimSpace(jql))
	if m == nil {
		return nil, errBadJQL
	}
	pr := s.project(m[1])
	if pr == nil {
		return nil, fmt.Errorf("the value '%s' does not exist for the field 'project'", m[1])
	}
	after := atoi(m[3], 0)
	var out []*Issue
	for _, is := range s.Issues {
		if keyProject(is.Key) == pr.Key && keyNumber(is.Key) > after {
			out = append(out, is)
		}
	}
	slices.SortFunc(out, func(a, b *Issue) int { return keyNumber(a.Key) - keyNumber(b.Key) })
	return out, nil
}

func (s *Site) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if s.Throttle > 0 {
		s.Throttle--
		w.Header().Set("Retry-After", "2")
		jiraError(w, 429, "Rate limit exceeded")
		return
	}
	if q.Get("maxResults") != "0" {
		s.searches++
		if s.failTimes > 0 && s.searches > s.failAfter {
			s.failTimes--
			jiraError(w, 500, "simulated outage")
			return
		}
	}
	list, err := s.query(q.Get("jql"))
	if err != nil {
		jiraError(w, 400, err.Error())
		return
	}
	size := s.pageSize(atoi(q.Get("maxResults"), 50))
	start := atoi(q.Get("startAt"), 0)
	if tok := q.Get("nextPageToken"); tok != "" {
		start = atoi(tok, 0)
	}
	if s.Flavor != Cloud && q.Get("maxResults") == "0" {
		writeJSON(w, 200, map[string]any{"startAt": 0, "maxResults": 0, "total": len(list), "issues": []any{}})
		return
	}
	pageList, last := page(list, start, size)
	var fields []string
	if f := q.Get("fields"); f != "" {
		fields = strings.Split(f, ",")
	}
	issues := []any{}
	for _, is := range pageList {
		issues = append(issues, s.issueJSON(is, fields, strings.Contains(q.Get("expand"), "changelog")))
	}
	if s.Flavor == Cloud {
		out := map[string]any{"issues": issues, "isLast": last}
		if !last {
			out["nextPageToken"] = strconv.Itoa(start + len(pageList))
		}
		writeJSON(w, 200, out)
		return
	}
	writeJSON(w, 200, map[string]any{"startAt": start, "maxResults": size, "total": len(list), "issues": issues})
}

func (s *Site) issueJSON(is *Issue, fields []string, changelog bool) map[string]any {
	f := map[string]any{}
	for k, v := range is.Fields {
		if len(fields) == 0 || slices.Contains(fields, k) {
			f[k] = v
		}
	}
	if len(fields) == 0 || slices.Contains(fields, "comment") {
		all := s.comments(is)
		n := len(all)
		if s.CommentsInSearch > 0 {
			n = min(n, s.CommentsInSearch)
		}
		f["comment"] = map[string]any{"comments": all[:n], "startAt": 0, "maxResults": n, "total": len(all)}
	}
	out := map[string]any{"id": is.ID, "key": is.Key, "fields": f}
	if changelog {
		h := s.histories(is)
		n := len(h)
		if s.ChangelogInSearch > 0 {
			n = min(n, s.ChangelogInSearch)
		}
		out["changelog"] = map[string]any{"startAt": 0, "maxResults": n, "total": len(h), "histories": h[:n]}
	}
	return out
}
