// Package local implements local accounts: people an administrator adds who sign in with an
// email address and a password (argon2id), optionally with TOTP two-factor authentication,
// instead of (or as well as) single sign-on. Invitations and password resets are single-use
// signed links sent by email. Sign-in is throttled per client address and per address+email,
// and repeated failures lock the account for a while.
//
// The HTTP layer (internal/httpapi) turns a finished sign-in into the same server-side session
// as single sign-on; an unfinished one (second factor, required password change, required
// two-factor enrolment) is a challenge whose token the browser keeps in an HttpOnly cookie.
package local

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/mail"
	"github.com/geoffo-dev/workflow-management/internal/ratelimit"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// Sign-in stages after the password.
const (
	StageTOTP           = "totp"
	StagePasswordChange = "password_change"
	StageTOTPEnroll     = "totp_enroll"
)

const (
	InviteTTL           = 7 * 24 * time.Hour
	ResetTTL            = time.Hour
	challengeTTL        = 10 * time.Minute
	challengeAttempts   = 5
	recoveryCodeCount   = 10
	forgotMinDuration   = 300 * time.Millisecond
	ChallengeCookie     = "wfm_signin"
	ChallengeCookiePath = "/api/v1/auth/local"
)

// Service implements local accounts. Create it with New.
type Service struct {
	Pool *pgxpool.Pool
	Q    *dbq.Queries
	Cfg  *config.Config
	Box  *secure.Box
	Jobs jobs.Enqueuer
	Log  *slog.Logger
	// Params are the argon2id parameters for new hashes; older hashes are upgraded at sign-in.
	Params Params

	signInIP    *ratelimit.Limiter // every anonymous local-account request, per client address
	signInEmail *ratelimit.Limiter // password attempts per email and client address
	userLimit   *ratelimit.Limiter // password checks of a signed-in person (change password, disable 2FA)
	forgotEmail *ratelimit.Limiter // reset emails per address (silently dropped beyond)
	checks      *ratelimit.Limiter // strength checks while typing, per client address
	phantom     phantomLocks
}

func New(pool *pgxpool.Pool, cfg *config.Config, box *secure.Box, enq jobs.Enqueuer, log *slog.Logger) *Service {
	forgot := ratelimit.New(1, 3)
	forgot.Rate = 3.0 / (15 * 60) // three emails per address per 15 minutes
	return &Service{
		Pool: pool, Q: dbq.New(pool), Cfg: cfg, Box: box, Jobs: enq, Log: log, Params: DefaultParams,
		signInIP:    ratelimit.New(30, 30),
		signInEmail: ratelimit.New(5, 10),
		userLimit:   ratelimit.New(5, 10),
		forgotEmail: forgot,
		checks:      ratelimit.New(120, 60),
		phantom:     phantomLocks{m: map[string]*phantom{}},
	}
}

// ---------------------------------------------------------------------------
// Errors

// RetryError is a 429 that says when to try again; the HTTP layer sets Retry-After from it.
type RetryError struct {
	Err   *apperr.Error
	After time.Duration
}

func (e *RetryError) Error() string { return e.Err.Message }
func (e *RetryError) Unwrap() error { return e.Err }

func tooMany(wait time.Duration) error {
	return &RetryError{Err: apperr.New(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Wait a moment and try again."), After: wait}
}

func locked(wait time.Duration) error {
	mins := int(wait.Round(time.Minute).Minutes())
	return &RetryError{Err: apperr.New(http.StatusTooManyRequests, "locked",
		"Too many failed sign-in attempts. Try again in %d minute%s, or reset your password.", max(mins, 1), map[bool]string{true: "", false: "s"}[max(mins, 1) == 1]), After: wait}
}

func invalidCredentials() error {
	return apperr.New(http.StatusUnauthorized, "invalid_credentials", "Incorrect email or password.")
}

func invalidCode() error {
	return apperr.New(http.StatusUnauthorized, "invalid_code", "That code is not valid. Check your authenticator app and try again.")
}

func challengeExpired() error {
	return apperr.New(http.StatusUnauthorized, "sign_in_expired", "Your sign-in has expired. Please start again.")
}

func invalidLink() error {
	return apperr.New(http.StatusNotFound, "invalid_link", "This link is invalid, has already been used or has expired. Ask for a new one.")
}

func disabled() error {
	return apperr.New(http.StatusNotFound, "local_login_disabled", "Password sign-in is turned off (WFM_LOCAL_LOGIN=false).")
}

func (s *Service) enabled() error {
	if !s.Cfg.Local.Enabled {
		return disabled()
	}
	return nil
}

func allow(l *ratelimit.Limiter, key string) error {
	if ok, wait := l.Allow(key); !ok {
		return tooMany(wait)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers

// ClientIP is the address used for throttling and the audit log: the one the HTTP layer resolved
// (X-Forwarded-For counts only from trusted proxies, WFM_TRUSTED_PROXIES), else the peer address.
func ClientIP(r *http.Request) string {
	if ip := ratelimit.ClientIPFrom(r.Context()); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// Managed reports whether an identity provider owns the person's sign-in (no passwords).
func Managed(source string) bool {
	return source == "scim" || source == "google" || source == "keycloak"
}

func sourceName(source string) string {
	return map[string]string{"scim": "SCIM", "google": "Google directory sync", "keycloak": "Keycloak directory sync"}[source]
}

func (s *Service) issuer() string {
	if u, err := url.Parse(s.Cfg.PublicURL); err == nil && u.Host != "" {
		return "Workflow (" + u.Host + ")"
	}
	return "Workflow"
}

func (s *Service) seal(v string) *string {
	sealed := s.Box.Seal([]byte(v))
	return &sealed
}

func (s *Service) open(v *string) (string, error) {
	if v == nil {
		return "", errors.New("no secret")
	}
	b, err := s.Box.Open(*v)
	return string(b), err
}

func userActor(u dbq.User) *authz.Actor { return authz.UserActor(u, u.OrgRole) }

// audit records a security event with the client address (tracker.Audit has no address).
func (s *Service) audit(ctx context.Context, q *dbq.Queries, actor *authz.Actor, action string, target *dbq.User, ip string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	raw, _ := json.Marshal(data)
	p := dbq.InsertAuditParams{ActorType: "system", Action: action, Data: raw}
	if actor != nil && actor.User != nil {
		p.ActorType = "user"
		if actor.ViaToken() {
			p.ActorType = "token"
		}
		p.ActorID = &actor.User.ID
	}
	if target != nil {
		tt, id := "user", target.ID.String()
		p.TargetType, p.TargetID = &tt, &id
	}
	if ip != "" {
		p.Ip = &ip
	}
	if err := q.InsertAudit(ctx, p); err != nil {
		s.Log.Warn("audit failed", "action", action, "error", err)
	}
}

func (s *Service) email(ctx context.Context, tx pgx.Tx, e email) error {
	return mail.Queue(ctx, s.Jobs, tx, e.message())
}

func (s *Service) securityEmail(ctx context.Context, tx pgx.Tx, u dbq.User, subject, what, ip string) error {
	return s.email(ctx, tx, securityEmail(u.Email, u.DisplayName, subject, what, ip, time.Now(), s.Cfg.PublicURL))
}

// Acceptable applies the password policy for a person; the error names the field.
func Acceptable(field, password string, u dbq.User) error {
	st := CheckPassword(password, u.Email, u.DisplayName)
	if !st.OK() {
		return apperr.Invalid(field, "%s", strings.Join(st.Problems, " "))
	}
	return nil
}
