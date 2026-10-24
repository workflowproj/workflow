package local

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// newLink makes a single-use link token: a random value (stored hashed) plus a signature that
// binds it to its purpose.
func (s *Service) newLink(purpose string) (raw, link string) {
	raw = secure.Token("", 32)
	token := strings.TrimPrefix(s.Box.Sign(purpose+":"+raw), purpose+":")
	path := map[string]string{"invite": "/invite", "reset": "/reset-password"}[purpose]
	return raw, s.Cfg.PublicURL + path + "?token=" + url.QueryEscape(token)
}

func (s *Service) resolveLink(ctx context.Context, token string) (dbq.AccountToken, dbq.User, error) {
	token = strings.TrimSpace(token)
	for _, purpose := range []string{"invite", "reset"} {
		v, ok := s.Box.Verify(purpose + ":" + token)
		if !ok {
			continue
		}
		raw := strings.TrimPrefix(v, purpose+":")
		t, err := s.Q.GetOpenAccountToken(ctx, dbq.GetOpenAccountTokenParams{TokenHash: secure.Hash(raw), Purpose: purpose})
		if err != nil {
			return t, dbq.User{}, invalidLink()
		}
		u, err := s.Q.GetUser(ctx, t.UserID)
		if err != nil || u.DeletedAt != nil || Managed(u.Source) || u.Kind != "human" {
			return t, u, invalidLink()
		}
		if !u.Active {
			return t, u, apperr.New(http.StatusForbidden, "deactivated", "This account is deactivated. Ask an administrator to reactivate it.")
		}
		return t, u, nil
	}
	return dbq.AccountToken{}, dbq.User{}, invalidLink()
}

// LinkInfo describes a valid invitation or reset link.
type LinkInfo struct {
	Purpose     string
	Email       string
	DisplayName string
	ExpiresAt   time.Time
}

func (s *Service) Link(ctx context.Context, token, ip string) (LinkInfo, error) {
	if err := s.enabled(); err != nil {
		return LinkInfo{}, err
	}
	if err := allow(s.signInIP, ip); err != nil {
		return LinkInfo{}, err
	}
	t, u, err := s.resolveLink(ctx, token)
	if err != nil {
		return LinkInfo{}, err
	}
	return LinkInfo{Purpose: t.Purpose, Email: u.Email, DisplayName: u.DisplayName, ExpiresAt: t.ExpiresAt}, nil
}

// Redeem sets a password from an invitation or reset link (used up), ends every session of the
// person and, for a reset, emails them. They then sign in as usual (with two-factor if on).
func (s *Service) Redeem(ctx context.Context, token, password, ip string) (LinkInfo, error) {
	if err := s.enabled(); err != nil {
		return LinkInfo{}, err
	}
	if err := allow(s.signInIP, ip); err != nil {
		return LinkInfo{}, err
	}
	t, u, err := s.resolveLink(ctx, token)
	if err != nil {
		return LinkInfo{}, err
	}
	if err := Acceptable("password", password, u); err != nil {
		return LinkInfo{}, err
	}
	h, err := HashPassword(password, s.Params)
	if err != nil {
		return LinkInfo{}, err
	}
	err = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if n, err := q.UseAccountToken(ctx, t.ID); err != nil {
			return err
		} else if n != 1 {
			return invalidLink()
		}
		if _, err := q.SetLocalPassword(ctx, dbq.SetLocalPasswordParams{UserID: u.ID, PasswordHash: &h, MustChange: false}); err != nil {
			return err
		}
		if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID}); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, u.ID); err != nil {
			return err
		}
		if err := q.DeleteUserLoginChallenges(ctx, u.ID); err != nil {
			return err
		}
		if t.Purpose == "invite" {
			s.audit(ctx, q, userActor(u), "user.invitation_accepted", &u, ip, nil)
			return nil
		}
		s.audit(ctx, q, userActor(u), "user.password_reset", &u, ip, nil)
		return s.securityEmail(ctx, tx, u, "Your password was changed", "The password of your Workflow account was reset", ip)
	})
	return LinkInfo{Purpose: t.Purpose, Email: u.Email, DisplayName: u.DisplayName}, err
}

// ForgotPassword emails a reset link to a local account. It answers the same way, and takes
// about as long, whether or not the account exists.
func (s *Service) ForgotPassword(ctx context.Context, email, ip string) error {
	if err := s.enabled(); err != nil {
		return err
	}
	if err := allow(s.signInIP, ip); err != nil {
		return err
	}
	start := time.Now()
	defer func() { time.Sleep(time.Until(start.Add(forgotMinDuration))) }()
	email = normEmail(email)
	if ok, _ := s.forgotEmail.Allow(email); !ok || !strings.Contains(email, "@") {
		return nil
	}
	acc, found, err := s.find(ctx, email)
	if err != nil || !found || !acc.user.Active {
		return err
	}
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := s.sendReset(ctx, tx, q, acc.user, nil); err != nil {
			return err
		}
		s.audit(ctx, q, nil, "user.password_reset_requested", &acc.user, ip, map[string]any{"email": email})
		return nil
	})
}

func (s *Service) sendReset(ctx context.Context, tx pgx.Tx, q *dbq.Queries, u dbq.User, by *uuid.UUID) error {
	raw, link := s.newLink("reset")
	if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID, Purpose: "reset"}); err != nil {
		return err
	}
	if _, err := q.CreateAccountToken(ctx, dbq.CreateAccountTokenParams{UserID: u.ID, Purpose: "reset", TokenHash: secure.Hash(raw), ExpiresAt: time.Now().Add(ResetTTL), CreatedBy: by}); err != nil {
		return err
	}
	return s.email(ctx, tx, resetEmail(u.Email, u.DisplayName, link))
}
