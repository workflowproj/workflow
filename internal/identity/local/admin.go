package local

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/secure"
)

// Status is how a local account stands, for the administration user list.
type Status struct {
	HasPassword         bool
	MustChange          bool
	TOTPEnabled         bool
	LockedUntil         *time.Time
	PasswordChangedAt   *time.Time
	InvitationExpiresAt *time.Time
}

// Statuses returns the local-account status of those of the users who are local accounts.
func (s *Service) Statuses(ctx context.Context, users []dbq.User) (map[uuid.UUID]Status, error) {
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		if !Managed(u.Source) {
			ids = append(ids, u.ID)
		}
	}
	out := map[uuid.UUID]Status{}
	if len(ids) == 0 {
		return out, nil
	}
	creds, err := s.Q.ListLocalCredentials(ctx, ids)
	if err != nil {
		return nil, err
	}
	invites, err := s.Q.OpenInvitations(ctx, ids)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, c := range creds {
		st := Status{HasPassword: c.PasswordHash != nil, MustChange: c.MustChange, TOTPEnabled: c.TotpSecret != nil, PasswordChangedAt: c.PasswordChangedAt}
		if c.LockedUntil != nil && c.LockedUntil.After(now) {
			st.LockedUntil = c.LockedUntil
		}
		out[c.UserID] = st
	}
	for _, inv := range invites {
		if st, ok := out[inv.UserID]; ok && !st.HasPassword {
			exp := inv.ExpiresAt
			st.InvitationExpiresAt = &exp
			out[inv.UserID] = st
		}
	}
	return out, nil
}

// person loads someone an administrator may give a password: a person, not deleted, not
// owned by SCIM or Google directory sync.
func (s *Service) person(ctx context.Context, q *dbq.Queries, id uuid.UUID) (dbq.User, error) {
	if err := s.enabled(); err != nil {
		return dbq.User{}, err
	}
	u, err := q.GetUser(ctx, id)
	if err != nil || u.DeletedAt != nil {
		return u, apperr.NotFound("user")
	}
	if u.Kind != "human" {
		return u, apperr.New(http.StatusBadRequest, "service_account", "Service accounts sign in with tokens, not passwords.")
	}
	if Managed(u.Source) {
		return u, apperr.New(http.StatusBadRequest, "managed_by_identity_provider",
			"%s is managed by %s, so they sign in through your identity provider and cannot have a password here.", u.DisplayName, sourceName(u.Source))
	}
	return u, nil
}

func inviter(a *authz.Actor) string {
	if a != nil && a.User != nil {
		return a.User.DisplayName
	}
	return "An administrator"
}

// Invite emails someone a link to choose a password (a new invitation replaces the old one).
func (s *Service) Invite(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) (time.Time, error) {
	expires := time.Now().Add(InviteTTL)
	err := db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		if !u.Active {
			return apperr.Invalid("userId", "Reactivate %s before inviting them.", u.DisplayName)
		}
		cred, err := q.EnsureLocalCredentials(ctx, u.ID)
		if err != nil {
			return err
		}
		if cred.PasswordHash != nil {
			return apperr.Conflict("has_password", "%s already has a password; send them a password reset instead.", u.DisplayName)
		}
		if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID, Purpose: "invite"}); err != nil {
			return err
		}
		raw, link := s.newLink("invite")
		if _, err := q.CreateAccountToken(ctx, dbq.CreateAccountTokenParams{UserID: u.ID, Purpose: "invite", TokenHash: secure.Hash(raw), ExpiresAt: expires, CreatedBy: admin.UserID()}); err != nil {
			return err
		}
		s.audit(ctx, q, admin, "user.invitation_sent", &u, ip, map[string]any{"email": u.Email, "expiresAt": expires})
		return s.email(ctx, tx, inviteEmail(u.Email, u.DisplayName, inviter(admin), link, expires))
	})
	return expires, err
}

// RevokeInvitation cancels an open invitation; someone without a password stops being a local account.
func (s *Service) RevokeInvitation(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) error {
	return db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID, Purpose: "invite"}); err != nil {
			return err
		}
		if cred, err := q.GetLocalCredentials(ctx, u.ID); err == nil && cred.PasswordHash == nil {
			if err := q.DeleteLocalCredentials(ctx, u.ID); err != nil {
				return err
			}
		}
		s.audit(ctx, q, admin, "user.invitation_revoked", &u, ip, nil)
		return nil
	})
}

// temporaryPassword is random, readable and long enough to pass the policy: xxxx-xxxx-xxxx-xxxx.
func temporaryPassword() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	for {
		raw := secure.Token("", 24)
		var b strings.Builder
		for i := 0; i < 16; i++ {
			if i > 0 && i%4 == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(alphabet[int(raw[i])%len(alphabet)])
		}
		if pw := b.String(); CheckPassword(pw, "", "").OK() {
			return pw
		}
	}
}

// SetTemporaryPassword gives someone a generated password (returned once) that they must change
// at their next sign-in. Their sessions end and open links close.
func (s *Service) SetTemporaryPassword(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) (string, error) {
	pw := temporaryPassword()
	h, err := HashPassword(pw, s.Params)
	if err != nil {
		return "", err
	}
	err = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		if !u.Active {
			return apperr.Invalid("userId", "Reactivate %s first.", u.DisplayName)
		}
		had := false
		if cred, err := q.GetLocalCredentials(ctx, u.ID); err == nil {
			had = cred.PasswordHash != nil
		}
		if _, err := q.SetLocalPassword(ctx, dbq.SetLocalPasswordParams{UserID: u.ID, PasswordHash: &h, MustChange: true}); err != nil {
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
		s.audit(ctx, q, admin, "user.temporary_password_set", &u, ip, nil)
		if had {
			return s.securityEmail(ctx, tx, u, "Your password was changed", "An administrator set a temporary password for your Workflow account", ip)
		}
		return nil
	})
	return pw, err
}

