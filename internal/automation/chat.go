package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/chat"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

// ---------------------------------------------------------------------------
// Slack and Mattermost channels (incoming webhooks), managed by project admins.

type ChatChannelInput struct {
	Provider   string
	Name       string
	WebhookURL string
	Events     []string
	JQL        string
	Active     bool
}

type ChatChannelPatch struct {
	Name       *string
	WebhookURL *string // nil keeps the current URL
	Events     *[]string
	JQL        *string
	Active     *bool
}

func (s *Service) validateChat(ctx context.Context, actor *authz.Actor, provider, name, webhookURL string, events []string, jql string) error {
	if provider != chat.Slack && provider != chat.Mattermost {
		return apperr.Invalid("provider", "provider must be slack or mattermost")
	}
	if strings.TrimSpace(name) == "" {
		return apperr.Invalid("name", "name the channel, e.g. #team-payments")
	}
	if err := chat.ValidateWebhookURL(provider, webhookURL); err != nil {
		return apperr.Invalid("webhookUrl", "%v", err)
	}
	if len(events) == 0 {
		return apperr.Invalid("events", "choose at least one event")
	}
	for _, e := range events {
		if e != "*" && !slices.Contains(chat.Events, e) {
			return apperr.Invalid("events", "unknown event %q", e)
		}
	}
	if strings.TrimSpace(jql) != "" {
		if err := s.Tracker.ValidateJQL(ctx, actor, jql); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListChatChannels(ctx context.Context, actor *authz.Actor, projectRef string) ([]dbq.ChatChannel, error) {
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectRef, authz.ProjectAdmin)
	if err != nil {
		return nil, err
	}
	return s.Q.ListChatChannels(ctx, p.ID)
}

func (s *Service) CreateChatChannel(ctx context.Context, actor *authz.Actor, projectRef string, in ChatChannelInput) (dbq.ChatChannel, error) {
	p, _, err := s.Tracker.ProjectFor(ctx, actor, projectRef, authz.ProjectAdmin)
	if err != nil {
		return dbq.ChatChannel{}, err
	}
	in.WebhookURL = strings.TrimSpace(in.WebhookURL)
	if err := s.validateChat(ctx, actor, in.Provider, in.Name, in.WebhookURL, in.Events, in.JQL); err != nil {
		return dbq.ChatChannel{}, err
	}
	ch, err := s.Q.CreateChatChannel(ctx, dbq.CreateChatChannelParams{
		ProjectID: p.ID, Provider: in.Provider, Name: strings.TrimSpace(in.Name), WebhookUrlEnc: s.Box.Seal([]byte(in.WebhookURL)),
		UrlHint: chat.URLHint(in.WebhookURL), Events: in.Events, Jql: strings.TrimSpace(in.JQL), Active: in.Active, CreatedBy: actor.UserID(),
	})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "chat.create", "chat_channel", ch.ID.String(), &p.ID, map[string]any{"provider": ch.Provider, "name": ch.Name})
	}
	return ch, err
}

func (s *Service) chatChannelForAdmin(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.ChatChannel, dbq.Project, error) {
	ch, err := s.Q.GetChatChannel(ctx, id)
	if err != nil {
		return ch, dbq.Project{}, apperr.NotFound("chat channel")
	}
	p, _, err := s.Tracker.ProjectFor(ctx, actor, ch.ProjectID.String(), authz.ProjectAdmin)
	if apperr.IsNotFound(err) {
		return ch, p, apperr.NotFound("chat channel")
	}
	return ch, p, err
}

