package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/automation"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/scm"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

type openapiUUID = openapi_types.UUID

func openapiDate(t time.Time) openapi_types.Date { return openapi_types.Date{Time: t} }

func (s *Server) connectionView(c dbq.ScmConnection, repoCount int) api.ScmConnection {
	return api.ScmConnection{
		Id: c.ID, Provider: api.ScmProvider(c.Provider), Name: c.Name, ApiUrl: c.ApiUrl, WebUrl: c.WebUrl, AppId: c.AppID, AppSlug: c.AppSlug,
		ManageHooks: c.ManageHooks, WriteBack: c.WriteBack, WebhookUrl: s.SCM.WebhookURL(c), CreatedAt: c.CreatedAt, LastEventAt: c.LastEventAt,
		RepositoryCount: repoCount,
	}
}

func (s *Server) AdminListScmConnections(ctx context.Context, _ api.AdminListScmConnectionsRequestObject) (api.AdminListScmConnectionsResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListScmConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := api.AdminListScmConnections200JSONResponse{}
	for _, r := range rows {
		c := dbq.ScmConnection{ID: r.ID, Provider: r.Provider, Name: r.Name, ApiUrl: r.ApiUrl, WebUrl: r.WebUrl, AppID: r.AppID, AppSlug: r.AppSlug,
			ManageHooks: r.ManageHooks, WriteBack: r.WriteBack, CreatedAt: r.CreatedAt, LastEventAt: r.LastEventAt}
		out = append(out, s.connectionView(c, int(r.RepositoryCount)))
	}
	return out, nil
}

func (s *Server) AdminCreateScmConnection(ctx context.Context, req api.AdminCreateScmConnectionRequestObject) (api.AdminCreateScmConnectionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	writeBack := true
	if b.WriteBack != nil {
		writeBack = *b.WriteBack
	}
	c, err := s.SCM.CreateConnection(ctx, a, scm.ConnectionInput{
		Provider: string(b.Provider), Name: b.Name, APIURL: deref(b.ApiUrl), WebURL: deref(b.WebUrl), AppID: b.AppId, AppSlug: b.AppSlug,
		PrivateKey: deref(b.PrivateKey), Token: deref(b.Token), WebhookSecret: deref(b.WebhookSecret), ManageHooks: deref(b.ManageHooks), WriteBack: writeBack,
	})
	if err != nil {
		return nil, err
	}
	return api.AdminCreateScmConnection201JSONResponse(s.connectionView(c, 0)), nil
}

func (s *Server) AdminUpdateScmConnection(ctx context.Context, req api.AdminUpdateScmConnectionRequestObject) (api.AdminUpdateScmConnectionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	c, err := s.SCM.UpdateConnection(ctx, a, req.ConnectionId, scm.ConnectionPatch{
		Name: b.Name, PrivateKey: b.PrivateKey, Token: b.Token, WebhookSecret: b.WebhookSecret, ManageHooks: b.ManageHooks, WriteBack: b.WriteBack,
	})
	if err != nil {
		return nil, err
	}
	repos, _ := s.Q.ListConnectionRepositories(ctx, c.ID)
	return api.AdminUpdateScmConnection200JSONResponse(s.connectionView(c, len(repos))), nil
}

func (s *Server) AdminDeleteScmConnection(ctx context.Context, req api.AdminDeleteScmConnectionRequestObject) (api.AdminDeleteScmConnectionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.SCM.DeleteConnection(ctx, a, req.ConnectionId); err != nil {
		return nil, err
	}
	return api.AdminDeleteScmConnection204Response{}, nil
}

func (s *Server) AdminSyncScmConnection(ctx context.Context, req api.AdminSyncScmConnectionRequestObject) (api.AdminSyncScmConnectionResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	if _, err := s.Q.GetScmConnection(ctx, req.ConnectionId); err != nil {
		return nil, apperr.NotFound("connection")
	}
	id := req.ConnectionId
	res, err := s.Jobs.Insert(ctx, jobs.SCMReconcile{ConnectionID: &id}, nil)
	if err != nil {
		return nil, err
	}
	return api.AdminSyncScmConnection202JSONResponse{JobId: res.Job.ID}, nil
}

