package httpapi

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

func meView(u dbq.User, orgRole string) api.Me {
	return api.Me{Id: u.ID, DisplayName: u.DisplayName, Email: u.Email, AvatarUrl: u.AvatarUrl, Kind: api.UserKind(u.Kind), OrgRole: api.OrgRole(orgRole), Source: api.UserSource(u.Source)}
}

func (s *Server) ListAuthProviders(ctx context.Context, _ api.ListAuthProvidersRequestObject) (api.ListAuthProvidersResponseObject, error) {
	out := api.ListAuthProviders200JSONResponse{Providers: []api.AuthProvider{}, DevLogin: s.Cfg.DevLogin, LocalLogin: s.Cfg.Local.Enabled}
	for _, p := range s.Cfg.OIDCProviders {
		out.Providers = append(out.Providers, api.AuthProvider{Id: p.ID, Name: p.DisplayName, LoginUrl: "/auth/" + p.ID + "/login"})
	}
	return out, nil
}

func (s *Server) DevLogin(ctx context.Context, req api.DevLoginRequestObject) (api.DevLoginResponseObject, error) {
	if !s.Cfg.DevLogin {
		return nil, apperr.NotFound("route")
	}
	u, err := s.Prov.DevSignIn(ctx, string(req.Body.Email), deref(req.Body.DisplayName))
	if err != nil {
		return nil, err
	}
	w, r := httpFrom(ctx)
	if err := s.Auth.StartSession(w, r, u); err != nil {
		return nil, err
	}
	role, err := s.Tracker.EffectiveOrgRole(ctx, u)
	if err != nil {
		return nil, err
	}
	return api.DevLogin200JSONResponse(meView(u, role)), nil
}

func (s *Server) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	w, r := httpFrom(ctx)
	s.Auth.EndSession(w, r)
	return api.Logout204Response{}, nil
}

func (s *Server) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetMe200JSONResponse(meView(*a.User, a.OrgRole)), nil
}

func tokenView(t dbq.ApiToken) api.ApiToken {
	scopes := make([]api.TokenScope, len(t.Scopes))
	for i, sc := range t.Scopes {
		scopes[i] = api.TokenScope(sc)
	}
	return api.ApiToken{Id: t.ID, Name: t.Name, Prefix: t.TokenPrefix, Scopes: scopes, ExpiresAt: t.ExpiresAt, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt}
}

func scopeStrings(in []api.TokenScope) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	return out
}

func (s *Server) ListMyTokens(ctx context.Context, _ api.ListMyTokensRequestObject) (api.ListMyTokensResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := s.Q.ListUserTokens(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListMyTokens200JSONResponse{}
	for _, t := range ts {
		out = append(out, tokenView(t))
	}
	return out, nil
}

func (s *Server) CreateMyToken(ctx context.Context, req api.CreateMyTokenRequestObject) (api.CreateMyTokenResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if a.TokenID != nil {
		return nil, apperr.Forbidden("tokens cannot create other tokens")
	}
	b := req.Body
	scopes := scopeStrings(b.Scopes)
	for _, sc := range scopes {
		if sc == "admin" && !a.IsOrgAdmin() {
			return nil, apperr.Invalid("scopes", "only organisation admins can create admin-scoped tokens")
		}
	}
	t, raw, err := s.Auth.CreateToken(ctx, s.Q, a.User.ID, &a.User.ID, b.Name, scopes, b.ExpiresInDays)
	if err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "token.create", "token", t.ID.String(), nil, map[string]any{"name": t.Name, "scopes": scopes})
	tv := tokenView(t)
	return api.CreateMyToken201JSONResponse{Id: tv.Id, Name: tv.Name, Prefix: tv.Prefix, Scopes: tv.Scopes, ExpiresAt: tv.ExpiresAt, CreatedAt: tv.CreatedAt, Token: raw}, nil
}

