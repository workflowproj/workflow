package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/secure"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

func (s *Server) adminUserView(ctx context.Context, u dbq.User) api.AdminUser {
	eff, _ := s.Tracker.EffectiveOrgRole(ctx, u)
	effRole := api.OrgRole(eff)
	return api.AdminUser{
		Id: u.ID, DisplayName: u.DisplayName, Email: u.Email, AvatarUrl: u.AvatarUrl, Kind: api.UserKind(u.Kind),
		Username: u.Username, OrgRole: api.OrgRole(u.OrgRole), EffectiveOrgRole: &effRole, Active: u.Active,
		Source: api.UserSource(u.Source), GoogleLinked: ptr(u.GoogleID != nil), LastLoginAt: u.LastLoginAt,
		DeactivatedAt: u.DeactivatedAt, DeletedAt: u.DeletedAt, CreatedAt: u.CreatedAt,
	}
}

func ptr[T any](v T) *T { return &v }

func (s *Server) AdminListUsers(ctx context.Context, req api.AdminListUsersRequestObject) (api.AdminListUsersResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	kind := ""
	if req.Params.Kind != nil {
		kind = string(*req.Params.Kind)
	}
	users, err := s.Q.AdminListUsers(ctx, dbq.AdminListUsersParams{Q: deref(req.Params.Q), Kind: kind})
	if err != nil {
		return nil, err
	}
	out := api.AdminListUsers200JSONResponse{}
	local := s.localStatuses(ctx, users)
	for _, u := range users {
		v := s.adminUserView(ctx, u)
		v.LocalAccount = local[u.ID]
		out = append(out, v)
	}
	return out, nil
}