func (s *Server) AdminListWebhookEvents(ctx context.Context, req api.AdminListWebhookEventsRequestObject) (api.AdminListWebhookEventsResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWebhookEvents(ctx, dbq.ListWebhookEventsParams{ConnectionID: req.ConnectionId, Limit: int32(limitOr(req.Params.Limit, 50))})
	if err != nil {
		return nil, err
	}
	out := api.AdminListWebhookEvents200JSONResponse{}
	for _, e := range rows {
		out = append(out, api.WebhookEvent{
			Id: e.ID, EventType: e.EventType, DeliveryId: e.DeliveryID, Status: api.WebhookEventStatus(e.Status), Error: e.Error,
			Attempts: int(e.Attempts), ReceivedAt: e.ReceivedAt, ProcessedAt: e.ProcessedAt,
		})
	}
	return out, nil
}

func (s *Server) AdminReprocessWebhookEvent(ctx context.Context, req api.AdminReprocessWebhookEventRequestObject) (api.AdminReprocessWebhookEventResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.SCM.Reprocess(ctx, a, req.EventId); err != nil {
		return nil, err
	}
	return api.AdminReprocessWebhookEvent202Response{}, nil
}

func repoView(id, connID uuid.UUID, provider, path, web, branch string, keys []string) api.ScmRepository {
	if keys == nil {
		keys = []string{}
	}
	return api.ScmRepository{Id: id, ConnectionId: connID, Provider: api.ScmProvider(provider), FullPath: path, WebUrl: web, DefaultBranch: branch, ProjectKeys: keys}
}

func (s *Server) ListScmRepositories(ctx context.Context, req api.ListScmRepositoriesRequestObject) (api.ListScmRepositoriesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if a.OrgRole == "guest" {
		return nil, apperr.Forbidden("guests cannot list repositories")
	}
	rows, err := s.Q.ListScmRepositories(ctx, deref(req.Params.Q))
	if err != nil {
		return nil, err
	}
	out := api.ListScmRepositories200JSONResponse{}
	for _, r := range rows {
		out = append(out, repoView(r.ID, r.ConnectionID, r.Provider, r.FullPath, r.WebUrl, r.DefaultBranch, r.ProjectKeys))
	}
	return out, nil
}