func (s *Server) RevokeMyToken(ctx context.Context, req api.RevokeMyTokenRequestObject) (api.RevokeMyTokenResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.Q.GetAPIToken(ctx, req.TokenId)
	if err != nil || t.UserID != a.User.ID {
		return nil, apperr.NotFound("token")
	}
	if err := s.Q.RevokeAPIToken(ctx, t.ID); err != nil {
		return nil, err
	}
	tracker.Audit(ctx, s.Q, a, "token.revoke", "token", t.ID.String(), nil, map[string]any{"name": t.Name})
	return api.RevokeMyToken204Response{}, nil
}

func (s *Server) ListNotifications(ctx context.Context, req api.ListNotificationsRequestObject) (api.ListNotificationsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: a.User.ID, UnreadOnly: deref(req.Params.Unread), Lim: int32(limitOr(req.Params.Limit, 50))})
	if err != nil {
		return nil, err
	}
	unread, err := s.Q.CountUnreadNotifications(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListNotifications200JSONResponse{Items: []api.Notification{}, Unread: int(unread)}
	actors := map[uuid.UUID]*api.User{}
	for _, n := range rows {
		v := api.Notification{Id: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt}
		if n.IssueKey != "" {
			k := n.IssueKey
			v.IssueKey = &k
		}
		if n.ActorID != nil {
			if _, ok := actors[*n.ActorID]; !ok {
				if u, err := s.Q.GetUser(ctx, *n.ActorID); err == nil {
					uv := tracker.UserView(u)
					actors[*n.ActorID] = &uv
				} else {
					actors[*n.ActorID] = nil
				}
			}
			v.Actor = actors[*n.ActorID]
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}

func (s *Server) MarkNotificationsRead(ctx context.Context, req api.MarkNotificationsReadRequestObject) (api.MarkNotificationsReadResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	if req.Body.Ids != nil {
		ids = *req.Body.Ids
	}
	if err := s.Q.MarkNotificationsRead(ctx, dbq.MarkNotificationsReadParams{UserID: a.User.ID, Ids: ids}); err != nil {
		return nil, err
	}
	return api.MarkNotificationsRead204Response{}, nil
}

// ---------------------------------------------------------------------------
// Directory

func (s *Server) ListUsers(ctx context.Context, req api.ListUsersRequestObject) (api.ListUsersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	limit := limitOr(req.Params.Limit, 50)
	var users []dbq.User
	if req.Params.Project != nil && *req.Params.Project != "" {
		users, err = s.Tracker.AssignableUsers(ctx, a, *req.Params.Project, deref(req.Params.Q), limit)
	} else {
		if a.OrgRole == "guest" {
			return nil, apperr.Forbidden("guests can only search people within a project")
		}
		users, err = s.Q.SearchUsers(ctx, dbq.SearchUsersParams{Q: deref(req.Params.Q), Lim: int32(limit)})
	}
	if err != nil {
		return nil, err
	}
	out := api.ListUsers200JSONResponse{}
	for _, u := range users {
		out = append(out, tracker.UserView(u))
	}
	return out, nil
}

func groupView(g dbq.Group, members int) api.Group {
	v := api.Group{Id: g.ID, DisplayName: g.DisplayName, Description: g.Description, Source: api.GroupSource(g.Source), Email: g.Email, MemberCount: members}
	if g.OrgRole != nil {
		r := api.OrgRole(*g.OrgRole)
		v.OrgRole = &r
	}
	return v
}

func (s *Server) ListGroups(ctx context.Context, req api.ListGroupsRequestObject) (api.ListGroupsResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListGroups(ctx, deref(req.Params.Q))
	if err != nil {
		return nil, err
	}
	out := api.ListGroups200JSONResponse{}
	for _, r := range rows {
		g := dbq.Group{ID: r.ID, DisplayName: r.DisplayName, Description: r.Description, Source: r.Source, ScimConnectionID: r.ScimConnectionID,
			ExternalID: r.ExternalID, Email: r.Email, OrgRole: r.OrgRole, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
		out = append(out, groupView(g, int(r.MemberCount)))
	}
	return out, nil
}

func (s *Server) GetGroup(ctx context.Context, req api.GetGroupRequestObject) (api.GetGroupResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	g, err := s.Q.GetGroup(ctx, req.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group")
	}
	members, err := s.Q.GroupMembers(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	gv := groupView(g, len(members))
	out := api.GetGroup200JSONResponse{Id: gv.Id, DisplayName: gv.DisplayName, Description: gv.Description, Source: gv.Source, Email: gv.Email, OrgRole: gv.OrgRole, MemberCount: gv.MemberCount, Members: []api.User{}}
	for _, m := range members {
		out.Members = append(out.Members, tracker.UserView(m))
	}
	return out, nil
}

func roleView(r dbq.Role) api.Role {
	perms := r.Permissions
	if perms == nil {
		perms = []string{}
	}
	return api.Role{Id: r.ID, Key: r.Key, Name: r.Name, Description: r.Description, Permissions: perms, Builtin: r.Builtin}
}

func (s *Server) ListRoles(ctx context.Context, _ api.ListRolesRequestObject) (api.ListRolesResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	rs, err := s.Q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	out := api.ListRoles200JSONResponse{}
	for _, r := range rs {
		out = append(out, roleView(r))
	}
	return out, nil
}

func (s *Server) ListPermissions(ctx context.Context, _ api.ListPermissionsRequestObject) (api.ListPermissionsResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	out := api.ListPermissions200JSONResponse{}
	for _, p := range authz.Permissions {
		out = append(out, api.PermissionInfo{Key: p.Key, Description: p.Description})
	}
	return out, nil
}

func (s *Server) ListStatuses(ctx context.Context, _ api.ListStatusesRequestObject) (api.ListStatusesResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	sts, err := s.Q.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	out := api.ListStatuses200JSONResponse{}
	for _, st := range sts {
		out = append(out, tracker.StatusView(st))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Custom fields

func (s *Server) fieldView(ctx context.Context, f dbq.FieldDefinition) api.FieldDefinition {
	var opts []string
	_ = json.Unmarshal(f.Options, &opts)
	if opts == nil {
		opts = []string{}
	}
	v := api.FieldDefinition{Id: f.ID, Key: f.Key, Name: f.Name, Description: f.Description, Type: api.FieldType(f.Type), Options: opts}
	if f.ProjectIds != nil {
		ps, _ := s.Q.ListProjectsByIDs(ctx, f.ProjectIds)
		keys := []string{}
		for _, p := range ps {
			keys = append(keys, p.Key)
		}
		v.ProjectKeys = &keys
	}
	if f.IssueTypeKeys != nil {
		k := f.IssueTypeKeys
		v.IssueTypeKeys = &k
	}
	return v
}

func fieldInput(b api.FieldDefinitionInput) tracker.FieldInput {
	return tracker.FieldInput{
		Key: b.Key, Name: b.Name, Description: deref(b.Description), Type: string(b.Type), Options: deref(b.Options),
		ProjectKeys: deref(b.ProjectKeys), IssueTypeKeys: deref(b.IssueTypeKeys),
	}
}

func (s *Server) ListFields(ctx context.Context, _ api.ListFieldsRequestObject) (api.ListFieldsResponseObject, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	fs, err := s.Q.ListFieldDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	out := api.ListFields200JSONResponse{}
	for _, f := range fs {
		out = append(out, s.fieldView(ctx, f))
	}
	return out, nil
}

func (s *Server) CreateField(ctx context.Context, req api.CreateFieldRequestObject) (api.CreateFieldResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.Tracker.CreateField(ctx, a, fieldInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateField201JSONResponse(s.fieldView(ctx, f)), nil
}

func (s *Server) UpdateField(ctx context.Context, req api.UpdateFieldRequestObject) (api.UpdateFieldResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.Tracker.UpdateField(ctx, a, req.FieldId, fieldInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateField200JSONResponse(s.fieldView(ctx, f)), nil
}

func (s *Server) DeleteField(ctx context.Context, req api.DeleteFieldRequestObject) (api.DeleteFieldResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Tracker.DeleteField(ctx, a, req.FieldId); err != nil {
		return nil, err
	}
	return api.DeleteField204Response{}, nil
}
