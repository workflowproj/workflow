package jiraimport

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/tracker"
	"github.com/geoffo-dev/workflow-management/internal/workflow"
)

// Config is what the admin chose in the wizard: projects and their targets, and how Jira
// values map onto values here.
type Config struct {
	Projects []ProjectTarget `json:"projects"`
	// Jira priority id → highest | high | medium | low | lowest.
	Priorities map[string]string `json:"priorities"`
	// The number field holding story points; "" = detect, "none" = do not import points.
	StoryPointsField string `json:"storyPointsField"`
	// Jira person id → how they are represented here (default: auto).
	People map[string]PersonMapping `json:"people"`
	// Import components as labels ("component:<name>").
	ComponentsAsLabels bool `json:"componentsAsLabels"`
	// Download attachments (up to WFM_ATTACHMENT_MAX_MB each).
	Attachments bool `json:"attachments"`
}

// DefaultConfig is the configuration of a new import.
func DefaultConfig() Config {
	return Config{Projects: []ProjectTarget{}, Priorities: map[string]string{}, People: map[string]PersonMapping{}, ComponentsAsLabels: true, Attachments: true}
}

// ProjectTarget is one Jira project and the project it becomes.
type ProjectTarget struct {
	JiraID   string `json:"jiraId"`
	JiraKey  string `json:"jiraKey"`
	JiraName string `json:"jiraName"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	// Template of the new project: scrum | kanban | bug_tracking; "" = from the Jira boards.
	Template string `json:"template"`
	// Open to every organisation member (member role); otherwise private.
	Open bool `json:"open"`
	// Jira status id → status name here.
	Statuses map[string]string `json:"statuses"`
	// Jira issue type id → issue type key here.
	Types map[string]string `json:"types"`
}

// Person mapping actions.
const (
	PersonAuto        = "auto"        // existing user with the email, else a placeholder
	PersonUser        = "user"        // a chosen existing user
	PersonPlaceholder = "placeholder" // always a placeholder (optionally with another email)
	PersonSkip        = "skip"        // leave assignee/reporter/author empty
)

type PersonMapping struct {
	Action string     `json:"action"`
	UserID *uuid.UUID `json:"userId,omitempty"`
	Email  string     `json:"email,omitempty"`
}

var priorityNames = []string{"highest", "high", "medium", "low", "lowest"}

// defaultPriority maps Jira's default and classic priority schemes.
func defaultPriority(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if slices.Contains(priorityNames, n) {
		return n
	}
	switch n {
	case "blocker", "critical", "urgent", "p1":
		return "highest"
	case "major", "p2":
		return "high"
	case "minor", "p4":
		return "low"
	case "trivial", "p5":
		return "lowest"
	}
	return "medium"
}

var keySanitizer = regexp.MustCompile(`[^A-Z0-9]`)

// defaultKey is the Jira key when it is a valid project key here, else a cleaned-up version.
func defaultKey(jiraKey string) string {
	k := keySanitizer.ReplaceAllString(strings.ToUpper(jiraKey), "")
	for len(k) > 0 && (k[0] < 'A' || k[0] > 'Z') {
		k = k[1:]
	}
	if len(k) > 10 {
		k = k[:10]
	}
	for len(k) < 2 {
		k += "X"
	}
	return k
}

// ---------------------------------------------------------------------------
// Scan results

// Scan is the analysis of the selected projects that pre-fills the mapping and the dry run.
type Scan struct {
	Complete  bool          `json:"complete"`
	ScannedAt *time.Time    `json:"scannedAt,omitempty"`
	Projects  []ScanProject `json:"projects"`
	// Every priority of the site, with how many scanned issues use it.
	Priorities []ValueCount `json:"priorities"`
	People     []ScanPerson `json:"people"`
	// Number fields that may hold story points, with how many issues have a value.
	StoryPointFields []ValueCount `json:"storyPointFields"`
	Oversize         []Oversize   `json:"oversizeAttachments"`
	OversizeCount    int          `json:"oversizeCount"`
}

type ScanProject struct {
	JiraID          string        `json:"jiraId"`
	Key             string        `json:"key"`
	Name            string        `json:"name"`
	BoardType       string        `json:"boardType"`
	Issues          int           `json:"issues"`
	MaxNumber       int           `json:"maxNumber"`
	Comments        int           `json:"comments"`
	Attachments     int           `json:"attachments"`
	AttachmentBytes int64         `json:"attachmentBytes"`
	Sprints         int           `json:"sprints"`
	Versions        int           `json:"versions"`
	Statuses        []StatusCount `json:"statuses"`
	Types           []TypeCount   `json:"types"`
	// Issues per (issue type, status), to tell which workflows lack a mapped status.
	Pairs []PairCount `json:"pairs"`
}

type PairCount struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type ValueCount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type StatusCount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type TypeCount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Level int    `json:"level"`
	Count int    `json:"count"`
}

type ScanPerson struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Active      bool   `json:"active"`
	App         bool   `json:"app"`
	Count       int    `json:"count"`
}

type Oversize struct {
	IssueKey string `json:"issueKey"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

const maxOversizeListed = 100

func (s *Scan) project(jiraID string) *ScanProject {
	for i := range s.Projects {
		if s.Projects[i].JiraID == jiraID {
			return &s.Projects[i]
		}
	}
	return nil
}

func (s *Scan) person(u *User) {
	id := u.ID()
	if id == "" {
		return
	}
	for i := range s.People {
		if s.People[i].ID == id {
			s.People[i].Count++
			if s.People[i].Email == "" && u.EmailAddress != "" {
				s.People[i].Email = strings.ToLower(u.EmailAddress)
			}
			return
		}
	}
	s.People = append(s.People, ScanPerson{
		ID: id, DisplayName: cmpOr(u.DisplayName, u.Name, id), Email: strings.ToLower(u.EmailAddress),
		Active: u.Active == nil || *u.Active, App: u.AccountType == "app", Count: 1,
	})
}

// ---------------------------------------------------------------------------
// Targets: the statuses and issue types a target project offers

type TargetStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type TargetType struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Level int    `json:"level"`
	// Statuses of the type's workflow, by name.
	Statuses []string `json:"statuses"`
}