func (s *Service) UpdateChatChannel(ctx context.Context, actor *authz.Actor, id uuid.UUID, in ChatChannelPatch) (dbq.ChatChannel, error) {
	ch, p, err := s.chatChannelForAdmin(ctx, actor, id)
	if err != nil {
		return ch, err
	}
	raw, err := s.Box.Open(ch.WebhookUrlEnc)
	if err != nil {
		return ch, err
	}
	webhookURL, name, events, jql, active := string(raw), ch.Name, ch.Events, ch.Jql, ch.Active
	if in.WebhookURL != nil && strings.TrimSpace(*in.WebhookURL) != "" {
		webhookURL = strings.TrimSpace(*in.WebhookURL)
	}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Events != nil {
		events = *in.Events
	}
	if in.JQL != nil {
		jql = strings.TrimSpace(*in.JQL)
	}
	if in.Active != nil {
		active = *in.Active
	}
	if err := s.validateChat(ctx, actor, ch.Provider, name, webhookURL, events, jql); err != nil {
		return ch, err
	}
	out, err := s.Q.UpdateChatChannel(ctx, dbq.UpdateChatChannelParams{
		ID: id, Name: name, WebhookUrlEnc: s.Box.Seal([]byte(webhookURL)), UrlHint: chat.URLHint(webhookURL), Events: events, Jql: jql, Active: active,
	})
	if err == nil {
		tracker.Audit(ctx, s.Q, actor, "chat.update", "chat_channel", id.String(), &p.ID, map[string]any{"name": out.Name, "active": out.Active})
	}
	return out, err
}

func (s *Service) DeleteChatChannel(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	ch, p, err := s.chatChannelForAdmin(ctx, actor, id)
	if err != nil {
		return err
	}
	// Rules that post to this channel could only fail from now on: pause them, visibly.
	return db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		disabled, err := q.DisableRulesUsingChannel(ctx, dbq.DisableRulesUsingChannelParams{ProjectID: ch.ProjectID, ChannelID: id.String()})
		if err != nil {
			return err
		}
		if err := q.DeleteChatChannel(ctx, id); err != nil {
			return err
		}
		names := make([]string, len(disabled))
		for i, r := range disabled {
			names[i] = r.Name
			tracker.Audit(ctx, q, actor, "automation.disable", "automation_rule", r.ID.String(), &p.ID, map[string]any{"name": r.Name, "reason": "chat channel " + ch.Name + " removed"})
		}
		tracker.Audit(ctx, q, actor, "chat.delete", "chat_channel", id.String(), &p.ID, map[string]any{"name": ch.Name, "disabledRules": names})
		return nil
	})
}

func (s *Service) ChatDeliveries(ctx context.Context, actor *authz.Actor, id uuid.UUID, limit int) ([]dbq.ChatDelivery, error) {
	if _, _, err := s.chatChannelForAdmin(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.Q.ListChatDeliveries(ctx, dbq.ListChatDeliveriesParams{ChannelID: id, Lim: int32(limit)})
}

// TestChatChannel queues a test message, so admins can check the webhook from settings.
func (s *Service) TestChatChannel(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	ch, p, err := s.chatChannelForAdmin(ctx, actor, id)
	if err != nil {
		return err
	}
	who := "Someone"
	if actor.User != nil {
		who = actor.User.DisplayName
	}
	ev := chat.Event{Kind: "test", ProjectName: p.Name, Text: fmt.Sprintf("**%s** connected this channel to %s. Notifications will appear here.", who, chatLink(s.projectURL(p), p.Name))}
	return s.queueChat(ctx, nil, ch, "test", "", chat.Format(ev))
}

// ---------------------------------------------------------------------------
// Dispatch and delivery

// chatEvent maps a domain event to the chat event channels subscribe to ("" when none).
func chatEvent(ev jobs.DomainEvent, data map[string]any) string {
	switch ev.Event {
	case "issue.created", "issue.transitioned", "comment.created", "sprint.started", "sprint.completed", "release.released", "pr.opened", "pr.merged":
		return ev.Event
	case "issue.updated":
		if fields, _ := data["fields"].([]any); slices.Contains(fields, any("assignee")) {
			return "issue.assigned"
		}
	case "pipeline.completed":
		if data["status"] == "failed" {
			return "pipeline.failed"
		}
	}
	return ""
}

func (s *Service) dispatchChat(ctx context.Context, ev jobs.DomainEvent, data map[string]any) error {
	kind := chatEvent(ev, data)
	if kind == "" {
		return nil
	}
	channels, err := s.Q.ChatChannelsForEvent(ctx, dbq.ChatChannelsForEventParams{ProjectID: ev.ProjectID, Event: kind})
	if err != nil || len(channels) == 0 {
		return err
	}
	e, err := s.chatEventDetails(ctx, ev, kind, data)
	if err != nil {
		return err
	}
	msg := chat.Format(e)
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		for _, ch := range channels {
			if ch.Jql != "" && ev.IssueID != nil {
				ok, err := s.Tracker.IssueMatches(ctx, ev.ProjectID, *ev.IssueID, ch.Jql)
				if err != nil {
					s.Log.Warn("chat channel filter failed", "channel", ch.Name, "error", err)
					continue
				}
				if !ok {
					continue
				}
			}
			if err := s.queueChat(ctx, &txq{tx, q}, ch, kind, e.IssueKey, msg); err != nil {
				return err
			}
		}
		return nil
	})
}

