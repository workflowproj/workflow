package httpapi

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
	"github.com/geoffo-dev/workflow-management/internal/workflow"
)

func (s *Server) ListProjectTemplates(ctx context.Context, _ api.ListProjectTemplatesRequestObject) (api.ListProjectTemplatesResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	out := api.ListProjectTemplates200JSONResponse{}
	for _, t := range tracker.Templates() {
		v := api.ProjectTemplate{Id: t.ID, Name: t.Name, Description: t.Description, BoardType: api.BoardType(t.BoardType), IssueTypes: []api.TemplateIssueType{}, Workflows: []string{}}
		for _, it := range t.IssueTypes {
			v.IssueTypes = append(v.IssueTypes, api.TemplateIssueType{Key: it.Key, Name: it.Name, Level: it.Level})
		}
		srcs, _ := t.WorkflowSources()
		for _, src := range srcs {
			if doc, err := workflow.Parse([]byte(src)); err == nil {
				v.Workflows = append(v.Workflows, doc.Name)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) ListProjects(ctx context.Context, _ api.ListProjectsRequestObject) (api.ListProjectsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := s.Tracker.ListProjects(ctx, a)
	if err != nil {
		return nil, err
	}
	out := api.ListProjects200JSONResponse{}
	for _, p := range ps {
		v, err := s.Tracker.ProjectView(ctx, p.Project, p.Access)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) GetProject(ctx context.Context, req api.GetProjectRequestObject) (api.GetProjectResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p, access, err := s.Tracker.ProjectFor(ctx, a, req.ProjectKey, "")
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.ProjectView(ctx, p, access)
	return api.GetProject200JSONResponse(v), err
}

func (s *Server) CreateProject(ctx context.Context, req api.CreateProjectRequestObject) (api.CreateProjectResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	openRole := "member"
	if b.OpenRoleKey != nil {
		openRole = *b.OpenRoleKey
	}
	if b.Access != nil && *b.Access == api.ProjectAccessPrivate {
		openRole = ""
	}
	p, err := s.Tracker.CreateProject(ctx, a, tracker.ProjectInput{Key: b.Key, Name: b.Name, Description: deref(b.Description), Template: b.Template, OpenRoleKey: openRole})
	if err != nil {
		return nil, err
	}
	_, access, err := s.Tracker.ProjectFor(ctx, a, p.Key, "")
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.ProjectView(ctx, p, access)
	return api.CreateProject201JSONResponse(v), err
}

func (s *Server) UpdateProject(ctx context.Context, req api.UpdateProjectRequestObject) (api.UpdateProjectResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	patch := tracker.ProjectPatch{Key: b.Key, Name: b.Name, Description: b.Description}
	if b.LeadId.IsSpecified() {
		var lead *uuid.UUID
		if !b.LeadId.IsNull() {
			v := b.LeadId.MustGet()
			lead = &v
		}
		patch.LeadID = &lead
	}
	switch {
	case b.Access != nil && *b.Access == api.ProjectAccessPrivate:
		patch.OpenRoleKey = ptr("")
	case b.OpenRoleKey != nil:
		patch.OpenRoleKey = b.OpenRoleKey
	case b.Access != nil && *b.Access == api.ProjectAccessOpen:
		patch.OpenRoleKey = ptr("member")
	}
	p, err := s.Tracker.UpdateProject(ctx, a, req.ProjectKey, patch)
	if err != nil {
		return nil, err
	}
	_, access, err := s.Tracker.ProjectFor(ctx, a, p.ID.String(), "")
	if err != nil {
		return nil, err
	}
	v, err := s.Tracker.ProjectView(ctx, p, access)
	return api.UpdateProject200JSONResponse(v), err
}

func (s *Server) DeleteProject(ctx context.Context, req api.DeleteProjectRequestObject) (api.DeleteProjectResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteProject(ctx, a, req.ProjectKey); err != nil {
		return nil, err
	}
	return api.DeleteProject204Response{}, nil
}

func (s *Server) memberView(ctx context.Context, id, projectID uuid.UUID, userID, groupID *uuid.UUID, role dbq.Role) api.ProjectMember {
	v := api.ProjectMember{Id: id, Role: roleView(role)}
	if userID != nil {
		if u, err := s.Q.GetUser(ctx, *userID); err == nil {
			uv := tracker.UserView(u)
			v.User = &uv
		}
	}
	if groupID != nil {
		if g, err := s.Q.GetGroup(ctx, *groupID); err == nil {
			ids, _ := s.Q.GroupMemberIDs(ctx, g.ID)
			gv := groupView(g, len(ids))
			v.Group = &gv
		}
	}
	return v
}

func (s *Server) ListProjectMembers(ctx context.Context, req api.ListProjectMembersRequestObject) (api.ListProjectMembersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Tracker.ListMembers(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	out := api.ListProjectMembers200JSONResponse{}
	for _, m := range rows {
		out = append(out, s.memberView(ctx, m.ID, m.ProjectID, m.UserID, m.GroupID, m.Role))
	}
	return out, nil
}

func (s *Server) AddProjectMember(ctx context.Context, req api.AddProjectMemberRequestObject) (api.AddProjectMemberResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.Tracker.AddMember(ctx, a, req.ProjectKey, req.Body.UserId, req.Body.GroupId, req.Body.RoleKey)
	if err != nil {
		return nil, err
	}
	role, _ := s.Q.GetRole(ctx, m.RoleID)
	return api.AddProjectMember201JSONResponse(s.memberView(ctx, m.ID, m.ProjectID, m.UserID, m.GroupID, role)), nil
}

func (s *Server) UpdateProjectMember(ctx context.Context, req api.UpdateProjectMemberRequestObject) (api.UpdateProjectMemberResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.Tracker.UpdateMember(ctx, a, req.ProjectKey, req.MemberId, req.Body.RoleKey)
	if err != nil {
		return nil, err
	}
	role, _ := s.Q.GetRole(ctx, m.RoleID)
	return api.UpdateProjectMember200JSONResponse(s.memberView(ctx, m.ID, m.ProjectID, m.UserID, m.GroupID, role)), nil
}

func (s *Server) RemoveProjectMember(ctx context.Context, req api.RemoveProjectMemberRequestObject) (api.RemoveProjectMemberResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.RemoveMember(ctx, a, req.ProjectKey, req.MemberId); err != nil {
		return nil, err
	}
	return api.RemoveProjectMember204Response{}, nil
}

func (s *Server) ListProjectLabels(ctx context.Context, req api.ListProjectLabelsRequestObject) (api.ListProjectLabelsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	labels, err := s.Tracker.Labels(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	if labels == nil {
		labels = []string{}
	}
	return api.ListProjectLabels200JSONResponse(labels), nil
}

func issueTypeView(t dbq.IssueType, workflowName string) api.IssueType {
	return api.IssueType{Id: t.ID, Key: t.Key, Name: t.Name, Description: t.Description, Icon: t.Icon, Color: t.Color,
		Level: int(t.Level), Position: int(t.Position), WorkflowId: t.WorkflowID, WorkflowName: workflowName}
}

func (s *Server) workflowName(ctx context.Context, id uuid.UUID) string {
	wf, err := s.Q.GetWorkflow(ctx, id)
	if err != nil {
		return ""
	}
	return wf.Name
}

func (s *Server) ListIssueTypes(ctx context.Context, req api.ListIssueTypesRequestObject) (api.ListIssueTypesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p, _, err := s.Tracker.ProjectFor(ctx, a, req.ProjectKey, "")
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListIssueTypes(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListIssueTypes200JSONResponse{}
	for _, r := range rows {
		t := dbq.IssueType{ID: r.ID, ProjectID: r.ProjectID, Key: r.Key, Name: r.Name, Description: r.Description, Icon: r.Icon, Color: r.Color, Level: r.Level, Position: r.Position, WorkflowID: r.WorkflowID}
		out = append(out, issueTypeView(t, r.WorkflowName))
	}
	return out, nil
}

func (s *Server) CreateIssueType(ctx context.Context, req api.CreateIssueTypeRequestObject) (api.CreateIssueTypeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	t, err := s.Tracker.CreateIssueType(ctx, a, req.ProjectKey, tracker.IssueTypeInput{
		Key: b.Key, Name: b.Name, Description: deref(b.Description), Icon: deref(b.Icon), Color: deref(b.Color), Level: b.Level, WorkflowID: b.WorkflowId,
	})
	if err != nil {
		return nil, err
	}
	return api.CreateIssueType201JSONResponse(issueTypeView(t, s.workflowName(ctx, t.WorkflowID))), nil
}

func (s *Server) UpdateIssueType(ctx context.Context, req api.UpdateIssueTypeRequestObject) (api.UpdateIssueTypeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	t, err := s.Tracker.UpdateIssueType(ctx, a, req.ProjectKey, req.TypeId, tracker.IssueTypePatch{
		Name: b.Name, Description: b.Description, Icon: b.Icon, Color: b.Color, WorkflowID: b.WorkflowId, StatusMapping: deref(b.StatusMapping),
	})
	if err != nil {
		return nil, err
	}
	return api.UpdateIssueType200JSONResponse(issueTypeView(t, s.workflowName(ctx, t.WorkflowID))), nil
}

func (s *Server) DeleteIssueType(ctx context.Context, req api.DeleteIssueTypeRequestObject) (api.DeleteIssueTypeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteIssueType(ctx, a, req.ProjectKey, req.TypeId); err != nil {
		return nil, err
	}
	return api.DeleteIssueType204Response{}, nil
}

func (s *Server) GetCycleTimeReport(ctx context.Context, req api.GetCycleTimeReportRequestObject) (api.GetCycleTimeReportResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	days := 30
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	rep, err := s.Tracker.CycleTime(ctx, a, req.ProjectKey, days)
	if err != nil {
		return nil, err
	}
	return api.GetCycleTimeReport200JSONResponse(cycleTimeView(rep)), nil
}

// ---------------------------------------------------------------------------
// Workflows

func definitionView(def *workflow.Definition) api.WorkflowDefinition {
	v := api.WorkflowDefinition{Name: def.Name, Statuses: []api.WorkflowStatusDef{}, Transitions: []api.TransitionDefinition{}}
	if def.Description != "" {
		v.Description = &def.Description
	}
	for _, st := range def.Statuses {
		v.Statuses = append(v.Statuses, api.WorkflowStatusDef{Id: st.ID, Name: st.Name, Category: api.StatusCategory(st.Category), Initial: st.Initial})
	}
	for _, t := range def.Transitions {
		tv := api.TransitionDefinition{Id: t.ID, Name: t.Name, To: t.To}
		if t.From != nil {
			from := t.From
			tv.From = &from
		}
		if t.Condition != "" {
			c := t.Condition
			tv.Condition = &c
		}
		if len(t.Validators) > 0 {
			vs := []api.ValidatorDef{}
			for _, val := range t.Validators {
				vd := api.ValidatorDef{Expr: val.Expr}
				if val.Message != "" {
					m := val.Message
					vd.Message = &m
				}
				vs = append(vs, vd)
			}
			tv.Validators = &vs
		}
		if len(t.Screen) > 0 {
			sc := []api.ScreenField{}
			for _, f := range t.Screen {
				req := f.Required
				sc = append(sc, api.ScreenField{Field: f.Field, Required: &req})
			}
			tv.Screen = &sc
		}
		if len(t.PostFunctions) > 0 {
			var pfs []api.PostFunction
			raw, _ := json.Marshal(t.PostFunctions)
			_ = json.Unmarshal(raw, &pfs)
			tv.PostFunctions = &pfs
		}
		v.Transitions = append(v.Transitions, tv)
	}
	if len(def.Layout) > 0 {
		layout := map[string]api.NodePosition{}
		for k, p := range def.Layout {
			layout[k] = api.NodePosition{X: p.X, Y: p.Y}
		}
		v.Layout = &layout
	}
	return v
}

func (s *Server) workflowView(ctx context.Context, wf dbq.Workflow) (api.Workflow, error) {
	v := api.Workflow{Id: wf.ID, Name: wf.Name, Description: wf.Description, Shared: wf.ProjectID == nil, CurrentVersion: int(wf.CurrentVersion), UpdatedAt: wf.UpdatedAt, Usage: []api.WorkflowUsage{}}
	if wf.ProjectID != nil {
		if p, err := s.Q.GetProject(ctx, *wf.ProjectID); err == nil {
			v.ProjectKey = &p.Key
		}
	}
	usage, err := s.Q.WorkflowUsage(ctx, wf.ID)
	if err != nil {
		return v, err
	}
	for _, u := range usage {
		v.Usage = append(v.Usage, api.WorkflowUsage{ProjectKey: u.ProjectKey, IssueTypeKey: u.IssueTypeKey})
	}
	return v, nil
}

func (s *Server) versionView(ctx context.Context, wv dbq.WorkflowVersion) (api.WorkflowVersion, error) {
	var def workflow.Definition
	if err := json.Unmarshal(wv.Definition, &def); err != nil {
		return api.WorkflowVersion{}, err
	}
	v := api.WorkflowVersion{Version: int(wv.Version), PublishedAt: wv.PublishedAt, Yaml: wv.SourceYaml, Definition: definitionView(&def)}
	if wv.PublishedBy != nil {
		if u, err := s.Q.GetUser(ctx, *wv.PublishedBy); err == nil {
			uv := tracker.UserView(u)
			v.PublishedBy = &uv
		}
	}
	return v, nil
}

func (s *Server) workflowDetail(ctx context.Context, wf dbq.Workflow) (api.WorkflowDetail, error) {
	base, err := s.workflowView(ctx, wf)
	if err != nil {
		return api.WorkflowDetail{}, err
	}
	wv, err := s.Q.GetWorkflowVersion(ctx, dbq.GetWorkflowVersionParams{WorkflowID: wf.ID, Version: wf.CurrentVersion})
	if err != nil {
		return api.WorkflowDetail{}, err
	}
	ver, err := s.versionView(ctx, wv)
	if err != nil {
		return api.WorkflowDetail{}, err
	}
	pending, err := s.Q.PendingMigrationCount(ctx, wf.ID)
	if err != nil {
		return api.WorkflowDetail{}, err
	}
	return api.WorkflowDetail{
		Id: base.Id, Name: base.Name, Description: base.Description, ProjectKey: base.ProjectKey, Shared: base.Shared,
		CurrentVersion: base.CurrentVersion, UpdatedAt: base.UpdatedAt, Usage: base.Usage, Version: ver, PendingMigration: ptr(pending > 0),
	}, nil
}

func (s *Server) ListWorkflows(ctx context.Context, req api.ListWorkflowsRequestObject) (api.ListWorkflowsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var wfs []dbq.Workflow
	if req.Params.Project != nil && *req.Params.Project != "" {
		p, _, err := s.Tracker.ProjectFor(ctx, a, *req.Params.Project, "")
		if err != nil {
			return nil, err
		}
		wfs, err = s.Q.ListWorkflows(ctx, &p.ID)
		if err != nil {
			return nil, err
		}
	} else {
		all, err := s.Q.ListAllWorkflows(ctx)
		if err != nil {
			return nil, err
		}
		for _, wf := range all {
			if _, err := s.Tracker.GetWorkflow(ctx, a, wf.ID); err == nil {
				wfs = append(wfs, wf)
			}
		}
	}
	out := api.ListWorkflows200JSONResponse{}
	for _, wf := range wfs {
		v, err := s.workflowView(ctx, wf)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) CreateWorkflow(ctx context.Context, req api.CreateWorkflowRequestObject) (api.CreateWorkflowResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.Tracker.CreateWorkflow(ctx, a, req.Body.ProjectKey, req.Body.Yaml)
	if err != nil {
		return nil, err
	}
	d, err := s.workflowDetail(ctx, res.Workflow)
	return api.CreateWorkflow201JSONResponse(d), err
}

func (s *Server) ApplyWorkflow(ctx context.Context, req api.ApplyWorkflowRequestObject) (api.ApplyWorkflowResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, action, err := s.Tracker.ApplyWorkflow(ctx, a, req.Body.ProjectKey, req.Body.Yaml, deref(req.Body.StatusMapping), deref(req.Body.DryRun))
	if err != nil {
		return nil, err
	}
	return api.ApplyWorkflow200JSONResponse{
		Action: api.WorkflowApplyResultAction(action), WorkflowId: res.Workflow.ID, Version: int(res.Version.Version), MigrationQueued: ptr(res.MigrationQueued),
	}, nil
}

func (s *Server) ValidateWorkflow(ctx context.Context, req api.ValidateWorkflowRequestObject) (api.ValidateWorkflowResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.Tracker.ValidateWorkflow(ctx, a, req.Body.Yaml, req.Body.WorkflowId)
	if err != nil {
		return nil, err
	}
	out := api.ValidateWorkflow200JSONResponse{Valid: len(res.Problems) == 0, Errors: []api.ValidationError{}}
	for _, p := range res.Problems {
		e := api.ValidationError{Message: p.Message}
		if p.Path != "" {
			path := p.Path
			e.Path = &path
		}
		out.Errors = append(out.Errors, e)
	}
	if res.Definition != nil {
		d := definitionView(res.Definition)
		out.Definition = &d
	}
	if len(res.Removed) > 0 {
		rs := []api.RemovedStatus{}
		for _, r := range res.Removed {
			rs = append(rs, api.RemovedStatus{Name: r.Name, IssueCount: r.IssueCount})
		}
		out.RemovedStatuses = &rs
	}
	return out, nil
}

func (s *Server) GetWorkflow(ctx context.Context, req api.GetWorkflowRequestObject) (api.GetWorkflowResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	wf, err := s.Tracker.GetWorkflow(ctx, a, req.WorkflowId)
	if err != nil {
		return nil, err
	}
	d, err := s.workflowDetail(ctx, wf)
	return api.GetWorkflow200JSONResponse(d), err
}

func (s *Server) DeleteWorkflow(ctx context.Context, req api.DeleteWorkflowRequestObject) (api.DeleteWorkflowResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteWorkflow(ctx, a, req.WorkflowId); err != nil {
		return nil, err
	}
	return api.DeleteWorkflow204Response{}, nil
}

func (s *Server) ListWorkflowVersions(ctx context.Context, req api.ListWorkflowVersionsRequestObject) (api.ListWorkflowVersionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.Tracker.GetWorkflow(ctx, a, req.WorkflowId); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWorkflowVersions(ctx, req.WorkflowId)
	if err != nil {
		return nil, err
	}
	out := api.ListWorkflowVersions200JSONResponse{}
	for _, r := range rows {
		v := api.WorkflowVersionSummary{Version: int(r.Version), PublishedAt: r.PublishedAt, IssueCount: ptr(int(r.IssueCount))}
		if r.PublishedBy != nil {
			if u, err := s.Q.GetUser(ctx, *r.PublishedBy); err == nil {
				uv := tracker.UserView(u)
				v.PublishedBy = &uv
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) PublishWorkflowVersion(ctx context.Context, req api.PublishWorkflowVersionRequestObject) (api.PublishWorkflowVersionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.Tracker.PublishWorkflow(ctx, a, req.WorkflowId, req.Body.Yaml, deref(req.Body.StatusMapping))
	if err != nil {
		return nil, err
	}
	d, err := s.workflowDetail(ctx, res.Workflow)
	return api.PublishWorkflowVersion201JSONResponse(d), err
}

func (s *Server) GetWorkflowVersion(ctx context.Context, req api.GetWorkflowVersionRequestObject) (api.GetWorkflowVersionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.Tracker.GetWorkflow(ctx, a, req.WorkflowId); err != nil {
		return nil, err
	}
	wv, err := s.Q.GetWorkflowVersion(ctx, dbq.GetWorkflowVersionParams{WorkflowID: req.WorkflowId, Version: int32(req.Version)})
	if err != nil {
		return nil, apperr.NotFound("workflow version")
	}
	v, err := s.versionView(ctx, wv)
	return api.GetWorkflowVersion200JSONResponse(v), err
}
