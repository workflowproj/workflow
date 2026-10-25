// Package authz resolves who may do what. Organisation roles are admin, member and guest;
// project roles are named sets of permissions, granted directly or through groups, and a
// user's effective rights in a project are the union of all their roles.
package authz

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/db/dbq"
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
	// CustomerManage lets agents invite and remove a service project's customers.
	CustomerManage Permission = "customer.manage"
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
	{CustomerManage, "Invite and remove the customers of a service project"},
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
	// ActorPortal is a person acting through the customer portal of one service project. The
	// servicedesk package checks first that they may use the request (or raise one there), then
	// acts for them with only PortalPermissions, in that project only. HTTP authentication never
	// produces this actor.
	ActorPortal ActorType = "portal"
)

// OrgCustomer is the organisation role of customers: people who only use the customer portal.
// They hold no project permissions (whatever their memberships), so every agent page and API
// refuses them, and groups never raise their role.
const OrgCustomer = "customer"

// PortalPermissions are what a portal actor may do in its project: read the request it was
// checked against, raise requests, reply and attach files.
var PortalPermissions = []Permission{ProjectBrowse, IssueCreate, IssueComment}

var orgRank = map[string]int{"customer": -1, "guest": 0, "member": 1, "admin": 2}

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
	// TokenID, TokenName and Scopes are set when authenticated with a personal access token or
	// an OAuth access token.
	TokenID   *uuid.UUID
	TokenName string
	Scopes    []string
	// External is set for an access token issued by an OIDC provider (no TokenID); like a
	// personal access token it is limited by Scopes and never counts as a browser session.
	External bool
	// Via names what the person used to act, recorded with their changes: the app connected
	// with OAuth or the token's name for an AI assistant over MCP ("Ada via Claude"). Empty for
	// the web app and plain API calls.
	Via string
	// Depth counts automation rules triggering further rules; RuleID is the rule acting.
	Depth  int
	RuleID *uuid.UUID
	// PortalProject is the one project a portal actor acts in.
	PortalProject *uuid.UUID

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

// PortalActor acts for a person in the customer portal of one project (see ActorPortal).
func PortalActor(u dbq.User, projectID uuid.UUID) *Actor {
	return &Actor{User: &u, Type: ActorPortal, OrgRole: u.OrgRole, PortalProject: &projectID, Via: "portal"}
}

func (a *Actor) UserID() *uuid.UUID {
	if a == nil || a.User == nil {
		return nil
	}
	id := a.User.ID
	return &id
}

func (a *Actor) IsOrgAdmin() bool { return a.OrgRole == "admin" }

// ViaToken reports authentication with a token (personal, OAuth or an IdP's access token) rather
// than a browser session: scopes apply, and session-only actions are refused.
func (a *Actor) ViaToken() bool { return a != nil && (a.TokenID != nil || a.External) }

// IsCustomer reports a person who may only use the customer portal.
func (a *Actor) IsCustomer() bool {
	return a != nil && (a.OrgRole == OrgCustomer || (a.User != nil && a.User.OrgRole == OrgCustomer))
}

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
	if actor.Type == ActorPortal {
		if actor.User == nil || actor.PortalProject == nil || *actor.PortalProject != project.ID {
			return Access{}, nil
		}
		return NewAccess([]string{"portal"}, PortalPermissions), nil
	}
	// Customers hold no project permissions, whatever memberships they have.
	if actor.User == nil || actor.IsCustomer() {
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
