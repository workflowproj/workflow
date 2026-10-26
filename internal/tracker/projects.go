package tracker

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/workflow"
)

var projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

type ProjectInput struct {
	Key         string
	Name        string
	Description string
	Template    string
	// OpenRoleKey: role every org member gets; "" makes the project private.
	OpenRoleKey string
	// RisksDecisions turns the Risks and decisions section on or off; nil follows the template.
	RisksDecisions *bool
}

// ProjectWithAccess pairs a project with the caller's permissions in it.
type ProjectWithAccess struct {
	Project dbq.Project
	Access  authz.Access
}

func (s *Service) ListProjects(ctx context.Context, actor *authz.Actor) ([]ProjectWithAccess, error) {
	all, err := s.Q.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var out []ProjectWithAccess
	for _, p := range all {
		access, err := s.Authz.Access(ctx, actor, p)
		if err != nil {
			return nil, err
		}
		if access.CanBrowse() {
			out = append(out, ProjectWithAccess{p, access})
		}
	}
	return out, nil
}

// BrowsableProjectIDs is the allowed-project list used to filter search in SQL.
func (s *Service) BrowsableProjectIDs(ctx context.Context, actor *authz.Actor) ([]uuid.UUID, error) {
	ps, err := s.ListProjects(ctx, actor)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		ids[i] = p.Project.ID
	}
	return ids, nil
}

func (s *Service) keyInUse(ctx context.Context, q *dbq.Queries, key string, exceptProject *uuid.UUID) (bool, error) {
	if p, err := q.GetProjectByKey(ctx, key); err == nil {
		return exceptProject == nil || p.ID != *exceptProject, nil
	} else if !db.IsNoRows(err) {
		return false, err
	}
	if p, err := q.GetProjectByAlias(ctx, key); err == nil {
		return exceptProject == nil || p.ID != *exceptProject, nil
	} else if !db.IsNoRows(err) {
		return false, err
	}
	return false, nil
}

func (s *Service) CreateProject(ctx context.Context, actor *authz.Actor, in ProjectInput) (dbq.Project, error) {
	if actor.User == nil || actor.OrgRole == "guest" || actor.IsCustomer() {
		return dbq.Project{}, apperr.Forbidden("guests cannot create projects")
	}
	in.Key = strings.ToUpper(strings.TrimSpace(in.Key))
	if !projectKeyPattern.MatchString(in.Key) {
		return dbq.Project{}, apperr.Invalid("key", "project keys are 2-10 uppercase letters or digits, starting with a letter")
	}
	if strings.TrimSpace(in.Name) == "" {
		return dbq.Project{}, apperr.Invalid("name", "name is required")
	}
	tpl, ok := templateByID(in.Template)
	if !ok {
		return dbq.Project{}, apperr.Invalid("template", "unknown template %q", in.Template)
	}
	var project dbq.Project
	err := s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if used, err := s.keyInUse(ctx, q, in.Key, nil); err != nil {
			return err
		} else if used {
			return apperr.Conflict("duplicate_key", "the key %s is already used by another project", in.Key)
		}
		var openRole *uuid.UUID
		if in.OpenRoleKey != "" {
			r, err := q.GetRoleByKey(ctx, in.OpenRoleKey)
			if err != nil {
				return apperr.Invalid("openRoleKey", "unknown role %q", in.OpenRoleKey)
			}
			openRole = &r.ID
		}
		var err error
		project, err = q.CreateProject(ctx, dbq.CreateProjectParams{
			Key: in.Key, Name: strings.TrimSpace(in.Name), Description: in.Description,
			LeadID: actor.UserID(), Template: tpl.ID, OpenRoleID: openRole,
		})
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("duplicate_key", "the key %s is already used", in.Key)
			}
			return err
		}
		admin, err := q.GetRoleByKey(ctx, "admin")
		if err != nil {
			return err
		}
		if _, err := q.AddProjectMember(ctx, dbq.AddProjectMemberParams{ProjectID: project.ID, UserID: actor.UserID(), RoleID: admin.ID}); err != nil {
			return err
		}
		if err := s.applyTemplate(ctx, tx, q, actor, project, tpl); err != nil {
			return err
		}
		on := tpl.RisksDecisions
		if in.RisksDecisions != nil {
			on = *in.RisksDecisions
		}
		if on {
			if project, err = s.installRisksDecisions(ctx, tx, q, actor, project); err != nil {
				return err
			}
		}
		if s.AfterCreateProject != nil {
			if err := s.AfterCreateProject(ctx, tx, q, actor, project); err != nil {
				return err
			}
		}
		Audit(ctx, q, actor, "project.create", "project", project.ID.String(), &project.ID, map[string]any{"key": project.Key, "template": tpl.ID})
		return nil
	})
	if err == nil {
		actor.Forget()
	}
	return project, err
}

