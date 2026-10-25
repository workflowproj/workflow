package httpapi

import (
	"context"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/automation"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

func chatChannelView(c dbq.ChatChannel) api.ChatChannel {
	events := make([]api.ChatEvent, len(c.Events))
	for i, e := range c.Events {
		events[i] = api.ChatEvent(e)
	}
	return api.ChatChannel{
		Id: c.ID, Provider: api.ChatProvider(c.Provider), Name: c.Name, Events: events, Jql: c.Jql, Active: c.Active,
		UrlHint: c.UrlHint, CreatedAt: c.CreatedAt,
	}
}

func chatEvents(in []api.ChatEvent) []string {
	out := make([]string, len(in))
	for i, e := range in {
		out[i] = string(e)
	}
	return out
}

func (s *Server) ListChatChannels(ctx context.Context, req api.ListChatChannelsRequestObject) (api.ListChatChannelsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Automation.ListChatChannels(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	out := api.ListChatChannels200JSONResponse{}
	for _, c := range rows {
		out = append(out, chatChannelView(c))
	}
	return out, nil
}

func (s *Server) CreateChatChannel(ctx context.Context, req api.CreateChatChannelRequestObject) (api.CreateChatChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	active := true
	if b.Active != nil {
		active = *b.Active
	}
	c, err := s.Automation.CreateChatChannel(ctx, a, req.ProjectKey, automation.ChatChannelInput{
		Provider: string(b.Provider), Name: b.Name, WebhookURL: b.WebhookUrl, Events: chatEvents(b.Events), JQL: deref(b.Jql), Active: active,
	})
	if err != nil {
		return nil, err
	}
	return api.CreateChatChannel201JSONResponse(chatChannelView(c)), nil
}

func (s *Server) UpdateChatChannel(ctx context.Context, req api.UpdateChatChannelRequestObject) (api.UpdateChatChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	patch := automation.ChatChannelPatch{Name: b.Name, WebhookURL: b.WebhookUrl, JQL: b.Jql, Active: b.Active}
	if b.Events != nil {
		ev := chatEvents(*b.Events)
		patch.Events = &ev
	}
	c, err := s.Automation.UpdateChatChannel(ctx, a, req.ChannelId, patch)
	if err != nil {
		return nil, err
	}
	return api.UpdateChatChannel200JSONResponse(chatChannelView(c)), nil
}

func (s *Server) DeleteChatChannel(ctx context.Context, req api.DeleteChatChannelRequestObject) (api.DeleteChatChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Automation.DeleteChatChannel(ctx, a, req.ChannelId); err != nil {
		return nil, err
	}
	return api.DeleteChatChannel204Response{}, nil
}

func (s *Server) TestChatChannel(ctx context.Context, req api.TestChatChannelRequestObject) (api.TestChatChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Automation.TestChatChannel(ctx, a, req.ChannelId); err != nil {
		return nil, err
	}
	return api.TestChatChannel202Response{}, nil
}

func (s *Server) ListChatDeliveries(ctx context.Context, req api.ListChatDeliveriesRequestObject) (api.ListChatDeliveriesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Automation.ChatDeliveries(ctx, a, req.ChannelId, limitOr(req.Params.Limit, 30))
	if err != nil {
		return nil, err
	}
	out := api.ListChatDeliveries200JSONResponse{}
	for _, d := range rows {
		v := api.ChatDelivery{Id: d.ID, Event: d.Event, IssueKey: d.IssueKey, Status: api.ChatDeliveryStatus(d.Status), Attempts: int(d.Attempts),
			LastError: d.LastError, CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt}
		if d.ResponseStatus != nil {
			rs := int(*d.ResponseStatus)
			v.ResponseStatus = &rs
		}
		out = append(out, v)
	}
	return out, nil
}
