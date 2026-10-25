package local

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

// Security summarises how a person signs in.
type Security struct {
	LocalAccount           bool // has a password
	PasswordChangedAt      *time.Time
	TOTPEnabled            bool
	TOTPEnabledAt          *time.Time
	RecoveryCodesRemaining int
}

func (s *Service) Security(ctx context.Context, u dbq.User) (Security, error) {
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if db.IsNoRows(err) || (err == nil && (cred.PasswordHash == nil || Managed(u.Source))) {
		return Security{}, nil
	} else if err != nil {
		return Security{}, err
	}
	out := Security{LocalAccount: true, PasswordChangedAt: cred.PasswordChangedAt, TOTPEnabled: cred.TotpSecret != nil, TOTPEnabledAt: cred.TotpEnabledAt}
	if out.TOTPEnabled {
		n, err := s.Q.CountRecoveryCodes(ctx, u.ID)
		if err != nil {
			return out, err
		}
		out.RecoveryCodesRemaining = int(n)
	}
	return out, nil
}

// own loads the signed-in person's credentials and checks the password they typed.
func (s *Service) own(ctx context.Context, u dbq.User, password, field, ip string) (account, error) {
	if err := s.enabled(); err != nil {
		return account{}, err
	}
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if err != nil || cred.PasswordHash == nil || Managed(u.Source) {
		return account{}, apperr.New(http.StatusBadRequest, "no_password", "You sign in with single sign-on; your account has no password here.")
	}
	if err := allow(s.userLimit, u.ID.String()); err != nil {
		return account{}, err
	}
	acc := account{user: u, cred: cred}
	if wait := acc.lockedFor(time.Now()); wait > 0 {
		return account{}, locked(wait)
	}
	if ok, _ := VerifyPassword(password, *cred.PasswordHash, s.Params); !ok {
		return account{}, s.failed(ctx, u, "password", ip, apperr.Invalid(field, "Your password is incorrect."))
	}
	return acc, nil
}

// ChangePassword changes the signed-in person's password and ends their other sessions
// (keepSession is the current session's id).
func (s *Service) ChangePassword(ctx context.Context, u dbq.User, current, next, keepSession, ip string) error {
	if _, err := s.own(ctx, u, current, "currentPassword", ip); err != nil {
		return err
	}
	if err := Acceptable("newPassword", next, u); err != nil {
		return err
	}
	if next == current {
		return apperr.Invalid("newPassword", "Choose a password different from your current one.")
	}
	_, err := s.setPassword(ctx, u, next, keepSession, ip, "user.password_changed", nil)
	return err
}

// BeginTOTP starts two-factor enrolment from Profile.
func (s *Service) BeginTOTP(ctx context.Context, u dbq.User) (TOTPSetup, error) {
	if err := s.enabled(); err != nil {
		return TOTPSetup{}, err
	}
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if err != nil || cred.PasswordHash == nil || Managed(u.Source) {
		return TOTPSetup{}, apperr.New(http.StatusBadRequest, "no_password", "Two-factor authentication here is for password sign-in; your identity provider handles yours.")
	}
	if cred.TotpSecret != nil {
		return TOTPSetup{}, apperr.Conflict("totp_enabled", "Two-factor authentication is already on.")
	}
	return s.beginTOTP(ctx, u)
}

// ConfirmTOTP turns two-factor on from Profile and returns the recovery codes.
func (s *Service) ConfirmTOTP(ctx context.Context, u dbq.User, code, ip string) ([]string, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if err := allow(s.userLimit, u.ID.String()); err != nil {
		return nil, err
	}
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if err != nil || cred.PasswordHash == nil {
		return nil, apperr.New(http.StatusBadRequest, "no_password", "Your account has no password here.")
	}
	if cred.TotpSecret != nil {
		return nil, apperr.Conflict("totp_enabled", "Two-factor authentication is already on.")
	}
	return s.confirmTOTP(ctx, account{user: u, cred: cred}, code, ip)
}

// DisableTOTP turns two-factor off; it needs the password and a code (or a recovery code).
func (s *Service) DisableTOTP(ctx context.Context, u dbq.User, password, code, ip string) error {
	if s.Cfg.Local.Require2FA {
		return apperr.New(http.StatusBadRequest, "two_factor_required", "Your organisation requires two-factor authentication.")
	}
	acc, err := s.own(ctx, u, password, "password", ip)
	if err != nil {
		return err
	}
	if acc.cred.TotpSecret == nil {
		return apperr.Conflict("totp_disabled", "Two-factor authentication is already off.")
	}
	if _, ok, err := s.secondFactor(ctx, s.Q, acc, code); err != nil {
		return err
	} else if !ok {
		return s.failed(ctx, u, "totp", ip, apperr.Invalid("code", "That code is not valid."))
	}
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DisableTOTP(ctx, u.ID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, u.ID); err != nil {
			return err
		}
		s.audit(ctx, q, userActor(u), "user.totp_disabled", &u, ip, nil)
		return s.securityEmail(ctx, tx, u, "Two-factor authentication turned off", "Two-factor authentication was turned off for your Workflow account", ip)
	})
}

// RegenerateRecoveryCodes replaces the recovery codes; it needs a current code.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, u dbq.User, code, ip string) ([]string, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if err := allow(s.userLimit, u.ID.String()); err != nil {
		return nil, err
	}
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if err != nil || cred.TotpSecret == nil {
		return nil, apperr.Conflict("totp_disabled", "Turn on two-factor authentication first.")
	}
	acc := account{user: u, cred: cred}
	if _, ok, err := s.secondFactor(ctx, s.Q, acc, code); err != nil {
		return nil, err
	} else if !ok {
		return nil, s.failed(ctx, u, "totp", ip, apperr.Invalid("code", "That code is not valid."))
	}
	codes := NewRecoveryCodes(recoveryCodeCount)
	err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		if err := s.replaceRecoveryCodes(ctx, q, u.ID, codes); err != nil {
			return err
		}
		s.audit(ctx, q, userActor(u), "user.recovery_codes_regenerated", &u, ip, nil)
		return nil
	})
	return codes, err
}

// CheckPassword rates a password while someone types it (throttled per client address).
func (s *Service) CheckPassword(password, email, name, ip string) (Strength, error) {
	if err := s.enabled(); err != nil {
		return Strength{}, err
	}
	if err := allow(s.checks, ip); err != nil {
		return Strength{}, err
	}
	return CheckPassword(password, email, name), nil
}