// applyTemplate copies the template's workflows, issue types and boards into the project.
func (s *Service) applyTemplate(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, p dbq.Project, tpl Template) error {
	sources, err := tpl.WorkflowSources()
	if err != nil {
		return err
	}
	workflows := map[string]PublishResult{}
	for _, src := range sources {
		res, err := s.createWorkflowTx(ctx, tx, q, actor, &p.ID, src)
		if err != nil {
			return fmt.Errorf("template workflow: %w", err)
		}
		workflows[res.Workflow.Name] = res
	}
	for i, t := range tpl.IssueTypes {
		wf, ok := workflows[t.Workflow]
		if !ok {
			return fmt.Errorf("template %s: issue type %s uses unknown workflow %q", tpl.ID, t.Key, t.Workflow)
		}
		if _, err := q.CreateIssueType(ctx, dbq.CreateIssueTypeParams{
			ProjectID: p.ID, Key: t.Key, Name: t.Name, Icon: t.Icon, Color: t.Color,
			Level: int32(t.Level), Position: int32(i), WorkflowID: wf.Workflow.ID, Kind: cmpOr(t.Kind, KindWork),
		}); err != nil {
			return err
		}
	}
	for i, b := range tpl.Boards {
		board, err := q.CreateBoard(ctx, dbq.CreateBoardParams{
			ProjectID: p.ID, Name: b.Name, Type: b.Type, Swimlanes: "none", DoneRetentionDays: 14, Position: int32(i),
		})
		if err != nil {
			return err
		}
		var cols []BoardColumnInput
		if len(b.Columns) > 0 {
			for _, c := range b.Columns {
				col := BoardColumnInput{Name: c.Name, WipLimit: c.WipLimit}
				for _, name := range c.Statuses {
					if st, err := q.GetStatusByName(ctx, name); err == nil {
						col.StatusIDs = append(col.StatusIDs, st.ID)
					}
				}
				cols = append(cols, col)
			}
		} else if cols, err = s.deriveColumns(ctx, q, p.ID); err != nil {
			return err
		}
		if err := writeColumns(ctx, q, board.ID, cols); err != nil {
			return err
		}
	}
	if tpl.BoardType == "scrum" {
		if _, err := q.CreateSprint(ctx, dbq.CreateSprintParams{ProjectID: p.ID, Name: p.Key + " Sprint 1"}); err != nil {
			return err
		}
	}
	return nil
}

type ProjectPatch struct {
	Key         *string
	Name        *string
	Description *string
	LeadID      **uuid.UUID
	// OpenRoleKey: nil = unchanged, "" = private.
	OpenRoleKey *string
	// RisksDecisions turns the Risks and decisions section on (installing what is missing) or off.
	RisksDecisions *bool
}

