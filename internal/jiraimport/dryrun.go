package jiraimport

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

// DryRun is what an import would do, from the scan and the mapping, without writing anything.
type DryRun struct {
	Ready    bool            `json:"ready"`
	Projects []DryRunProject `json:"projects"`
	People   DryRunPeople    `json:"people"`
	Problems []Problem       `json:"problems"`
}

type DryRunProject struct {
	JiraKey  string `json:"jiraKey"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Template string `json:"template"`
	// The project exists from an earlier run and is updated.
	Existing           bool  `json:"existing"`
	Issues             int   `json:"issues"`
	Comments           int   `json:"comments"`
	Attachments        int   `json:"attachments"`
	AttachmentBytes    int64 `json:"attachmentBytes"`
	SkippedAttachments int   `json:"skippedAttachments"`
	Sprints            int   `json:"sprints"`
	Versions           int   `json:"versions"`
}

type DryRunPeople struct {
	// Existing accounts (same email, chosen, or mapped by an earlier run).
	Matched int `json:"matched"`
	// New placeholders that their owner can claim by signing in with that email.
	Placeholders int `json:"placeholders"`
	// New placeholders without a usable email: nobody can claim them.
	Unclaimable int `json:"unclaimable"`
	Skipped     int `json:"skipped"`
}

func (s *Service) DryRun(ctx context.Context, actor *authz.Actor, id uuid.UUID) (DryRun, error) {
	imp, err := s.load(ctx, actor, id)
	if err != nil {
		return DryRun{}, err
	}
	return s.dryRun(ctx, imp)
}

func (s *Service) dryRun(ctx context.Context, imp dbq.JiraImport) (DryRun, error) {
	out := DryRun{Projects: []DryRunProject{}}
	prog := newProgress(nil)
	cfg := decodeConfig(imp)
	sc := decodeScan(imp)
	if imp.TokenEnc == nil {
		prog.add("error", "credentials", "", "The Jira credentials were deleted: enter a token again.", "")
	}
	if len(cfg.Projects) == 0 {
		prog.add("error", "projects", "", "Choose at least one project.", "")
	}
	if scanStale(cfg, sc) {
		prog.add("error", "scan", "", "Analyse the selected projects first (the scan is missing or does not cover every selected project).", "")
		out.Problems = prog.Problems
		return out, nil
	}
	targets, err := s.targets(ctx, imp, cfg)
	if err != nil {
		return out, err
	}
	cfg.ApplyDefaults(sc, targets)
	maxBytes := s.Cfg.AttachmentMaxBytes
	keys := map[string]string{}
	for i, p := range cfg.Projects {
		t := targets[i]
		sp := sc.project(p.JiraID)
		dp := DryRunProject{JiraKey: p.JiraKey, Key: p.Key, Name: p.Name, Template: p.Template, Existing: t.ExistingKey != "",
			Issues: sp.Issues, Comments: sp.Comments, Attachments: sp.Attachments, AttachmentBytes: sp.AttachmentBytes,
			Sprints: sp.Sprints, Versions: sp.Versions}
		if dp.Existing {
			dp.Key = t.ExistingKey
		}
		if other, ok := keys[dp.Key]; ok {
			prog.add("error", "key", p.JiraKey, fmt.Sprintf("%s and %s would both become %s.", other, p.JiraKey, dp.Key), "")
		}
		keys[dp.Key] = p.JiraKey
		if !dp.Existing {
			if used, err := s.keyUsed(ctx, dp.Key); err != nil {
				return out, err
			} else if used {
				prog.add("error", "key", p.JiraKey, fmt.Sprintf("The key %s is already used by a project here; choose another key for %s.", dp.Key, p.JiraKey), "")
			}
		} else if n, err := s.nativeNumberClashes(ctx, imp.Site, dp.Key, sp.MaxNumber); err != nil {
			return out, err
		} else if n > 0 {
			prog.add("warning", "numbers", p.JiraKey, fmt.Sprintf("%d issue(s) created in %s since the last import use numbers Jira also uses; those Jira issues are skipped.", n, dp.Key), "")
		}
		if dp.Key != p.JiraKey {
			prog.add("info", "key", p.JiraKey, fmt.Sprintf("Issues keep their numbers but move to the key %s (%s-12 becomes %s-12); the old key keeps resolving.", dp.Key, p.JiraKey, dp.Key), "")
		}
		// Statuses: mapped to a status the target has, and in the workflow of each type using it.
		for _, st := range sp.Statuses {
			if target := p.Statuses[st.ID]; target == "" || !hasStatus(t, target) {
				prog.add("warning", "status", p.JiraKey, fmt.Sprintf("Status %q is not mapped; its issues use the first %s status.", st.Name, categoryLabel(st.Category)), "")
			}
		}
		for _, pair := range sp.Pairs {
			target := p.Statuses[pair.Status]
			tt := targetType(t, p.Types[pair.Type])
			if target == "" || tt == nil || slices.ContainsFunc(tt.Statuses, func(n string) bool { return strings.EqualFold(n, target) }) {
				continue
			}
			jiraStatus := pair.Status
			for _, st := range sp.Statuses {
				if st.ID == pair.Status {
					jiraStatus = st.Name
				}
			}
			prog.add("info", "status", p.JiraKey, fmt.Sprintf("The %s workflow has no %q: %d %s issue(s) in Jira's %q become %q.",
				tt.Name, target, pair.Count, tt.Name, jiraStatus, fallbackStatus(t, *tt, target)), "")
		}
		levels := map[int]bool{}
		for _, tt := range t.Types {
			levels[tt.Level] = true
		}
		for _, ty := range sp.Types {
			if ty.Count == 0 {
				continue
			}
			key := p.Types[ty.ID]
			tt := targetType(t, key)
			switch {
			case tt == nil:
				prog.add("warning", "type", p.JiraKey, fmt.Sprintf("Issue type %q is not mapped; its %d issue(s) become tasks.", ty.Name, ty.Count), "")
			case tt.Level != ty.Level:
				prog.add("warning", "type", p.JiraKey, fmt.Sprintf("%q (level %d) maps to %s (level %d): parent links that break the epic → story → sub-task hierarchy are dropped.", ty.Name, ty.Level, tt.Name, tt.Level), "")
			}
		}
		if sp.Sprints > 0 && p.Template != "scrum" && !dp.Existing {
			prog.add("info", "sprints", p.JiraKey, fmt.Sprintf("%d sprint(s) are imported, but %s boards do not show sprints.", sp.Sprints, templateName(p.Template)), "")
		}
		for _, o := range sc.Oversize {
			if strings.HasPrefix(o.IssueKey, p.JiraKey+"-") {
				dp.SkippedAttachments++
			}
		}
		if !cfg.Attachments {
			dp.SkippedAttachments = dp.Attachments
		}
		out.Projects = append(out.Projects, dp)
	}
	if sc.OversizeCount > 0 && cfg.Attachments {
		ex := make([]string, 0, maxExamples)
		for _, o := range sc.Oversize {
			if len(ex) < maxExamples {
				ex = append(ex, fmt.Sprintf("%s %s (%s)", o.IssueKey, o.Filename, humanBytes(o.Size)))
			}
		}
		out.Problems = append(out.Problems, Problem{Severity: "warning", Kind: "attachments", Count: sc.OversizeCount, Examples: ex,
			Message: fmt.Sprintf("%d attachment(s) are larger than the %s limit (WFM_ATTACHMENT_MAX_MB) and are skipped.", sc.OversizeCount, humanBytes(maxBytes))})
	}
	if !cfg.Attachments {
		prog.add("info", "attachments", "", "Attachments are not downloaded (turned off).", "")
	}
	for _, pr := range sc.Priorities {
		if pr.Count > 0 && !slices.Contains(priorityNames, cfg.Priorities[pr.ID]) {
			prog.add("info", "priority", "", fmt.Sprintf("Priority %q is not mapped; it becomes Medium.", pr.Name), "")
		}
	}
	switch {
	case cfg.StoryPointsField == "none":
		prog.add("info", "points", "", "Story points are not imported.", "")
	case cfg.StoryPointsField == "":
		prog.add("warning", "points", "", "No story points field was found; story points are not imported.", "")
	}
	// People.
	people, err := s.people(ctx, imp, sc.People)
	if err != nil {
		return out, err
	}
	var noEmail []string
	for _, p := range people {
		pm := cfg.People[p.ID]
		switch {
		case pm.Action == PersonSkip:
			out.People.Skipped++
		case pm.Action == PersonUser:
			out.People.Matched++
		case pm.Action == PersonPlaceholder:
			if pm.Email != "" || p.Email != "" {
				out.People.Placeholders++
			} else {
				out.People.Unclaimable++
				noEmail = append(noEmail, p.DisplayName)
			}
		case p.Match != nil:
			out.People.Matched++
		case p.Email != "":
			out.People.Placeholders++
		default:
			out.People.Unclaimable++
			noEmail = append(noEmail, p.DisplayName)
		}
	}
	if len(noEmail) > 0 {
		out.Problems = append(out.Problems, Problem{Severity: "warning", Kind: "people", Count: len(noEmail), Examples: noEmail[:min(len(noEmail), maxExamples)],
			Message: fmt.Sprintf("%d person(s) have no email visible to the import account (Jira Cloud hides emails unless people share them). They become placeholders nobody can claim; map them to an account or enter their email.", len(noEmail))})
	}
	out.Problems = append(prog.Problems, out.Problems...)
	slices.SortStableFunc(out.Problems, func(a, b Problem) int { return severityRank[a.Severity] - severityRank[b.Severity] })
	out.Ready = !hasErrors(out.Problems)
	return out, nil
}

var severityRank = map[string]int{"error": 0, "warning": 1, "info": 2}

func categoryLabel(c string) string {
	return map[string]string{"todo": "to-do", "in_progress": "in-progress", "done": "done"}[c]
}

func templateName(id string) string {
	return map[string]string{"scrum": "Scrum", "kanban": "Kanban", "bug_tracking": "Bug tracking"}[id]
}

func targetType(t Target, key string) *TargetType {
	for i := range t.Types {
		if t.Types[i].Key == key {
			return &t.Types[i]
		}
	}
	return nil
}

// fallbackStatus is the status a type's workflow uses for a status it lacks: the first with
// the same category.
func fallbackStatus(t Target, tt TargetType, name string) string {
	cat := ""
	for _, st := range t.Statuses {
		if strings.EqualFold(st.Name, name) {
			cat = st.Category
		}
	}
	for _, n := range tt.Statuses {
		for _, st := range t.Statuses {
			if strings.EqualFold(st.Name, n) && st.Category == cat {
				return n
			}
		}
	}
	if len(tt.Statuses) > 0 {
		return tt.Statuses[0]
	}
	return name
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// nativeNumberClashes counts issues in an imported project that the import did not create and
// whose number Jira may also use.
func (s *Service) nativeNumberClashes(ctx context.Context, site, key string, maxNumber int) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM issues i JOIN projects p ON p.id = i.project_id
		WHERE p.key = $1 AND i.number <= $2
		AND NOT EXISTS (SELECT 1 FROM jira_import_map m WHERE m.site = $3 AND m.kind = 'issue' AND m.local_id = i.id)`, key, maxNumber, site).Scan(&n)
	return n, err
}
