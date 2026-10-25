package httpapi

import (
	"context"
	"net/http"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/notify"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

// ---------------------------------------------------------------------------
// Email notifications

func (s *Server) emailSettingsView(a *authz.Actor, st notify.Settings) api.EmailNotificationSettings {
	out := api.EmailNotificationSettings{
		Address: a.User.Email, Preferences: []api.EmailNotificationPreference{},
		DigestHour: st.DigestHour, TimeZone: st.TimeZone, Paused: st.Paused, LastDigestAt: st.LastDigestAt,
		NextDigestAt: st.NextDigest(s.Notify.Now()), EmailConfigured: s.Notify.Transport() == "smtp",
	}
	for _, k := range notify.Kinds {
		out.Preferences = append(out.Preferences, api.EmailNotificationPreference{
			Kind: k.Key, Label: k.Label, Description: k.Description,
			Delivery: api.EmailDelivery(st.Delivery(k.Key)), DefaultDelivery: api.EmailDelivery(k.Default),
		})
	}
	return out
}

// emailUser is the signed-in person (service accounts have no notification emails).
func emailUser(ctx context.Context) (*authz.Actor, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if a.User == nil {
		return nil, apperr.Forbidden("only people have email notification settings")
	}
	return a, nil
}

func (s *Server) GetMyEmailNotifications(ctx context.Context, _ api.GetMyEmailNotificationsRequestObject) (api.GetMyEmailNotificationsResponseObject, error) {
	a, err := emailUser(ctx)
	if err != nil {
		return nil, err
	}
	st, err := s.Notify.Get(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	return api.GetMyEmailNotifications200JSONResponse(s.emailSettingsView(a, st)), nil
}

func (s *Server) UpdateMyEmailNotifications(ctx context.Context, req api.UpdateMyEmailNotificationsRequestObject) (api.UpdateMyEmailNotificationsResponseObject, error) {
	a, err := emailUser(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	p := notify.Patch{DigestHour: b.DigestHour, TimeZone: b.TimeZone, Paused: b.Paused}
	if b.Preferences != nil {
		p.Deliveries = map[string]string{}
		for _, pr := range *b.Preferences {
			p.Deliveries[pr.Kind] = string(pr.Delivery)
		}
	}
	st, err := s.Notify.Update(ctx, a.User.ID, p)
	if err != nil {
		return nil, err
	}
	return api.UpdateMyEmailNotifications200JSONResponse(s.emailSettingsView(a, st)), nil
}

func (s *Server) AdminGetEmailStatus(ctx context.Context, _ api.AdminGetEmailStatusRequestObject) (api.AdminGetEmailStatusResponseObject, error) {
	if _, err := orgAdmin(ctx); err != nil {
		return nil, err
	}
	c := s.Cfg.SMTP
	out := api.AdminGetEmailStatus200JSONResponse{
		Transport: api.EmailTransport(s.Notify.Transport()), From: c.From, PublicUrl: s.Cfg.PublicURL,
		UnsubscribeUrl: s.Cfg.HooksPublicURL + notify.UnsubscribePath, BatchWindowSeconds: int(s.Cfg.EmailBatchWindow.Seconds()),
	}
	if c.Host != "" {
		tls := api.EmailStatusTls(c.TLS)
		out.Host, out.Port, out.Tls = &c.Host, &c.Port, &tls
		if c.Username != "" {
			out.Username = &c.Username
		}
	}
	return out, nil
}

func (s *Server) AdminSendTestEmail(ctx context.Context, _ api.AdminSendTestEmailRequestObject) (api.AdminSendTestEmailResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if a.User == nil || a.User.Email == "" {
		return nil, apperr.Invalid("to", "your account has no email address")
	}
	if _, err := s.Notify.SendTest(ctx, *a.User, a.User.DisplayName); err != nil {
		s.Log.Warn("test email failed", "to", a.User.Email, "error", err)
		return nil, apperr.New(http.StatusBadGateway, "email_failed", "the email could not be sent: %v", err)
	}
	tracker.Audit(ctx, s.Q, a, "email.test", "user", a.User.ID.String(), nil, map[string]any{"transport": s.Notify.Transport()})
	return api.AdminSendTestEmail200JSONResponse{To: a.User.Email, Transport: api.EmailTransport(s.Notify.Transport())}, nil
}