func (s *Service) UpdateProject(ctx context.Context, actor *authz.Actor, ref string, in ProjectPatch) (dbq.Project, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return p, err
	}
	var out dbq.Project
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		params := dbq.UpdateProjectParams{ID: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, LeadID: p.LeadID, OpenRoleID: p.OpenRoleID}
		if in.Name != nil {
			if strings.TrimSpace(*in.Name) == "" {
				return apperr.Invalid("name", "name is required")
			}
			params.Name = strings.TrimSpace(*in.Name)
		}
		if in.Description != nil {
			params.Description = *in.Description
		}
		if in.LeadID != nil {
			params.LeadID = *in.LeadID
		}
		if in.OpenRoleKey != nil {
			if *in.OpenRoleKey == "" {
				params.OpenRoleID = nil
			} else {
				r, err := q.GetRoleByKey(ctx, *in.OpenRoleKey)
				if err != nil {
					return apperr.Invalid("openRoleKey", "unknown role %q", *in.OpenRoleKey)
				}
				params.OpenRoleID = &r.ID
			}
		}
		if in.Key != nil && strings.ToUpper(*in.Key) != p.Key {
			newKey := strings.ToUpper(strings.TrimSpace(*in.Key))
			if !projectKeyPattern.MatchString(newKey) {
				return apperr.Invalid("key", "project keys are 2-10 uppercase letters or digits, starting with a letter")
			}
			if used, err := s.keyInUse(ctx, q, newKey, &p.ID); err != nil {
				return err
			} else if used {
				return apperr.Conflict("duplicate_key", "the key %s is already used by another project", newKey)
			}
			// Keep the old key resolving (links in commits, bookmarks); drop an alias that becomes the key again.
			if err := q.AddProjectKeyAlias(ctx, dbq.AddProjectKeyAliasParams{Key: p.Key, ProjectID: p.ID}); err != nil {
				return err
			}
			if err := q.DeleteProjectKeyAlias(ctx, newKey); err != nil {
				return err
			}
			params.Key = newKey
			Audit(ctx, q, actor, "project.rename_key", "project", p.ID.String(), &p.ID, map[string]any{"from": p.Key, "to": newKey})
		}
		if in.RisksDecisions != nil {
			if err := s.setRisksDecisions(ctx, tx, q, actor, p, *in.RisksDecisions); err != nil {
				return err
			}
		}
		var err error
		out, err = q.UpdateProject(ctx, params)
		if err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, out, nil, "project.updated", nil)
	})
	return out, err
}

func (s *Service) DeleteProject(ctx context.Context, actor *authz.Actor, ref string) error {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteProject(ctx, p.ID); err != nil {
			return err
		}
		Audit(ctx, q, actor, "project.delete", "project", p.ID.String(), nil, map[string]any{"key": p.Key, "name": p.Name})
		return nil
	})
}

// ---------------------------------------------------------------------------
// Members

func (s *Service) ListMembers(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.ListProjectMembersRow, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	return s.Q.ListProjectMembers(ctx, p.ID)
}

func (s *Service) AddMember(ctx context.Context, actor *authz.Actor, ref string, userID, groupID *uuid.UUID, roleKey string) (dbq.ProjectMember, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return dbq.ProjectMember{}, err
	}
	if (userID == nil) == (groupID == nil) {
		return dbq.ProjectMember{}, apperr.BadRequest("set exactly one of userId or groupId")
	}
	role, err := s.Q.GetRoleByKey(ctx, roleKey)
	if err != nil {
		return dbq.ProjectMember{}, apperr.Invalid("roleKey", "unknown role %q", roleKey)
	}
	if userID != nil {
		if u, err := s.Q.GetUser(ctx, *userID); err == nil && u.OrgRole == authz.OrgCustomer {
			return dbq.ProjectMember{}, apperr.Invalid("userId", "%s is a customer: customers use the portal. Change their organisation role to make them an agent.", u.DisplayName)
		}
	}
	var m dbq.ProjectMember
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		m, err = q.AddProjectMember(ctx, dbq.AddProjectMemberParams{ProjectID: p.ID, UserID: userID, GroupID: groupID, RoleID: role.ID})
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("already_member", "already a member of the project; change their role instead")
			}
			if db.IsForeignKeyViolation(err) {
				return apperr.BadRequest("user or group not found")
			}
			return err
		}
		Audit(ctx, q, actor, "project.member_add", "project", p.ID.String(), &p.ID, map[string]any{"userId": userID, "groupId": groupID, "role": roleKey})
		return nil
	})
	return m, err
}