var serviceSlug = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Server) AdminCreateUser(ctx context.Context, req api.AdminCreateUserRequestObject) (api.AdminCreateUserResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	kind := "human"
	if b.Kind != nil {
		kind = string(*b.Kind)
	}
	role := "member"
	if b.OrgRole != nil {
		role = string(*b.OrgRole)
	}
	email := strings.ToLower(strings.TrimSpace(string(deref(b.Email))))
	if kind == "service" {
		if role == "admin" {
			return nil, apperr.Invalid("orgRole", "service accounts cannot be organisation admins; grant project roles instead")
		}
		if role == authz.OrgCustomer {
			return nil, apperr.Invalid("orgRole", "service accounts cannot be customers")
		}
		if email == "" {
			slug := strings.Trim(serviceSlug.ReplaceAllString(strings.ToLower(b.DisplayName), "-"), "-")
			email = fmt.Sprintf("%s.%s@service.invalid", slug, uuid.NewString()[:8])
		}
	} else if email == "" {
		return nil, apperr.Invalid("email", "email is required")
	}
	var u dbq.User
	err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		if u, err = q.CreateUser(ctx, dbq.CreateUserParams{
			Kind: kind, Email: email, Username: email, DisplayName: strings.TrimSpace(b.DisplayName), OrgRole: role, Source: "admin", Active: true,
		}); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("duplicate_user", "a user with that email already exists")
			}
			return err
		}
		for _, gid := range deref(b.GroupIds) {
			if g, err := q.GetGroup(ctx, gid); err != nil || g.Source != "local" {
				return apperr.Invalid("groupIds", "people can only be added to local groups here")
			}
			if err := q.AddGroupMember(ctx, dbq.AddGroupMemberParams{GroupID: gid, UserID: u.ID}); err != nil {
				return err
			}
		}
		tracker.Audit(ctx, q, a, "user.create", "user", u.ID.String(), nil, map[string]any{"email": email, "kind": kind, "orgRole": role, "groupIds": deref(b.GroupIds)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return api.AdminCreateUser201JSONResponse(s.adminUserView(ctx, u)), nil
}

func (s *Server) AdminUpdateUser(ctx context.Context, req api.AdminUpdateUserRequestObject) (api.AdminUpdateUserResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	var out dbq.User
	err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		u, err := q.GetUser(ctx, req.UserId)
		if err != nil || u.DeletedAt != nil {
			return apperr.NotFound("user")
		}
		b := req.Body
		demoting := (b.OrgRole != nil && *b.OrgRole != api.OrgRoleAdmin) || (b.Active != nil && !*b.Active)
		if demoting && u.ID == a.User.ID {
			return apperr.Invalid("orgRole", "you cannot demote or deactivate yourself")
		}
		if demoting && u.OrgRole == "admin" {
			n, err := q.CountActiveAdmins(ctx, u.ID)
			if err != nil {
				return err
			}
			if n == 0 {
				return apperr.Invalid("orgRole", "at least one active organisation admin is required")
			}
		}
		if b.DisplayName != nil {
			if u, err = q.UpdateUserProfile(ctx, dbq.UpdateUserProfileParams{ID: u.ID, DisplayName: strings.TrimSpace(*b.DisplayName), GivenName: u.GivenName, FamilyName: u.FamilyName}); err != nil {
				return err
			}
		}
		if b.OrgRole != nil && string(*b.OrgRole) != u.OrgRole {
			if u.Kind == "service" && *b.OrgRole == api.OrgRoleAdmin {
				return apperr.Invalid("orgRole", "service accounts cannot be organisation admins")
			}
			if u, err = q.SetUserOrgRole(ctx, dbq.SetUserOrgRoleParams{ID: u.ID, OrgRole: string(*b.OrgRole)}); err != nil {
				return err
			}
			tracker.Audit(ctx, q, a, "user.role", "user", u.ID.String(), nil, map[string]any{"orgRole": u.OrgRole})
		}
		if b.Active != nil && *b.Active != u.Active {
			if *b.Active {
				u, err = q.SetUserActive(ctx, dbq.SetUserActiveParams{ID: u.ID, Active: true})
			} else {
				u, err = s.Prov.Deactivate(ctx, q, u.ID)
			}
			if err != nil {
				return err
			}
			tracker.Audit(ctx, q, a, map[bool]string{true: "user.activate", false: "user.deactivate"}[*b.Active], "user", u.ID.String(), nil, nil)
		}
		out = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	v := s.adminUserView(ctx, out)
	v.LocalAccount = s.localStatuses(ctx, []dbq.User{out})[out.ID]
	return api.AdminUpdateUser200JSONResponse(v), nil
}

func (s *Server) AdminListUserTokens(ctx context.Context, req api.AdminListUserTokensRequestObject) (api.AdminListUserTokensResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	ts, err := s.Q.ListUserTokens(ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	out := api.AdminListUserTokens200JSONResponse{}
	for _, t := range ts {
		out = append(out, tokenView(t))
	}
	return out, nil
}

func (s *Server) AdminCreateUserToken(ctx context.Context, req api.AdminCreateUserTokenRequestObject) (api.AdminCreateUserTokenResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.Q.GetUser(ctx, req.UserId)
	if err != nil {
		return nil, apperr.NotFound("user")
	}
	if u.Kind != "service" {
		return nil, apperr.Invalid("userId", "admins can only issue tokens for service accounts; people create their own")
	}
	scopes := scopeStrings(req.Body.Scopes)
	for _, sc := range scopes {
		if sc == "admin" {
			return nil, apperr.Invalid("scopes", "service-account tokens cannot have the admin scope")
		}
	}
	t, raw, err := s.Auth.CreateToken(ctx, s.Q, u.ID, &a.User.ID, req.Body.Name, scopes, req.Body.ExpiresInDays)
	if err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "token.create", "token", t.ID.String(), nil, map[string]any{"name": t.Name, "serviceAccount": u.DisplayName, "scopes": scopes})
	tv := tokenView(t)
	return api.AdminCreateUserToken201JSONResponse{Id: tv.Id, Name: tv.Name, Prefix: tv.Prefix, Scopes: tv.Scopes, ExpiresAt: tv.ExpiresAt, CreatedAt: tv.CreatedAt, Token: raw}, nil
}

func (s *Server) AdminRevokeToken(ctx context.Context, req api.AdminRevokeTokenRequestObject) (api.AdminRevokeTokenResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.Q.GetAPIToken(ctx, req.TokenId)
	if err != nil {
		return nil, apperr.NotFound("token")
	}
	if err := s.Q.RevokeAPIToken(ctx, t.ID); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "token.revoke", "token", t.ID.String(), nil, map[string]any{"name": t.Name})
	return api.AdminRevokeToken204Response{}, nil
}