// SendPasswordReset emails a local account a reset link on an administrator's behalf.
func (s *Service) SendPasswordReset(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) error {
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		if cred, err := q.GetLocalCredentials(ctx, u.ID); err != nil || cred.PasswordHash == nil {
			return apperr.Invalid("userId", "%s has no password yet; send an invitation instead.", u.DisplayName)
		}
		if !u.Active {
			return apperr.Invalid("userId", "Reactivate %s first.", u.DisplayName)
		}
		if err := s.sendReset(ctx, tx, q, u, admin.UserID()); err != nil {
			return err
		}
		s.audit(ctx, q, admin, "user.password_reset_sent", &u, ip, nil)
		return nil
	})
}

// Unlock clears a lockout and the failure count.
func (s *Service) Unlock(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) error {
	return db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		if err := q.ClearLoginFailures(ctx, u.ID); err != nil {
			return err
		}
		s.audit(ctx, q, admin, "user.unlocked", &u, ip, nil)
		return nil
	})
}

// ResetTOTP turns someone's two-factor authentication off (a lost phone); with
// WFM_LOCAL_REQUIRE_2FA they set it up again at their next sign-in.
func (s *Service) ResetTOTP(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) error {
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		cred, err := q.GetLocalCredentials(ctx, u.ID)
		if err != nil || cred.TotpSecret == nil {
			return apperr.Invalid("userId", "%s has no two-factor authentication to reset.", u.DisplayName)
		}
		if err := q.DisableTOTP(ctx, u.ID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, u.ID); err != nil {
			return err
		}
		s.audit(ctx, q, admin, "user.totp_reset", &u, ip, nil)
		return s.securityEmail(ctx, tx, u, "Two-factor authentication turned off", "An administrator turned off two-factor authentication for your Workflow account", ip)
	})
}

// RemovePassword makes someone a single sign-on user again: password, two-factor, recovery
// codes, links and sessions go.
func (s *Service) RemovePassword(ctx context.Context, admin *authz.Actor, id uuid.UUID, ip string) error {
	return db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		u, err := s.person(ctx, q, id)
		if err != nil {
			return err
		}
		if _, err := q.GetLocalCredentials(ctx, u.ID); err != nil {
			return apperr.Invalid("userId", "%s is not a local account.", u.DisplayName)
		}
		for _, f := range []func(context.Context, uuid.UUID) error{q.DeleteLocalCredentials, q.DeleteRecoveryCodes, q.DeleteUserLoginChallenges, q.DeleteUserSessions} {
			if err := f(ctx, u.ID); err != nil {
				return err
			}
		}
		if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID}); err != nil {
			return err
		}
		s.audit(ctx, q, admin, "user.password_removed", &u, ip, nil)
		return nil
	})
}

// BootstrapAdmin makes someone an organisation admin with a local account and returns a
// temporary password, creating the person if needed. It is the way into an install without
// single sign-on (`wfm create-admin`); running it again issues a new temporary password.
func (s *Service) BootstrapAdmin(ctx context.Context, email, name string) (dbq.User, string, error) {
	if err := s.enabled(); err != nil {
		return dbq.User{}, "", err
	}
	email = normEmail(email)
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return dbq.User{}, "", apperr.Invalid("email", "a valid email address is required")
	}
	var u dbq.User
	err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		var err error
		u, err = q.GetUserByEmail(ctx, email)
		switch {
		case db.IsNoRows(err):
			if name = strings.TrimSpace(name); name == "" {
				name = email[:at]
			}
			if u, err = q.CreateUser(ctx, dbq.CreateUserParams{Kind: "human", Email: email, Username: email, DisplayName: name, OrgRole: "admin", Source: "admin", Active: true}); err != nil {
				return err
			}
			s.audit(ctx, q, nil, "user.create", &u, "", map[string]any{"email": email, "orgRole": "admin", "via": "wfm create-admin"})
			return nil
		case err != nil:
			return err
		}
		if _, err := s.person(ctx, q, u.ID); err != nil {
			return err
		}
		if !u.Active {
			if u, err = q.SetUserActive(ctx, dbq.SetUserActiveParams{ID: u.ID, Active: true}); err != nil {
				return err
			}
		}
		if u.OrgRole != "admin" {
			if u, err = q.SetUserOrgRole(ctx, dbq.SetUserOrgRoleParams{ID: u.ID, OrgRole: "admin"}); err != nil {
				return err
			}
			s.audit(ctx, q, nil, "user.role", &u, "", map[string]any{"orgRole": "admin", "via": "wfm create-admin"})
		}
		return nil
	})
	if err != nil {
		return u, "", err
	}
	pw, err := s.SetTemporaryPassword(ctx, authz.SystemActor(), u.ID, "")
	return u, pw, err
}

// Prune removes expired challenges and links older than a month (maintenance job).
func (s *Service) Prune(ctx context.Context) error {
	if _, err := s.Q.DeleteExpiredLoginChallenges(ctx); err != nil {
		return err
	}
	_, err := s.Q.PruneAccountTokens(ctx, time.Now().AddDate(0, -1, 0))
	return err
}