func (s *Service) UpdateMember(ctx context.Context, actor *authz.Actor, ref string, memberID uuid.UUID, roleKey string) (dbq.ProjectMember, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return dbq.ProjectMember{}, err
	}
	role, err := s.Q.GetRoleByKey(ctx, roleKey)
	if err != nil {
		return dbq.ProjectMember{}, apperr.Invalid("roleKey", "unknown role %q", roleKey)
	}
	m, err := s.Q.UpdateProjectMemberRole(ctx, dbq.UpdateProjectMemberRoleParams{ID: memberID, ProjectID: p.ID, RoleID: role.ID})
	if err != nil {
		return m, apperr.NotFound("member")
	}
	Audit(ctx, s.Q, actor, "project.member_role", "project", p.ID.String(), &p.ID, map[string]any{"memberId": memberID, "role": roleKey})
	return m, nil
}

func (s *Service) RemoveMember(ctx context.Context, actor *authz.Actor, ref string, memberID uuid.UUID) error {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return err
	}
	if _, err := s.Q.GetProjectMember(ctx, dbq.GetProjectMemberParams{ID: memberID, ProjectID: p.ID}); err != nil {
		return apperr.NotFound("member")
	}
	if err := s.Q.RemoveProjectMember(ctx, dbq.RemoveProjectMemberParams{ID: memberID, ProjectID: p.ID}); err != nil {
		return err
	}
	Audit(ctx, s.Q, actor, "project.member_remove", "project", p.ID.String(), &p.ID, map[string]any{"memberId": memberID})
	return nil
}

// AssignableUsers lists people who can browse the project (for assignee pickers).
func (s *Service) AssignableUsers(ctx context.Context, actor *authz.Actor, ref, q string, limit int) ([]dbq.User, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	candidates, err := s.Q.SearchUsers(ctx, dbq.SearchUsersParams{Q: q, Lim: int32(max(limit*4, 100))})
	if err != nil {
		return nil, err
	}
	var out []dbq.User
	for _, u := range candidates {
		ok, err := s.userCanBrowse(ctx, u, p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, u)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// userCanBrowse evaluates another user's access (for assignee validation; notify.canBrowse
// applies the same rule to notification emails).
func (s *Service) userCanBrowse(ctx context.Context, u dbq.User, p dbq.Project) (bool, error) {
	if !u.Active || u.DeletedAt != nil {
		return false, nil
	}
	role, err := s.EffectiveOrgRole(ctx, u)
	if err != nil {
		return false, err
	}
	access, err := s.Authz.Access(ctx, authz.UserActor(u, role), p)
	if err != nil {
		return false, err
	}
	return access.CanBrowse(), nil
}

// EffectiveOrgRole combines a user's own role with roles mapped from their groups. Customers
// stay customers: only changing their own role gives them more.
func (s *Service) EffectiveOrgRole(ctx context.Context, u dbq.User) (string, error) {
	return EffectiveOrgRoleWith(ctx, s.Q, u)
}

// EffectiveOrgRoleWith is EffectiveOrgRole read through q (a caller's transaction).
func EffectiveOrgRoleWith(ctx context.Context, q *dbq.Queries, u dbq.User) (string, error) {
	role := u.OrgRole
	if role == authz.OrgCustomer {
		return role, nil
	}
	roles, err := q.EffectiveOrgRoles(ctx, u.ID)
	if err != nil {
		return role, err
	}
	for _, r := range roles {
		role = authz.HigherOrgRole(role, r)
	}
	return role, nil
}

func (s *Service) Labels(ctx context.Context, actor *authz.Actor, ref string) ([]string, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	return s.Q.ProjectLabels(ctx, p.ID)
}

// ---------------------------------------------------------------------------
// Issue types

type IssueTypeInput struct {
	Key         string
	Name        string
	Description string
	Icon        string
	Color       string
	Level       int
	// Kind is work (the default), risk or decision; fixed once created.
	Kind       string
	WorkflowID uuid.UUID
}

func (s *Service) checkWorkflowForProject(ctx context.Context, q *dbq.Queries, p dbq.Project, workflowID uuid.UUID) error {
	wf, err := q.GetWorkflow(ctx, workflowID)
	if err != nil {
		return apperr.Invalid("workflowId", "workflow not found")
	}
	if wf.ProjectID != nil && *wf.ProjectID != p.ID {
		return apperr.Invalid("workflowId", "that workflow belongs to another project")
	}
	if wf.CurrentVersion == 0 {
		return apperr.Invalid("workflowId", "that workflow has no published version")
	}
	return nil
}

func (s *Service) CreateIssueType(ctx context.Context, actor *authz.Actor, ref string, in IssueTypeInput) (dbq.IssueType, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return dbq.IssueType{}, err
	}
	if err := s.checkWorkflowForProject(ctx, s.Q, p, in.WorkflowID); err != nil {
		return dbq.IssueType{}, err
	}
	in.Kind = cmpOr(in.Kind, KindWork)
	if !slices.Contains(issueTypeKinds, in.Kind) {
		return dbq.IssueType{}, apperr.Invalid("kind", "kind must be work, risk or decision")
	}
	if in.Level < -1 || in.Level > 2 {
		return dbq.IssueType{}, apperr.Invalid("level", "levels are -1 (sub-task), 0 (standard), 1 (epic) or 2")
	}
	if in.Kind != KindWork && in.Level != 0 {
		return dbq.IssueType{}, apperr.Invalid("level", "risk and decision types are standard issue types (level 0)")
	}
	if in.Icon == "" {
		in.Icon = "task"
	}
	if in.Color == "" {
		in.Color = "#4f7cff"
	}
	var t dbq.IssueType
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		existing, err := q.ListIssueTypes(ctx, p.ID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(existing, func(e dbq.ListIssueTypesRow) bool { return e.Key == in.Key }) {
			return apperr.Conflict("duplicate_type", "an issue type with key %q already exists", in.Key)
		}
		t, err = q.CreateIssueType(ctx, dbq.CreateIssueTypeParams{
			ProjectID: p.ID, Key: in.Key, Name: in.Name, Description: in.Description, Icon: in.Icon, Color: in.Color,
			Level: int32(in.Level), Position: int32(len(existing)), WorkflowID: in.WorkflowID, Kind: in.Kind,
		})
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("duplicate_type", "an issue type with key %q already exists", in.Key)
		} else if err != nil {
			return err
		}
		// Risk and decision types get their kind's fields.
		if in.Kind != KindWork {
			return s.ensureKindFields(ctx, q, p, in.Kind, in.Key)
		}
		return nil
	})
	return t, err
}