// Target lists what a project created from a template (or an existing imported project) offers.
type Target struct {
	JiraID string `json:"jiraId"`
	// Key of the project an earlier run created; "" when it will be created.
	ExistingKey string         `json:"existingKey,omitempty"`
	Statuses    []TargetStatus `json:"statuses"`
	Types       []TargetType   `json:"types"`
}

// templateTarget reads a template's workflows and issue types.
func templateTarget(id string) (Target, bool) {
	for _, tpl := range tracker.Templates() {
		if tpl.ID != id {
			continue
		}
		srcs, err := tpl.WorkflowSources()
		if err != nil {
			return Target{}, false
		}
		byWorkflow := map[string][]workflow.StatusDoc{}
		var order []string
		for _, src := range srcs {
			doc, err := workflow.Parse([]byte(src))
			if err != nil {
				continue
			}
			byWorkflow[doc.Name] = doc.Statuses
			order = append(order, doc.Name)
		}
		t := Target{}
		seen := map[string]bool{}
		// Statuses of the standard types' workflow first, so defaults prefer them.
		types := slices.Clone(tpl.IssueTypes)
		slices.SortStableFunc(types, func(a, b tracker.TemplateIssueType) int { return cmp.Compare(absLevel(a.Level), absLevel(b.Level)) })
		addWorkflow := func(name string) {
			for _, st := range byWorkflow[name] {
				if !seen[strings.ToLower(st.Name)] {
					seen[strings.ToLower(st.Name)] = true
					t.Statuses = append(t.Statuses, TargetStatus{Name: st.Name, Category: st.Category})
				}
			}
		}
		for _, it := range types {
			addWorkflow(it.Workflow)
		}
		for _, name := range order {
			addWorkflow(name)
		}
		for _, it := range tpl.IssueTypes {
			tt := TargetType{Key: it.Key, Name: it.Name, Level: it.Level}
			for _, st := range byWorkflow[it.Workflow] {
				tt.Statuses = append(tt.Statuses, st.Name)
			}
			t.Types = append(t.Types, tt)
		}
		sortStatuses(t.Statuses)
		return t, true
	}
	return Target{}, false
}

func absLevel(l int) int {
	if l < 0 {
		return -l * 2
	}
	return l
}

var categoryRank = map[string]int{"todo": 0, "in_progress": 1, "done": 2}