// ---------------------------------------------------------------------------
// Groups

// errCustomerGroup: groups raise their members' organisation role; customers are only ever
// people whose own role is customer.
var errCustomerGroup = apperr.Invalid("orgRole", "groups cannot make people customers; set a person's own organisation role instead")

func (s *Server) AdminCreateGroup(ctx context.Context, req api.AdminCreateGroupRequestObject) (api.AdminCreateGroupResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	var role *string
	if req.Body.OrgRole != nil {
		if *req.Body.OrgRole == api.OrgRoleCustomer {
			return nil, errCustomerGroup
		}
		role = ptr(string(*req.Body.OrgRole))
	}
	g, err := s.Q.CreateGroup(ctx, dbq.CreateGroupParams{DisplayName: strings.TrimSpace(req.Body.DisplayName), Description: deref(req.Body.Description), Source: "local", OrgRole: role})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("duplicate_group", "a group with that name already exists")
		}
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "group.create", "group", g.ID.String(), nil, map[string]any{"name": g.DisplayName})
	return api.AdminCreateGroup201JSONResponse(groupView(g, 0)), nil
}

func (s *Server) AdminUpdateGroup(ctx context.Context, req api.AdminUpdateGroupRequestObject) (api.AdminUpdateGroupResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.Q.GetGroup(ctx, req.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group")
	}
	b := req.Body
	if g.Source != "local" && (b.DisplayName != nil || b.Description != nil) {
		return nil, apperr.Invalid("displayName", "this group is managed by %s; only its organisation role mapping can change here", g.Source)
	}
	if b.DisplayName != nil || b.Description != nil {
		name, desc := g.DisplayName, g.Description
		if b.DisplayName != nil {
			name = strings.TrimSpace(*b.DisplayName)
		}
		if b.Description != nil {
			desc = *b.Description
		}
		if g, err = s.Q.UpdateGroup(ctx, dbq.UpdateGroupParams{ID: g.ID, DisplayName: name, Description: desc, ExternalID: g.ExternalID, Email: g.Email}); err != nil {
			return nil, err
		}
	}
	if b.OrgRole.IsSpecified() {
		var role *string
		if !b.OrgRole.IsNull() {
			if b.OrgRole.MustGet() == api.OrgRoleCustomer {
				return nil, errCustomerGroup
			}
			role = ptr(string(b.OrgRole.MustGet()))
		}
		if g, err = s.Q.SetGroupOrgRole(ctx, dbq.SetGroupOrgRoleParams{ID: g.ID, OrgRole: role}); err != nil {
			return nil, err
		}
		tracker.Audit(ctx, s.Q, a, "group.org_role", "group", g.ID.String(), nil, map[string]any{"name": g.DisplayName, "orgRole": role})
	}
	members, _ := s.Q.GroupMemberIDs(ctx, g.ID)
	return api.AdminUpdateGroup200JSONResponse(groupView(g, len(members))), nil
}

func (s *Server) localGroup(ctx context.Context, id uuid.UUID) (dbq.Group, error) {
	g, err := s.Q.GetGroup(ctx, id)
	if err != nil {
		return g, apperr.NotFound("group")
	}
	if g.Source != "local" {
		return g, apperr.Invalid("groupId", "this group is managed by %s and is read-only here", g.Source)
	}
	return g, nil
}

func (s *Server) AdminDeleteGroup(ctx context.Context, req api.AdminDeleteGroupRequestObject) (api.AdminDeleteGroupResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.localGroup(ctx, req.GroupId)
	if err != nil {
		return nil, err
	}
	if err := s.Q.DeleteGroup(ctx, g.ID); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "group.delete", "group", g.ID.String(), nil, map[string]any{"name": g.DisplayName})
	return api.AdminDeleteGroup204Response{}, nil
}

