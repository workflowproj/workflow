// Package authz resolves who may do what. Organisation roles are admin, member and guest;
// project roles are named sets of permissions, granted directly or through groups, and a
// user's effective rights in a project are the union of all their roles.
package authz

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

type Permission = string

const (
	ProjectBrowse   Permission = "project.browse"
	IssueCreate     Permission = "issue.create"
	IssueEdit       Permission = "issue.edit"
	IssueEditOthers Permission = "issue.edit_others"
	IssueTransition Permission = "issue.transition"
	IssueAssign     Permission = "issue.assign"
	IssueComment    Permission = "issue.comment"
	IssueDelete     Permission = "issue.delete"
	CommentModerate Permission = "comment.moderate"
	SprintManage    Permission = "sprint.manage"
	ReleaseManage   Permission = "release.manage"
	BoardManage     Permission = "board.manage"
	ProjectAdmin    Permission = "project.admin"
)

type PermissionInfo struct {
	Key         Permission
	Description string
}

var Permissions = []PermissionInfo{
	{ProjectBrowse, "View the project, its boards and issues"},
	{IssueCreate, "Create issues"},
	{IssueEdit, "Edit issues you reported or are assigned to"},
	{IssueEditOthers, "Edit anyone's issues"},
	{IssueTransition, "Move issues through the workflow"},
	{IssueAssign, "Change assignees"},
	{IssueComment, "Comment on issues"},
	{IssueDelete, "Delete issues"},
	{CommentModerate, "Edit or delete other people's comments"},
	{SprintManage, "Create, start and complete sprints"},
	{ReleaseManage, "Manage releases"},
	{BoardManage, "Create and configure boards"},
	{ProjectAdmin, "Administer the project: settings, members, issue types, workflows, integrations"},
}

func IsKnown(p string) bool {
	return slices.ContainsFunc(Permissions, func(i PermissionInfo) bool { return i.Key == p })
}

func allPermissions() map[Permission]bool {
	m := make(map[Permission]bool, len(Permissions))
	for _, p := range Permissions {
		m[p.Key] = true
	}
	return m
}

type ActorType string

const (
	ActorUser       ActorType = "user"
	ActorAutomation ActorType = "automation"
	ActorSCM        ActorType = "scm"
	ActorSystem     ActorType = "system"
)

var orgRank = map[string]int{"guest": 0, "member": 1, "admin": 2}

// HigherOrgRole returns the more privileged of two organisation roles.
func HigherOrgRole(a, b string) string {
	if orgRank[b] > orgRank[a] {
		return b
	}
	return a
}

// Actor is whoever is performing an operation: a person (session or token), an automation
// rule, the SCM integration acting for a mapped user, or the system itself.
type Actor struct {
	User *dbq.User
	Type ActorType
	// OrgRole is the effective organisation role (own role combined with group mappings).
	OrgRole string
	// TokenID and Scopes are set when authenticated with a personal access token or an OAuth
	// access token.
	TokenID *uuid.UUID
	Scopes  []string
	// Via names the app acting for the person, e.g. "Claude" for an AI assistant connected with
	// OAuth, so history can read "Ada via Claude".
	Via string
	// Depth counts automation rules triggering further rules; RuleID is the rule acting.
	Depth  int
	RuleID *uuid.UUID

	mu    sync.Mutex
	cache map[uuid.UUID]Access
}

func UserActor(u dbq.User, orgRole string) *Actor {
	return &Actor{User: &u, Type: ActorUser, OrgRole: orgRole}
}

func SystemActor() *Actor { return &Actor{Type: ActorSystem, OrgRole: "admin"} }

func AutomationActor(ruleID uuid.UUID, depth int) *Actor {
	return &Actor{Type: ActorAutomation, OrgRole: "admin", RuleID: &ruleID, Depth: depth}
}

func (a *Actor) UserID() *uuid.UUID {
	if a == nil || a.User == nil {
		return nil
	}
	id := a.User.ID
	return &id
}

func (a *Actor) IsOrgAdmin() bool { return a.OrgRole == "admin" }

// Privileged actors (automation, system) are not subject to project permissions.
func (a *Actor) Privileged() bool { return a.Type == ActorAutomation || a.Type == ActorSystem }

func (a *Actor) Name() string {
	switch {
	case a.User != nil:
		return a.User.DisplayName
	case a.Type == ActorAutomation:
		return "Automation"
	default:
		return "System"
	}
}

// Access is an actor's effective rights in one project.
type Access struct {
	Roles []string
	perms map[Permission]bool
}

func (a Access) Has(p Permission) bool { return a.perms[p] }

func (a Access) CanBrowse() bool { return a.perms[ProjectBrowse] }

func (a Access) List() []string {
	var out []string
	for _, p := range Permissions {
		if a.perms[p.Key] {
			out = append(out, p.Key)
		}
	}
	return out
}

// NewAccess builds an Access from role keys and their permissions (exported for tests).
func NewAccess(roles []string, perms []string) Access {
	m := map[Permission]bool{}
	for _, p := range perms {
		m[p] = true
	}
	return Access{Roles: roles, perms: m}
}

// Resolver computes project access, caching per actor (i.e. per request).
type Resolver struct{ Q *dbq.Queries }

func (r *Resolver) Access(ctx context.Context, actor *Actor, project dbq.Project) (Access, error) {
	if actor.Privileged() {
		return Access{Roles: []string{"*"}, perms: allPermissions()}, nil
	}
	if actor.User == nil {
		return Access{}, nil
	}
	actor.mu.Lock()
	if cached, ok := actor.cache[project.ID]; ok {
		actor.mu.Unlock()
		return cached, nil
	}
	actor.mu.Unlock()

	var access Access
	if actor.IsOrgAdmin() {
		access = Access{Roles: []string{"admin"}, perms: allPermissions()}
	} else {
		rows, err := r.Q.UserProjectRoles(ctx, dbq.UserProjectRolesParams{ProjectID: project.ID, UserID: actor.User.ID})
		if err != nil {
			return Access{}, err
		}
		perms := map[Permission]bool{}
		var roles []string
		for _, row := range rows {
			roles = append(roles, row.Key)
			for _, p := range row.Permissions {
				perms[p] = true
			}
		}
		// Open projects grant their open role to organisation members (not guests) without an explicit
		// role in the project, so an explicit membership (direct or via a group) can also restrict.
		if project.OpenRoleID != nil && actor.OrgRole != "guest" && len(rows) == 0 {
			role, err := r.Q.GetRole(ctx, *project.OpenRoleID)
			if err == nil {
				roles = append(roles, role.Key)
				for _, p := range role.Permissions {
					perms[p] = true
				}
			}
		}
		slices.Sort(roles)
		access = Access{Roles: slices.Compact(roles), perms: perms}
	}
	actor.mu.Lock()
	if actor.cache == nil {
		actor.cache = map[uuid.UUID]Access{}
	}
	actor.cache[project.ID] = access
	actor.mu.Unlock()
	return access, nil
}

// Forget drops cached access (after membership changes within the same request).
func (a *Actor) Forget() {
	a.mu.Lock()
	a.cache = nil
	a.mu.Unlock()
}