type txq struct {
	tx pgx.Tx
	q  *dbq.Queries
}

// queueChat stores a delivery and enqueues its job (in tx when given).
func (s *Service) queueChat(ctx context.Context, t *txq, ch dbq.ChatChannel, event, issueKey string, msg chat.Message) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(chat.Payload(ch.Provider, msg)); err != nil {
		return err
	}
	var key *string
	if issueKey != "" {
		key = &issueKey
	}
	q := s.Q
	if t != nil {
		q = t.q
	}
	d, err := q.CreateChatDelivery(ctx, dbq.CreateChatDeliveryParams{ChannelID: ch.ID, Event: event, IssueKey: key, Payload: buf.Bytes()})
	if err != nil {
		return err
	}
	if t != nil {
		_, err = s.Jobs.InsertTx(ctx, t.tx, jobs.DeliverChat{DeliveryID: d.ID}, nil)
	} else {
		_, err = s.Jobs.Insert(ctx, jobs.DeliverChat{DeliveryID: d.ID}, nil)
	}
	return err
}

// DeliverChat posts a stored message. Network errors, 5xx and 429 are retried; other answers
// (a deleted channel, a revoked webhook) are recorded and not retried.
func (s *Service) DeliverChat(ctx context.Context, deliveryID uuid.UUID) error {
	d, err := s.Q.GetChatDelivery(ctx, deliveryID)
	if err != nil {
		return nil
	}
	ch, err := s.Q.GetChatChannel(ctx, d.ChannelID)
	if err != nil {
		return nil
	}
	raw, err := s.Box.Open(ch.WebhookUrlEnc)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, string(raw), bytes.NewReader(d.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "workflow-platform-chat/1.0")
	res, err := s.HTTP.Do(req)
	finish := func(status string, code *int32, msg *string) {
		if uerr := s.Q.FinishChatDelivery(ctx, dbq.FinishChatDeliveryParams{ID: d.ID, Status: status, ResponseStatus: code, LastError: msg}); uerr != nil {
			s.Log.Warn("record chat delivery", "error", uerr)
		}
	}
	if err != nil {
		m := err.Error()
		finish("failed", nil, &m)
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	code := int32(res.StatusCode)
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		finish("delivered", &code, nil)
		return nil
	}
	m := fmt.Sprintf("%s answered HTTP %d: %s", ch.Provider, res.StatusCode, strings.TrimSpace(string(body)))
	finish("failed", &code, &m)
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		return &statusError{res.StatusCode}
	}
	return nil
}