func sortStatuses(list []TargetStatus) {
	slices.SortStableFunc(list, func(a, b TargetStatus) int { return categoryRank[a.Category] - categoryRank[b.Category] })
}

// defaultStatus picks the status here for a Jira status: the same name, else the first status
// of the same category (standard types' workflow first).
func defaultStatus(t Target, name, category string) string {
	for _, st := range t.Statuses {
		if strings.EqualFold(st.Name, name) {
			return st.Name
		}
	}
	for _, st := range t.Statuses {
		if st.Category == category {
			return st.Name
		}
	}
	if len(t.Statuses) > 0 {
		return t.Statuses[0].Name
	}
	return ""
}

// defaultType picks the issue type here for a Jira issue type: by hierarchy for epics and
// sub-tasks, by name for the rest, else Task.
func defaultType(t Target, name string, level int) string {
	if level != 0 {
		for _, tt := range t.Types {
			if tt.Level == level {
				return tt.Key
			}
		}
	}
	for _, tt := range t.Types {
		if tt.Level == level && (strings.EqualFold(tt.Name, name) || strings.EqualFold(tt.Key, name)) {
			return tt.Key
		}
	}
	for _, tt := range t.Types {
		if tt.Key == "task" {
			return tt.Key
		}
	}
	for _, tt := range t.Types {
		if tt.Level == 0 {
			return tt.Key
		}
	}
	return ""
}

// storyPointCandidates are number fields named like story points.
func storyPointCandidates(fields []Field) []Field {
	var out []Field
	for _, f := range fields {
		n := strings.ToLower(f.Name)
		if f.Schema.Type == "number" && (strings.Contains(n, "story point") || n == "story points" || n == "estimate") {
			out = append(out, f)
		}
	}
	// "Story Points" before "Story point estimate" when nothing else decides.
	slices.SortStableFunc(out, func(a, b Field) int { return cmp.Compare(storyPointPreference(a.Name), storyPointPreference(b.Name)) })
	return out
}

func storyPointPreference(name string) int {
	switch strings.ToLower(name) {
	case "story points":
		return 0
	case "story point estimate":
		return 1
	}
	return 2
}

// ApplyDefaults fills mappings the admin has not set from the scan and the targets.
func (c *Config) ApplyDefaults(scan *Scan, targets []Target) {
	if c.Priorities == nil {
		c.Priorities = map[string]string{}
	}
	if c.People == nil {
		c.People = map[string]PersonMapping{}
	}
	for i := range c.Projects {
		p := &c.Projects[i]
		if p.Statuses == nil {
			p.Statuses = map[string]string{}
		}
		if p.Types == nil {
			p.Types = map[string]string{}
		}
		var target Target
		for _, t := range targets {
			if t.JiraID == p.JiraID {
				target = t
			}
		}
		if scan == nil {
			continue
		}
		sp := scan.project(p.JiraID)
		if sp == nil {
			continue
		}
		for _, st := range sp.Statuses {
			if p.Statuses[st.ID] == "" || !hasStatus(target, p.Statuses[st.ID]) {
				p.Statuses[st.ID] = defaultStatus(target, st.Name, st.Category)
			}
		}
		for _, ty := range sp.Types {
			if p.Types[ty.ID] == "" || !hasType(target, p.Types[ty.ID]) {
				p.Types[ty.ID] = defaultType(target, ty.Name, ty.Level)
			}
		}
	}
	if scan == nil {
		return
	}
	for _, pr := range scan.Priorities {
		if !slices.Contains(priorityNames, c.Priorities[pr.ID]) {
			c.Priorities[pr.ID] = defaultPriority(pr.Name)
		}
	}
	if c.StoryPointsField == "" && len(scan.StoryPointFields) > 0 {
		best := scan.StoryPointFields[0]
		for _, f := range scan.StoryPointFields[1:] {
			if f.Count > best.Count {
				best = f
			}
		}
		c.StoryPointsField = best.ID
	}
}

func hasStatus(t Target, name string) bool {
	return slices.ContainsFunc(t.Statuses, func(s TargetStatus) bool { return strings.EqualFold(s.Name, name) })
}

func hasType(t Target, key string) bool {
	return slices.ContainsFunc(t.Types, func(tt TargetType) bool { return tt.Key == key })
}
