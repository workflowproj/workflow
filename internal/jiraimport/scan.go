package jiraimport

import (
	"context"
	"slices"
	"strings"
)

const scanPageSize = 100

// scanStep reads metadata, then the issues of each selected project a page at a time, counting
// what the mapping and the dry run need.
func (r *run) scanStep(ctx context.Context) (bool, error) {
	switch r.cur.Phase {
	case "":
		return false, r.scanMetadata(ctx)
	case "issues":
		return r.scanIssues(ctx)
	}
	return true, nil
}

func (r *run) scanMetadata(ctx context.Context) error {
	ph := r.prog.start("metadata")
	ph.Total = len(r.cfg.Projects)
	sc := &Scan{Projects: []ScanProject{}, Priorities: []ValueCount{}, People: []ScanPerson{}, StoryPointFields: []ValueCount{}, Oversize: []Oversize{}}
	fields, err := r.client.Fields(ctx)
	if err != nil {
		return err
	}
	for _, f := range storyPointCandidates(fields) {
		sc.StoryPointFields = append(sc.StoryPointFields, ValueCount{ID: f.ID, Name: f.Name})
	}
	prs, err := r.client.Priorities(ctx)
	if err != nil {
		return err
	}
	for _, p := range prs {
		sc.Priorities = append(sc.Priorities, ValueCount{ID: p.ID, Name: p.Name})
	}
	issues := r.prog.phase("issues")
	issues.Total = 0
	for _, p := range r.cfg.Projects {
		sp := ScanProject{JiraID: p.JiraID, Key: p.JiraKey, Name: p.JiraName, Statuses: []StatusCount{}, Types: []TypeCount{}}
		proj, err := r.client.Project(ctx, p.JiraKey)
		if err != nil {
			return err
		}
		sp.Name = cmpOr(proj.Name, sp.Name)
		for _, t := range proj.IssueTypes {
			sp.Types = append(sp.Types, TypeCount{ID: t.ID, Name: t.Name, Level: t.Level()})
		}
		byType, err := r.client.ProjectStatuses(ctx, p.JiraKey)
		if err != nil {
			return err
		}
		for _, tt := range byType {
			for _, st := range tt.Statuses {
				if !slices.ContainsFunc(sp.Statuses, func(s StatusCount) bool { return s.ID == st.ID }) {
					sp.Statuses = append(sp.Statuses, StatusCount{ID: st.ID, Name: st.Name, Category: st.Category()})
				}
			}
		}
		versions, err := r.client.Versions(ctx, p.JiraKey)
		if err != nil {
			return err
		}
		sp.Versions = len(versions)
		boards, err := r.client.Boards(ctx, p.JiraKey)
		if err != nil {
			return err
		}
		sprints, boardType, err := r.projectSprints(ctx, boards)
		if err != nil {
			return err
		}
		sp.Sprints, sp.BoardType = len(sprints), boardType
		if n, err := r.client.Count(ctx, jqlProject(p.JiraKey, 0)); err == nil {
			issues.Total += n
		}
		sc.Projects = append(sc.Projects, sp)
		ph.Done++
	}
	r.scan, r.scanDirty = sc, true
	r.cur = cursor{Phase: "issues"}
	r.prog.start("issues")
	return nil
}

// projectSprints lists the sprints of a project's scrum boards (sprints created on them).
func (r *run) projectSprints(ctx context.Context, boards []Board) ([]Sprint, string, error) {
	var out []Sprint
	boardType := ""
	ids := map[int64]bool{}
	for _, b := range boards {
		ids[b.ID] = true
		if b.Type == "scrum" {
			boardType = "scrum"
		} else if boardType == "" {
			boardType = "kanban"
		}
	}
	seen := map[int64]bool{}
	for _, b := range boards {
		if b.Type != "scrum" {
			continue
		}
		list, err := r.client.Sprints(ctx, b.ID)
		if err != nil {
			return nil, "", err
		}
		for _, sp := range list {
			origin := sp.OriginBoardID
			if seen[sp.ID] || (origin != 0 && !ids[origin]) {
				continue
			}
			seen[sp.ID] = true
			out = append(out, sp)
		}
	}
	return out, boardType, nil
}