func (s *Server) AdminAddGroupMember(ctx context.Context, req api.AdminAddGroupMemberRequestObject) (api.AdminAddGroupMemberResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.localGroup(ctx, req.GroupId)
	if err != nil {
		return nil, err
	}
	if _, err := s.Q.GetUser(ctx, req.UserId); err != nil {
		return nil, apperr.NotFound("user")
	}
	if err := s.Q.AddGroupMember(ctx, dbq.AddGroupMemberParams{GroupID: g.ID, UserID: req.UserId}); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "group.member_add", "group", g.ID.String(), nil, map[string]any{"userId": req.UserId})
	return api.AdminAddGroupMember204Response{}, nil
}

func (s *Server) AdminRemoveGroupMember(ctx context.Context, req api.AdminRemoveGroupMemberRequestObject) (api.AdminRemoveGroupMemberResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.localGroup(ctx, req.GroupId)
	if err != nil {
		return nil, err
	}
	if err := s.Q.RemoveGroupMember(ctx, dbq.RemoveGroupMemberParams{GroupID: g.ID, UserID: req.UserId}); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "group.member_remove", "group", g.ID.String(), nil, map[string]any{"userId": req.UserId})
	return api.AdminRemoveGroupMember204Response{}, nil
}

// ---------------------------------------------------------------------------
// Roles

func validateRole(b api.RoleInput) error {
	if len(b.Permissions) == 0 {
		return apperr.Invalid("permissions", "a role needs at least one permission")
	}
	browse := false
	for _, p := range b.Permissions {
		if !authz.IsKnown(p) {
			return apperr.Invalid("permissions", "unknown permission %q", p)
		}
		browse = browse || p == authz.ProjectBrowse
	}
	if !browse {
		return apperr.Invalid("permissions", "every role must include project.browse")
	}
	return nil
}

func (s *Server) AdminCreateRole(ctx context.Context, req api.AdminCreateRoleRequestObject) (api.AdminCreateRoleResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateRole(*req.Body); err != nil {
		return nil, err
	}
	r, err := s.Q.CreateRole(ctx, dbq.CreateRoleParams{Key: req.Body.Key, Name: req.Body.Name, Description: deref(req.Body.Description), Permissions: req.Body.Permissions})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("duplicate_role", "a role with key %q already exists", req.Body.Key)
		}
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "role.create", "role", r.ID.String(), nil, map[string]any{"key": r.Key, "permissions": r.Permissions})
	return api.AdminCreateRole201JSONResponse(roleView(r)), nil
}

func (s *Server) AdminUpdateRole(ctx context.Context, req api.AdminUpdateRoleRequestObject) (api.AdminUpdateRoleResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := s.Q.GetRole(ctx, req.RoleId)
	if err != nil {
		return nil, apperr.NotFound("role")
	}
	if cur.Builtin {
		return nil, apperr.Invalid("roleId", "built-in roles cannot be changed; create a custom role instead")
	}
	if err := validateRole(*req.Body); err != nil {
		return nil, err
	}
	r, err := s.Q.UpdateRole(ctx, dbq.UpdateRoleParams{ID: cur.ID, Key: req.Body.Key, Name: req.Body.Name, Description: deref(req.Body.Description), Permissions: req.Body.Permissions})
	if err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "role.update", "role", r.ID.String(), nil, map[string]any{"key": r.Key, "permissions": r.Permissions})
	return api.AdminUpdateRole200JSONResponse(roleView(r)), nil
}

func (s *Server) AdminDeleteRole(ctx context.Context, req api.AdminDeleteRoleRequestObject) (api.AdminDeleteRoleResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Q.GetRole(ctx, req.RoleId)
	if err != nil {
		return nil, apperr.NotFound("role")
	}
	if r.Builtin {
		return nil, apperr.Invalid("roleId", "built-in roles cannot be deleted")
	}
	if err := s.Q.DeleteRole(ctx, r.ID); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "role.delete", "role", r.ID.String(), nil, map[string]any{"key": r.Key})
	return api.AdminDeleteRole204Response{}, nil
}