type IssueTypePatch struct {
	Name, Description, Icon, Color *string
	WorkflowID                     *uuid.UUID
	StatusMapping                  map[string]string
}

// UpdateIssueType changes presentation and, optionally, the type -> workflow mapping. Moving to a
// new workflow re-pins the type's issues through the migration job; statuses that do not exist
// in the new workflow must be mapped.
func (s *Service) UpdateIssueType(ctx context.Context, actor *authz.Actor, ref string, typeID uuid.UUID, in IssueTypePatch) (dbq.IssueType, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return dbq.IssueType{}, err
	}
	var out dbq.IssueType
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		t, err := q.GetIssueType(ctx, typeID)
		if err != nil || t.ProjectID != p.ID {
			return apperr.NotFound("issue type")
		}
		params := dbq.UpdateIssueTypeParams{ID: t.ID, Name: t.Name, Description: t.Description, Icon: t.Icon, Color: t.Color, WorkflowID: t.WorkflowID}
		if in.Name != nil {
			params.Name = *in.Name
		}
		if in.Description != nil {
			params.Description = *in.Description
		}
		if in.Icon != nil {
			params.Icon = *in.Icon
		}
		if in.Color != nil {
			params.Color = *in.Color
		}
		remap := in.WorkflowID != nil && *in.WorkflowID != t.WorkflowID
		statusMap := map[string]string{}
		if remap {
			if err := s.checkWorkflowForProject(ctx, q, p, *in.WorkflowID); err != nil {
				return err
			}
			def, _, err := currentVersion(ctx, q, *in.WorkflowID)
			if err != nil {
				return err
			}
			missing := apperr.New(http.StatusUnprocessableEntity, "status_mapping_required", "Some issues are in statuses the new workflow does not have; map them")
			rows, err := tx.Query(ctx, `SELECT i.status_id, s.name, count(*) FROM issues i JOIN statuses s ON s.id = i.status_id WHERE i.type_id = $1 GROUP BY 1, 2`, t.ID)
			if err != nil {
				return err
			}
			type usage struct {
				id   uuid.UUID
				name string
				n    int
			}
			var used []usage
			for rows.Next() {
				var u usage
				if err := rows.Scan(&u.id, &u.name, &u.n); err != nil {
					rows.Close()
					return err
				}
				used = append(used, u)
			}
			rows.Close()
			for _, u := range used {
				if def.HasStatus(u.id) {
					continue
				}
				target, ok := def.StatusByName(lookupMapping(in.StatusMapping, u.name))
				if !ok {
					missing.Fields = append(missing.Fields, apperr.FieldError{Field: u.name, Message: fmt.Sprintf("%d issue(s) are in %q", u.n, u.name)})
					continue
				}
				statusMap[u.id.String()] = target.ID.String()
			}
			if len(missing.Fields) > 0 {
				return missing
			}
			params.WorkflowID = *in.WorkflowID
		}
		out, err = q.UpdateIssueType(ctx, params)
		if err != nil {
			return err
		}
		if remap && s.Jobs != nil {
			if _, err := s.Jobs.InsertTx(ctx, tx, jobs.WorkflowMigrate{WorkflowID: params.WorkflowID, Mapping: statusMap}, nil); err != nil {
				return err
			}
			Audit(ctx, q, actor, "issue_type.workflow_change", "issue_type", t.ID.String(), &p.ID, map[string]any{"type": t.Key, "workflowId": params.WorkflowID})
		}
		return nil
	})
	return out, err
}