// chatAction posts an automation message to one of the project's channels.
func (s *Service) chatAction(ctx context.Context, p dbq.Project, issue dbq.Issue, a Action, text string) (string, error) {
	id, err := uuid.Parse(a.ChannelID)
	if err != nil {
		return "", fmt.Errorf("no chat channel")
	}
	ch, err := s.Q.GetChatChannel(ctx, id)
	if err != nil || ch.ProjectID != issue.ProjectID {
		return "", fmt.Errorf("chat channel not found")
	}
	if !ch.Active {
		return "chat channel " + ch.Name + " is paused", nil
	}
	view, err := s.Tracker.IssueView(ctx, issue)
	if err != nil {
		return "", err
	}
	msg := chat.Format(chat.Event{
		Kind: "automation", Text: text, ProjectName: p.Name, IssueKey: view.Key, IssueURL: s.issueURL(view.Key), Summary: issue.Summary,
		Category: string(view.Status.Category), ExtraContext: "automation",
	})
	if err := s.queueChat(ctx, nil, ch, "automation", view.Key, msg); err != nil {
		return "", err
	}
	return "posted to " + ch.Name, nil
}

// chatEventDetails gathers what a message shows: the issue, who did it, and event specifics.
func (s *Service) chatEventDetails(ctx context.Context, ev jobs.DomainEvent, kind string, data map[string]any) (chat.Event, error) {
	p, err := s.Q.GetProject(ctx, ev.ProjectID)
	if err != nil {
		return chat.Event{}, err
	}
	e := chat.Event{Kind: kind, ProjectKey: p.Key, ProjectName: p.Name, ProjectURL: s.projectURL(p)}
	if ev.ActorID != nil {
		if u, err := s.Q.GetUser(ctx, *ev.ActorID); err == nil {
			e.Actor = u.DisplayName
		}
	}
	if ev.IssueID != nil {
		if issue, err := s.Q.GetIssue(ctx, *ev.IssueID); err == nil {
			if view, err := s.Tracker.IssueView(ctx, issue); err == nil {
				e.IssueKey, e.IssueURL, e.Summary = view.Key, s.issueURL(view.Key), view.Summary
				e.Type, e.Status, e.Category, e.Priority = view.Type.Name, view.Status.Name, string(view.Status.Category), string(view.Priority)
				if view.Assignee != nil {
					e.Assignee = view.Assignee.DisplayName
				}
			}
		}
	}
	str := func(k string) string {
		if v, ok := data[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	switch kind {
	case "issue.transitioned":
		e.FromStatus, e.ToStatus = str("from"), str("to")
		if c := str("toCategory"); c != "" {
			e.Category = c
		}
	case "comment.created":
		e.CommentBody = str("body")
	case "sprint.started", "sprint.completed":
		e.Name = str("name")
		if id, err := uuid.Parse(str("sprintId")); err == nil {
			if sp, err := s.Q.GetSprint(ctx, id); err == nil {
				e.Goal = sp.Goal
				if sp.StartDate != nil && sp.EndDate != nil {
					e.ExtraContext = sp.StartDate.Format("2 Jan") + " – " + sp.EndDate.Format("2 Jan 2006")
				}
			}
		}
		if n, ok := data["movedIssues"].(float64); ok {
			e.MovedIssues = int(n)
		}
	case "release.released":
		e.Name = str("name")
	case "pr.opened", "pr.merged":
		e.PRNumber, e.PRTitle, e.PRURL, e.Branch, e.RepositoryRef = str("number"), str("title"), str("url"), str("branch"), str("repository")
	case "pipeline.failed":
		e.PipelineName, e.PipelineURL, e.Branch, e.RepositoryRef = str("pipeline"), str("url"), str("branch"), str("repository")
	}
	return e, nil
}

func (s *Service) issueURL(key string) string { return s.Cfg.PublicURL + "/browse/" + key }

func (s *Service) projectURL(p dbq.Project) string {
	return s.Cfg.PublicURL + "/projects/" + p.Key + "/board"
}

func chatLink(u, text string) string { return "[" + text + "](" + u + ")" }

// PruneChatDeliveries drops delivery logs older than the cutoff (maintenance).
func (s *Service) PruneChatDeliveries(ctx context.Context, before time.Time) (int64, error) {
	return s.Q.PruneChatDeliveries(ctx, before)
}