// ---------------------------------------------------------------------------
// Provisioning

func (s *Server) scimBase() string { return s.Cfg.HooksPublicURL + "/scim/v2" }

func scimConnView(c dbq.ScimConnection, users, groups int) api.ScimConnection {
	return api.ScimConnection{Id: c.ID, Name: c.Name, Prefix: c.TokenPrefix, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt, UserCount: &users, GroupCount: &groups}
}

func (s *Server) AdminGetProvisioning(ctx context.Context, _ api.AdminGetProvisioningRequestObject) (api.AdminGetProvisioningResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	providers := []string{}
	domains := []string{}
	for _, p := range s.Cfg.OIDCProviders {
		providers = append(providers, p.DisplayName)
		domains = append(domains, p.AllowedDomains...)
	}
	conns, err := s.Q.ListScimConnections(ctx)
	if err != nil {
		return nil, err
	}
	scim := api.ScimStatus{BaseUrl: s.scimBase(), Connections: []api.ScimConnection{}}
	for _, c := range conns {
		scim.Connections = append(scim.Connections, scimConnView(dbq.ScimConnection{ID: c.ID, Name: c.Name, TokenPrefix: c.TokenPrefix, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt}, int(c.UserCount), int(c.GroupCount)))
	}
	runs, err := s.Q.ListDirectorySyncRuns(ctx, 10)
	if err != nil {
		return nil, err
	}
	dir := s.directoryStatus()
	for _, r := range runs {
		stats := map[string]int{}
		_ = json.Unmarshal(r.Stats, &stats)
		dir.Runs = append(dir.Runs, api.DirectorySyncRun{Id: r.ID, Trigger: r.Trigger, Source: api.DirectorySyncRunSource(r.Source), Status: api.DirectorySyncRunStatus(r.Status), Stats: stats, Error: r.Error, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt})
	}
	bootstrap := s.Cfg.BootstrapAdmins
	if bootstrap == nil {
		bootstrap = []string{}
	}
	return api.AdminGetProvisioning200JSONResponse{
		Oidc:          api.OidcStatus{Providers: providers, AllowedDomains: domains, JitProvisioning: s.Prov.JITEnabled(ctx), BootstrapAdmins: bootstrap},
		Scim:          scim,
		DirectorySync: dir,
	}, nil
}

// directoryStatus describes the configured directory sync (Google Workspace or Keycloak), without runs.
func (s *Server) directoryStatus() api.DirectorySyncStatus {
	dir := api.DirectorySyncStatus{Runs: []api.DirectorySyncRun{}}
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	switch gd, kc := s.Cfg.GoogleDirectory, s.Cfg.Keycloak; {
	case gd.Enabled:
		src := api.DirectorySyncStatusSourceGoogle
		dir = api.DirectorySyncStatus{Configured: true, Source: &src, IntervalMinutes: int(gd.Interval.Minutes()), Group: opt(gd.Group),
			OrgUnit: opt(gd.OrgUnit), SyncGroups: &gd.SyncGroups, GroupPrefix: opt(gd.GroupPrefix), Runs: dir.Runs}
	case kc.Enabled:
		src := api.DirectorySyncStatusSourceKeycloak
		dir = api.DirectorySyncStatus{Configured: true, Source: &src, IntervalMinutes: int(kc.Interval.Minutes()), Group: opt(kc.SyncGroup),
			SyncGroups: &kc.SyncGroups, GroupPrefix: opt(kc.GroupPrefix), Url: opt(kc.URL), Realm: opt(kc.Realm), Provider: opt(kc.Provider), Runs: dir.Runs}
	default:
		dir.IntervalMinutes = int(gd.Interval.Minutes())
	}
	return dir
}