func (s *Server) ListProjectRepositories(ctx context.Context, req api.ListProjectRepositoriesRequestObject) (api.ListProjectRepositoriesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p, _, err := s.Tracker.ProjectFor(ctx, a, req.ProjectKey, "")
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListProjectRepositories(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListProjectRepositories200JSONResponse{}
	for _, r := range rows {
		out = append(out, repoView(r.ID, r.ConnectionID, r.Provider, r.FullPath, r.WebUrl, r.DefaultBranch, r.ProjectKeys))
	}
	return out, nil
}

func (s *Server) LinkProjectRepository(ctx context.Context, req api.LinkProjectRepositoryRequestObject) (api.LinkProjectRepositoryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.SCM.LinkRepository(ctx, a, req.ProjectKey, req.Body.RepositoryId)
	if err != nil {
		return nil, err
	}
	c, _ := s.Q.GetScmConnection(ctx, r.ConnectionID)
	projects, _ := s.Q.RepositoryProjects(ctx, r.ID)
	keys := []string{}
	for _, p := range projects {
		keys = append(keys, p.Key)
	}
	return api.LinkProjectRepository201JSONResponse(repoView(r.ID, r.ConnectionID, c.Provider, r.FullPath, r.WebUrl, r.DefaultBranch, keys)), nil
}

func (s *Server) UnlinkProjectRepository(ctx context.Context, req api.UnlinkProjectRepositoryRequestObject) (api.UnlinkProjectRepositoryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.SCM.UnlinkRepository(ctx, a, req.ProjectKey, req.RepositoryId); err != nil {
		return nil, err
	}
	return api.UnlinkProjectRepository204Response{}, nil
}

func (s *Server) CreateIssueBranch(ctx context.Context, req api.CreateIssueBranchRequestObject) (api.CreateIssueBranchResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	link, err := s.SCM.CreateBranch(ctx, a, req.IssueKey, req.Body.RepositoryId, deref(req.Body.Name), deref(req.Body.FromRef))
	if err != nil {
		return nil, err
	}
	repo, _ := s.Q.GetScmRepository(ctx, link.RepositoryID)
	c, _ := s.Q.GetScmConnection(ctx, repo.ConnectionID)
	data := map[string]any{}
	_ = json.Unmarshal(link.Data, &data)
	return api.CreateIssueBranch201JSONResponse{
		Id: link.ID, Kind: api.DevLinkKind(link.Kind), ExternalId: link.ExternalID, Title: link.Title, Url: link.Url, State: link.State,
		AuthorName: link.AuthorName, Data: data, UpdatedAt: link.UpdatedAt,
		Repository: api.RepositoryRef{Id: repo.ID, Provider: api.ScmProvider(c.Provider), FullPath: repo.FullPath, WebUrl: repo.WebUrl},
	}, nil
}

// ---------------------------------------------------------------------------
// Automation rules

func ruleView(r dbq.AutomationRule) api.AutomationRule {
	v := api.AutomationRule{Id: r.ID, Name: r.Name, Description: r.Description, Enabled: r.Enabled, Condition: r.Condition, LastRunAt: r.LastRunAt, NextRunAt: r.NextRunAt, CreatedAt: r.CreatedAt}
	_ = json.Unmarshal(r.TriggerConfig, &v.Trigger)
	v.Trigger.Type = api.AutomationTriggerType(r.TriggerType)
	_ = json.Unmarshal(r.Actions, &v.Actions)
	if v.Actions == nil {
		v.Actions = []api.AutomationAction{}
	}
	return v
}

func ruleInput(b api.AutomationRuleInput) automation.RuleInput {
	in := automation.RuleInput{Name: b.Name, Description: deref(b.Description), Enabled: true, Condition: deref(b.Condition)}
	if b.Enabled != nil {
		in.Enabled = *b.Enabled
	}
	raw, _ := json.Marshal(b.Trigger)
	_ = json.Unmarshal(raw, &in.Trigger)
	raw, _ = json.Marshal(b.Actions)
	_ = json.Unmarshal(raw, &in.Actions)
	return in
}

func (s *Server) ListAutomationRules(ctx context.Context, req api.ListAutomationRulesRequestObject) (api.ListAutomationRulesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := s.Automation.ListRules(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	out := api.ListAutomationRules200JSONResponse{}
	for _, r := range rules {
		out = append(out, ruleView(r))
	}
	return out, nil
}

func (s *Server) CreateAutomationRule(ctx context.Context, req api.CreateAutomationRuleRequestObject) (api.CreateAutomationRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Automation.CreateRule(ctx, a, req.ProjectKey, ruleInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateAutomationRule201JSONResponse(ruleView(r)), nil
}

func (s *Server) UpdateAutomationRule(ctx context.Context, req api.UpdateAutomationRuleRequestObject) (api.UpdateAutomationRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Automation.UpdateRule(ctx, a, req.RuleId, ruleInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateAutomationRule200JSONResponse(ruleView(r)), nil
}

func (s *Server) DeleteAutomationRule(ctx context.Context, req api.DeleteAutomationRuleRequestObject) (api.DeleteAutomationRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Automation.DeleteRule(ctx, a, req.RuleId); err != nil {
		return nil, err
	}
	return api.DeleteAutomationRule204Response{}, nil
}

func (s *Server) ListAutomationRuns(ctx context.Context, req api.ListAutomationRunsRequestObject) (api.ListAutomationRunsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := s.Automation.Runs(ctx, a, req.RuleId, limitOr(req.Params.Limit, 50))
	if err != nil {
		return nil, err
	}
	out := api.ListAutomationRuns200JSONResponse{}
	for _, r := range runs {
		v := api.AutomationRun{Id: r.ID, Status: api.AutomationRunStatus(r.Status), Message: r.Message, Depth: int(r.Depth), CreatedAt: r.CreatedAt}
		if r.IssueKey != "" {
			k := r.IssueKey
			v.IssueKey = &k
		}
		out = append(out, v)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Outbound webhooks

func (s *Server) webhookView(ctx context.Context, w dbq.WebhookSubscription) api.WebhookSubscription {
	v := api.WebhookSubscription{Id: w.ID, Name: w.Name, Url: w.Url, Events: w.Events, Active: w.Active, CreatedAt: w.CreatedAt}
	if w.ProjectID != nil {
		if p, err := s.Q.GetProject(ctx, *w.ProjectID); err == nil {
			v.ProjectKey = &p.Key
		}
	}
	return v
}

func webhookInput(b api.WebhookSubscriptionInput) automation.WebhookInput {
	in := automation.WebhookInput{ProjectKey: deref(b.ProjectKey), Name: b.Name, URL: b.Url, Active: true}
	if b.Active != nil {
		in.Active = *b.Active
	}
	for _, e := range b.Events {
		in.Events = append(in.Events, string(e))
	}
	return in
}

func (s *Server) ListWebhooks(ctx context.Context, req api.ListWebhooksRequestObject) (api.ListWebhooksResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	subs, err := s.Automation.ListWebhooks(ctx, a, deref(req.Params.Project))
	if err != nil {
		return nil, err
	}
	out := api.ListWebhooks200JSONResponse{}
	for _, w := range subs {
		out = append(out, s.webhookView(ctx, w))
	}
	return out, nil
}

func (s *Server) CreateWebhook(ctx context.Context, req api.CreateWebhookRequestObject) (api.CreateWebhookResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	w, secret, err := s.Automation.CreateWebhook(ctx, a, webhookInput(*req.Body))
	if err != nil {
		return nil, err
	}
	v := s.webhookView(ctx, w)
	return api.CreateWebhook201JSONResponse{Id: v.Id, ProjectKey: v.ProjectKey, Name: v.Name, Url: v.Url, Events: v.Events, Active: v.Active, CreatedAt: v.CreatedAt, Secret: secret}, nil
}

func (s *Server) UpdateWebhook(ctx context.Context, req api.UpdateWebhookRequestObject) (api.UpdateWebhookResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	w, err := s.Automation.UpdateWebhook(ctx, a, req.WebhookId, webhookInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateWebhook200JSONResponse(s.webhookView(ctx, w)), nil
}

func (s *Server) DeleteWebhook(ctx context.Context, req api.DeleteWebhookRequestObject) (api.DeleteWebhookResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Automation.DeleteWebhook(ctx, a, req.WebhookId); err != nil {
		return nil, err
	}
	return api.DeleteWebhook204Response{}, nil
}

func (s *Server) ListWebhookDeliveries(ctx context.Context, req api.ListWebhookDeliveriesRequestObject) (api.ListWebhookDeliveriesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ds, err := s.Automation.Deliveries(ctx, a, req.WebhookId, limitOr(req.Params.Limit, 50))
	if err != nil {
		return nil, err
	}
	out := api.ListWebhookDeliveries200JSONResponse{}
	for _, d := range ds {
		v := api.WebhookDelivery{Id: d.ID, Event: d.Event, Status: api.WebhookDeliveryStatus(d.Status), Attempts: int(d.Attempts), LastError: d.LastError, CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt}
		if d.ResponseStatus != nil {
			rs := int(*d.ResponseStatus)
			v.ResponseStatus = &rs
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) TestWebhook(ctx context.Context, req api.TestWebhookRequestObject) (api.TestWebhookResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Automation.TestWebhook(ctx, a, req.WebhookId); err != nil {
		return nil, err
	}
	return api.TestWebhook202Response{}, nil
}

var _ = tracker.UserView
