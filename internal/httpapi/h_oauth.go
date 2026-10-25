package httpapi

import (
	"context"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/oauth"
	"github.com/workflowproj/workflow/internal/tracker"
)

// AI assistants connected over OAuth: the consent page's API and the connected apps lists.
// The OAuth endpoints themselves (/oauth/authorize, /oauth/token, …) live in internal/oauth.

func oauthClientView(c dbq.OauthClient) api.OAuthClient {
	return api.OAuthClient{Id: c.ID, Name: c.Name, Kind: api.OAuthClientKind(c.Kind), Host: oauth.ClientHost(c), ClientUri: c.ClientUri, LogoUri: c.LogoUri}
}

func oauthScopes(in []string) []api.OAuthScope {
	out := make([]api.OAuthScope, len(in))
	for i, s := range in {
		out[i] = api.OAuthScope(s)
	}
	return out
}

func connectedAppView(g dbq.OauthGrant, c dbq.OauthClient) api.ConnectedApp {
	updated := g.UpdatedAt
	return api.ConnectedApp{Id: g.ID, Client: oauthClientView(c), Scopes: oauthScopes(g.Scopes), CreatedAt: g.CreatedAt, UpdatedAt: &updated, LastUsedAt: g.LastUsedAt}
}

func (s *Server) GetOAuthConsent(ctx context.Context, req api.GetOAuthConsentRequestObject) (api.GetOAuthConsentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.OAuth.Consent(ctx, a, req.Params.Request)
	if err != nil {
		return nil, err
	}
	out := api.GetOAuthConsent200JSONResponse{
		Client: oauthClientView(c.Client), RedirectUri: c.RedirectURI, RedirectHost: c.RedirectHost(), NativeRedirect: c.NativeRedirect(),
		RequestedScopes: oauthScopes(c.Requested), AlreadyGranted: c.AlreadyGranted(),
	}
	if c.Granted != nil {
		g := oauthScopes(c.Granted)
		out.GrantedScopes = &g
	}
	return out, nil
}

func (s *Server) DecideOAuthConsent(ctx context.Context, req api.DecideOAuthConsentRequestObject) (api.DecideOAuthConsentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var scopes []string
	for _, sc := range deref(req.Body.Scopes) {
		scopes = append(scopes, string(sc))
	}
	to, err := s.OAuth.Decide(ctx, a, req.Body.Request, req.Body.Approve, scopes)
	if err != nil {
		return nil, err
	}
	return api.DecideOAuthConsent200JSONResponse{RedirectUrl: to}, nil
}

func (s *Server) ListMyConnectedApps(ctx context.Context, _ api.ListMyConnectedAppsRequestObject) (api.ListMyConnectedAppsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListUserOAuthGrants(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListMyConnectedApps200JSONResponse{}
	for _, r := range rows {
		out = append(out, connectedAppView(r.OauthGrant, r.OauthClient))
	}
	return out, nil
}

func (s *Server) RevokeMyConnectedApp(ctx context.Context, req api.RevokeMyConnectedAppRequestObject) (api.RevokeMyConnectedAppResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.OAuth.RevokeGrant(ctx, a, req.GrantId, false); err != nil {
		return nil, err
	}
	return api.RevokeMyConnectedApp204Response{}, nil
}

func (s *Server) AdminListConnectedApps(ctx context.Context, _ api.AdminListConnectedAppsRequestObject) (api.AdminListConnectedAppsResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListOAuthGrants(ctx)
	if err != nil {
		return nil, err
	}
	out := api.AdminListConnectedApps200JSONResponse{}
	for _, r := range rows {
		v := connectedAppView(r.OauthGrant, r.OauthClient)
		u := tracker.UserView(r.User)
		v.User = &u
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) AdminRevokeConnectedApp(ctx context.Context, req api.AdminRevokeConnectedAppRequestObject) (api.AdminRevokeConnectedAppResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.OAuth.RevokeGrant(ctx, a, req.GrantId, true); err != nil {
		return nil, err
	}
	return api.AdminRevokeConnectedApp204Response{}, nil
}