func (s *Server) AdminCreateScimConnection(ctx context.Context, req api.AdminCreateScimConnectionRequestObject) (api.AdminCreateScimConnectionResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	token := secure.Token("wfm_scim_", 32)
	c, err := s.Q.CreateScimConnection(ctx, dbq.CreateScimConnectionParams{Name: strings.TrimSpace(req.Body.Name), TokenHash: secure.Hash(token), TokenPrefix: token[:15], CreatedBy: &a.User.ID})
	if err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "scim.connection_create", "scim_connection", c.ID.String(), nil, map[string]any{"name": c.Name})
	v := scimConnView(c, 0, 0)
	return api.AdminCreateScimConnection201JSONResponse{Id: v.Id, Name: v.Name, Prefix: v.Prefix, CreatedAt: v.CreatedAt, Token: token, BaseUrl: s.scimBase()}, nil
}

func (s *Server) AdminRotateScimConnection(ctx context.Context, req api.AdminRotateScimConnectionRequestObject) (api.AdminRotateScimConnectionResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	token := secure.Token("wfm_scim_", 32)
	c, err := s.Q.RotateScimConnection(ctx, dbq.RotateScimConnectionParams{ID: req.ConnectionId, TokenHash: secure.Hash(token), TokenPrefix: token[:15]})
	if err != nil {
		return nil, apperr.NotFound("SCIM connection")
	}
	tracker.Audit(ctx, s.Q, a, "scim.connection_rotate", "scim_connection", c.ID.String(), nil, map[string]any{"name": c.Name})
	v := scimConnView(c, 0, 0)
	return api.AdminRotateScimConnection200JSONResponse{Id: v.Id, Name: v.Name, Prefix: v.Prefix, CreatedAt: v.CreatedAt, LastUsedAt: c.LastUsedAt, Token: token, BaseUrl: s.scimBase()}, nil
}

func (s *Server) AdminRevokeScimConnection(ctx context.Context, req api.AdminRevokeScimConnectionRequestObject) (api.AdminRevokeScimConnectionResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Q.RevokeScimConnection(ctx, req.ConnectionId); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "scim.connection_revoke", "scim_connection", req.ConnectionId.String(), nil, nil)
	return api.AdminRevokeScimConnection204Response{}, nil
}

func (s *Server) AdminRunDirectorySync(ctx context.Context, _ api.AdminRunDirectorySyncRequestObject) (api.AdminRunDirectorySyncResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	dir := s.directoryStatus()
	if !dir.Configured {
		return nil, apperr.Invalid("directory", "directory sync is not configured (WFM_GOOGLE_DIRECTORY_ENABLED or WFM_KEYCLOAK_ENABLED)")
	}
	res, err := s.Jobs.Insert(ctx, jobs.DirectorySync{Trigger: "manual"}, nil)
	if err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "directory.sync_requested", "directory", "", nil, map[string]any{"source": *dir.Source})
	return api.AdminRunDirectorySync202JSONResponse{JobId: res.Job.ID}, nil
}

func (s *Server) AdminListAudit(ctx context.Context, req api.AdminListAuditRequestObject) (api.AdminListAuditResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListAudit(ctx, dbq.ListAuditParams{Action: deref(req.Params.Action), ActorID: req.Params.ActorId, Before: req.Params.Before, Lim: int32(limitOr(req.Params.Limit, 100))})
	if err != nil {
		return nil, err
	}
	actors := map[uuid.UUID]*api.User{}
	out := api.AdminListAudit200JSONResponse{}
	for _, e := range rows {
		data := map[string]any{}
		_ = json.Unmarshal(e.Data, &data)
		v := api.AuditEvent{Id: e.ID, ActorType: e.ActorType, Action: e.Action, TargetType: e.TargetType, TargetId: e.TargetID, ProjectId: e.ProjectID, Data: data, Ip: e.Ip, CreatedAt: e.CreatedAt}
		if e.ActorID != nil {
			if _, ok := actors[*e.ActorID]; !ok {
				if u, err := s.Q.GetUser(ctx, *e.ActorID); err == nil {
					uv := tracker.UserView(u)
					actors[*e.ActorID] = &uv
				}
			}
			v.Actor = actors[*e.ActorID]
		}
		out = append(out, v)
	}
	return out, nil
}