func (s *Service) DeleteIssueType(ctx context.Context, actor *authz.Actor, ref string, typeID uuid.UUID) error {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return err
	}
	t, err := s.Q.GetIssueType(ctx, typeID)
	if err != nil || t.ProjectID != p.ID {
		return apperr.NotFound("issue type")
	}
	n, err := s.Q.CountIssuesOfType(ctx, t.ID)
	if err != nil {
		return err
	}
	if n > 0 {
		return apperr.Conflict("type_in_use", "%d issue(s) use this type; change their type first", n)
	}
	if err := s.Q.DeleteIssueType(ctx, t.ID); db.IsForeignKeyViolation(err) {
		return apperr.Conflict("type_in_use", "request types create issues of this type; change them first")
	} else if err != nil {
		return err
	}
	return nil
}

// projectStatuses returns the statuses used by the workflows of the project's work types,
// ordered by category then by their order in the workflows.
func (s *Service) projectStatuses(ctx context.Context, q *dbq.Queries, projectID uuid.UUID) ([]workflow.Status, error) {
	types, err := q.ListIssueTypes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	seenWf := map[uuid.UUID]bool{}
	seen := map[uuid.UUID]bool{}
	var out []workflow.Status
	// Standard types first so their workflow defines the column order.
	slices.SortStableFunc(types, func(a, b dbq.ListIssueTypesRow) int { return absLevel(a.Level) - absLevel(b.Level) })
	for _, t := range types {
		// Risks and decisions are never on a board, so their statuses are not columns.
		if seenWf[t.WorkflowID] || t.Kind != KindWork {
			continue
		}
		seenWf[t.WorkflowID] = true
		def, _, err := currentVersion(ctx, q, t.WorkflowID)
		if err != nil {
			return nil, err
		}
		for _, st := range def.Statuses {
			if !seen[st.ID] {
				seen[st.ID] = true
				out = append(out, st)
			}
		}
	}
	rank := map[string]int{"todo": 0, "in_progress": 1, "done": 2}
	slices.SortStableFunc(out, func(a, b workflow.Status) int { return rank[a.Category] - rank[b.Category] })
	return out, nil
}

func absLevel(l int32) int {
	if l < 0 {
		return int(-l) * 2
	}
	return int(l)
}