func (r *run) scanIssues(ctx context.Context) (bool, error) {
	if r.cur.Project >= len(r.cfg.Projects) {
		return true, nil
	}
	p := r.cfg.Projects[r.cur.Project]
	sp := r.scan.project(p.JiraID)
	fields := []string{"issuetype", "status", "priority", "assignee", "reporter", "creator", "attachment", "comment"}
	for _, f := range r.scan.StoryPointFields {
		fields = append(fields, f.ID)
	}
	page, err := r.client.Search(ctx, jqlProject(p.JiraKey, r.cur.After), fields, "", "", scanPageSize)
	if err != nil {
		return false, err
	}
	ph := r.prog.phase("issues")
	maxBytes := r.s.Cfg.AttachmentMaxBytes
	for _, is := range page.Issues {
		f, err := is.Decode()
		if err != nil {
			return false, err
		}
		sp.Issues++
		sp.MaxNumber = max(sp.MaxNumber, is.Number())
		if k := slices.IndexFunc(sp.Statuses, func(s StatusCount) bool { return s.ID == f.Status.ID }); k >= 0 {
			sp.Statuses[k].Count++
		} else {
			sp.Statuses = append(sp.Statuses, StatusCount{ID: f.Status.ID, Name: f.Status.Name, Category: f.Status.Category(), Count: 1})
		}
		if k := slices.IndexFunc(sp.Types, func(t TypeCount) bool { return t.ID == f.IssueType.ID }); k >= 0 {
			sp.Types[k].Count++
		} else {
			sp.Types = append(sp.Types, TypeCount{ID: f.IssueType.ID, Name: f.IssueType.Name, Level: f.IssueType.Level(), Count: 1})
		}
		if f.Priority != nil {
			if k := slices.IndexFunc(r.scan.Priorities, func(v ValueCount) bool { return v.ID == f.Priority.ID }); k >= 0 {
				r.scan.Priorities[k].Count++
			} else {
				r.scan.Priorities = append(r.scan.Priorities, ValueCount{ID: f.Priority.ID, Name: f.Priority.Name, Count: 1})
			}
		}
		for _, u := range []*User{f.Assignee, f.Reporter, f.Creator} {
			if u != nil {
				r.scan.person(u)
			}
		}
		if f.Comment != nil {
			sp.Comments += max(f.Comment.Total, len(f.Comment.Comments))
			for _, c := range f.Comment.Comments {
				if c.Author != nil {
					r.scan.person(c.Author)
				}
			}
		}
		for _, a := range f.Attachment {
			sp.Attachments++
			sp.AttachmentBytes += a.Size
			if a.Author != nil {
				r.scan.person(a.Author)
			}
			if a.Size > maxBytes {
				r.scan.OversizeCount++
				if len(r.scan.Oversize) < maxOversizeListed {
					r.scan.Oversize = append(r.scan.Oversize, Oversize{IssueKey: is.Key, Filename: a.Filename, Size: a.Size})
				}
			}
		}
		for k, sf := range r.scan.StoryPointFields {
			if is.Float(sf.ID) != nil {
				r.scan.StoryPointFields[k].Count++
			}
		}
		r.cur.After = is.Number()
		ph.Done++
	}
	r.scanDirty = true
	if page.Last || len(page.Issues) == 0 {
		r.cur.Project++
		r.cur.After = 0
	}
	if r.cur.Project >= len(r.cfg.Projects) {
		slices.SortStableFunc(r.scan.People, func(a, b ScanPerson) int {
			return strings.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName))
		})
		return true, nil
	}
	return false, nil
}
