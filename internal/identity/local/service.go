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
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
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

// ClientIP is the address used for throttling and the audit log: the address the nearest proxy
// saw (the last X-Forwarded-For entry, which a client cannot forge past an appending proxy),
// else the peer address.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// Managed reports whether an identity provider owns the person's sign-in (no passwords).
func Managed(source string) bool { return source == "scim" || source == "google" }

func sourceName(source string) string {
	return map[string]string{"scim": "SCIM", "google": "Google directory sync"}[source]
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
		if actor.TokenID != nil {
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

// ---------------------------------------------------------------------------
// Sign-in

// Step is where a password sign-in stands.
type Step struct {
	// Stage is empty once the person is signed in; otherwise StageTOTP, StagePasswordChange or
	// StageTOTPEnroll says what they must do next.
	Stage string
	User  dbq.User
	// Challenge identifies an unfinished sign-in (Stage set); the browser keeps it in a cookie.
	Challenge string
	// RecoveryCodes are shown once, after two-factor enrolment during sign-in.
	RecoveryCodes []string
}

type account struct {
	user dbq.User
	cred dbq.LocalCredential
}

func (a account) lockedFor(now time.Time) time.Duration {
	if a.cred.LockedUntil != nil && a.cred.LockedUntil.After(now) {
		return a.cred.LockedUntil.Sub(now)
	}
	return 0
}

// find returns the local account for an email: a person (not a service account), not deleted,
// not owned by an identity provider, with local credentials.
func (s *Service) find(ctx context.Context, email string) (account, bool, error) {
	u, err := s.Q.GetUserByEmail(ctx, email)
	if db.IsNoRows(err) {
		return account{}, false, nil
	} else if err != nil {
		return account{}, false, err
	}
	if u.Kind != "human" || u.DeletedAt != nil || Managed(u.Source) {
		return account{}, false, nil
	}
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if db.IsNoRows(err) {
		return account{}, false, nil
	} else if err != nil {
		return account{}, false, err
	}
	return account{user: u, cred: cred}, true, nil
}

// Login checks an email and password. Every wrong answer looks the same whether or not the
// account exists, takes as long, and counts towards a lockout.
func (s *Service) Login(ctx context.Context, email, password, ip string) (Step, error) {
	if err := s.enabled(); err != nil {
		return Step{}, err
	}
	email = normEmail(email)
	if err := allow(s.signInIP, ip); err != nil {
		return Step{}, err
	}
	if err := allow(s.signInEmail, email+"|"+ip); err != nil {
		return Step{}, err
	}
	now := time.Now()
	acc, found, err := s.find(ctx, email)
	if err != nil {
		return Step{}, err
	}
	if !found {
		if wait := s.phantom.lockedFor(email, now); wait > 0 {
			return Step{}, locked(wait)
		}
		VerifyDummy(password)
		s.audit(ctx, s.Q, nil, "user.login_failed", nil, ip, map[string]any{"method": "password", "email": email, "reason": "unknown_account"})
		if s.phantom.fail(email, now, s.Cfg.Local.LockoutAttempts, s.Cfg.Local.LockoutDuration) {
			return Step{}, locked(s.Cfg.Local.LockoutDuration)
		}
		return Step{}, invalidCredentials()
	}
	if wait := acc.lockedFor(now); wait > 0 {
		return Step{}, locked(wait)
	}
	ok, rehash := false, false
	if acc.cred.PasswordHash != nil {
		ok, rehash = VerifyPassword(password, *acc.cred.PasswordHash, s.Params)
	} else {
		VerifyDummy(password) // invited, no password yet
	}
	if !ok {
		return Step{}, s.failed(ctx, acc.user, "password", ip, invalidCredentials())
	}
	if !acc.user.Active {
		s.audit(ctx, s.Q, nil, "user.login_failed", &acc.user, ip, map[string]any{"method": "password", "email": email, "reason": "deactivated"})
		return Step{}, apperr.New(http.StatusForbidden, "deactivated", "This account is deactivated. Ask an administrator to reactivate it.")
	}
	if rehash {
		if h, err := HashPassword(password, s.Params); err == nil {
			_ = s.Q.RehashLocalPassword(ctx, dbq.RehashLocalPasswordParams{UserID: acc.user.ID, PasswordHash: &h})
		}
	}
	s.signInEmail.Reset(email + "|" + ip)
	return s.advance(ctx, acc.user, acc.cred, "", false, ip)
}

// failed counts a failed attempt for a person, locks the account at the threshold and audits.
func (s *Service) failed(ctx context.Context, u dbq.User, method, ip string, err error) error {
	s.audit(ctx, s.Q, nil, "user.login_failed", &u, ip, map[string]any{"method": method, "email": u.Email})
	if s.Cfg.Local.LockoutAttempts <= 0 {
		return err
	}
	now := time.Now()
	n, qerr := s.Q.RecordLoginFailure(ctx, dbq.RecordLoginFailureParams{UserID: u.ID, WindowStart: now.Add(-s.Cfg.Local.LockoutDuration)})
	if qerr != nil || int(n) < s.Cfg.Local.LockoutAttempts {
		return err
	}
	until := now.Add(s.Cfg.Local.LockoutDuration)
	if lerr := s.Q.LockLocalAccount(ctx, dbq.LockLocalAccountParams{UserID: u.ID, LockedUntil: &until}); lerr != nil {
		return err
	}
	_ = s.Q.DeleteUserLoginChallenges(ctx, u.ID)
	s.audit(ctx, s.Q, nil, "user.locked", &u, ip, map[string]any{"until": until, "failures": n})
	return locked(s.Cfg.Local.LockoutDuration)
}

// advance moves a sign-in to its next stage, or finishes it. Order: second factor, then a
// required password change, then required two-factor enrolment.
func (s *Service) advance(ctx context.Context, u dbq.User, cred dbq.LocalCredential, challenge string, totpDone bool, ip string) (Step, error) {
	stage := ""
	switch {
	case cred.TotpSecret != nil && !totpDone:
		stage = StageTOTP
	case cred.MustChange:
		stage = StagePasswordChange
	case s.Cfg.Local.Require2FA && cred.TotpSecret == nil:
		stage = StageTOTPEnroll
	}
	if stage == "" {
		if challenge != "" {
			_ = s.Q.DeleteLoginChallenge(ctx, secure.Hash(challenge))
		}
		if err := s.Q.ClearLoginFailures(ctx, u.ID); err != nil {
			return Step{}, err
		}
		s.audit(ctx, s.Q, userActor(u), "user.login", &u, ip, map[string]any{"method": "password", "twoFactor": cred.TotpSecret != nil})
		return Step{User: u}, nil
	}
	if challenge == "" {
		challenge = secure.Token("", 32)
		if err := s.Q.CreateLoginChallenge(ctx, dbq.CreateLoginChallengeParams{ID: secure.Hash(challenge), UserID: u.ID, Stage: stage, ExpiresAt: time.Now().Add(challengeTTL)}); err != nil {
			return Step{}, err
		}
	} else if err := s.Q.SetLoginChallengeStage(ctx, dbq.SetLoginChallengeStageParams{ID: secure.Hash(challenge), Stage: stage}); err != nil {
		return Step{}, err
	}
	return Step{Stage: stage, User: u, Challenge: challenge}, nil
}

// challenge loads an unfinished sign-in at the expected stage. count spends one of its attempts.
func (s *Service) challenge(ctx context.Context, token, stage, ip string, count bool) (account, error) {
	if err := s.enabled(); err != nil {
		return account{}, err
	}
	if err := allow(s.signInIP, ip); err != nil {
		return account{}, err
	}
	if token == "" {
		return account{}, challengeExpired()
	}
	id := secure.Hash(token)
	ch, err := s.Q.GetLoginChallenge(ctx, id)
	if err != nil || ch.Stage != stage {
		return account{}, challengeExpired()
	}
	u, err := s.Q.GetUser(ctx, ch.UserID)
	if err != nil {
		return account{}, challengeExpired()
	}
	cred, err := s.Q.GetLocalCredentials(ctx, u.ID)
	if err != nil || !u.Active || u.DeletedAt != nil || Managed(u.Source) {
		_ = s.Q.DeleteLoginChallenge(ctx, id)
		return account{}, challengeExpired()
	}
	acc := account{user: u, cred: cred}
	if wait := acc.lockedFor(time.Now()); wait > 0 {
		_ = s.Q.DeleteLoginChallenge(ctx, id)
		return account{}, locked(wait)
	}
	if count {
		n, err := s.Q.CountLoginChallengeAttempt(ctx, id)
		if err != nil {
			return account{}, err
		}
		if n > challengeAttempts {
			_ = s.Q.DeleteLoginChallenge(ctx, id)
			return account{}, apperr.New(http.StatusUnauthorized, "sign_in_expired", "Too many attempts. Please start signing in again.")
		}
	}
	return acc, nil
}

// secondFactor accepts a current TOTP code (once) or an unused recovery code.
func (s *Service) secondFactor(ctx context.Context, q *dbq.Queries, acc account, code string) (method string, ok bool, err error) {
	if LooksLikeTOTP(code) {
		secret, err := s.open(acc.cred.TotpSecret)
		if err != nil {
			return "totp", false, nil
		}
		step, valid := VerifyTOTP(secret, code, time.Now())
		if !valid {
			return "totp", false, nil
		}
		n, err := q.UseTOTPStep(ctx, dbq.UseTOTPStepParams{UserID: acc.user.ID, Step: &step})
		return "totp", n == 1, err
	}
	if strings.TrimSpace(code) == "" {
		return "recovery_code", false, nil
	}
	n, err := q.UseRecoveryCode(ctx, dbq.UseRecoveryCodeParams{UserID: acc.user.ID, CodeHash: HashRecoveryCode(code)})
	return "recovery_code", n == 1, err
}

// LoginTOTP completes the second factor with an authenticator code or a recovery code.
func (s *Service) LoginTOTP(ctx context.Context, token, code, ip string) (Step, error) {
	acc, err := s.challenge(ctx, token, StageTOTP, ip, true)
	if err != nil {
		return Step{}, err
	}
	method, ok, err := s.secondFactor(ctx, s.Q, acc, code)
	if err != nil {
		return Step{}, err
	}
	if !ok {
		return Step{}, s.failed(ctx, acc.user, method, ip, invalidCode())
	}
	if method == "recovery_code" {
		left, _ := s.Q.CountRecoveryCodes(ctx, acc.user.ID)
		s.audit(ctx, s.Q, userActor(acc.user), "user.recovery_code_used", &acc.user, ip, map[string]any{"remaining": left})
		if err := s.securityEmail(ctx, nil, acc.user, "A recovery code was used to sign in", fmt.Sprintf("A recovery code was used to sign in to your account (%d left)", left), ip); err != nil {
			s.Log.Warn("queue email", "error", err)
		}
	}
	return s.advance(ctx, acc.user, acc.cred, token, true, ip)
}

// LoginNewPassword replaces a temporary password during sign-in.
func (s *Service) LoginNewPassword(ctx context.Context, token, newPassword, ip string) (Step, error) {
	acc, err := s.challenge(ctx, token, StagePasswordChange, ip, true)
	if err != nil {
		return Step{}, err
	}
	if err := Acceptable("newPassword", newPassword, acc.user); err != nil {
		return Step{}, err
	}
	if acc.cred.PasswordHash != nil {
		if same, _ := VerifyPassword(newPassword, *acc.cred.PasswordHash, s.Params); same {
			return Step{}, apperr.Invalid("newPassword", "Choose a password different from the temporary one.")
		}
	}
	cred, err := s.setPassword(ctx, acc.user, newPassword, "", ip, "user.password_changed", map[string]any{"reason": "temporary_password_replaced"})
	if err != nil {
		return Step{}, err
	}
	return s.advance(ctx, acc.user, cred, token, true, ip)
}

// setPassword stores a new password chosen by its owner, ends their sessions (except keep, a
// session id), closes outstanding links and sends a "password changed" email.
func (s *Service) setPassword(ctx context.Context, u dbq.User, password, keep, ip, action string, data map[string]any) (dbq.LocalCredential, error) {
	h, err := HashPassword(password, s.Params)
	if err != nil {
		return dbq.LocalCredential{}, err
	}
	var cred dbq.LocalCredential
	err = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if cred, err = q.SetLocalPassword(ctx, dbq.SetLocalPasswordParams{UserID: u.ID, PasswordHash: &h, MustChange: false}); err != nil {
			return err
		}
		if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID}); err != nil {
			return err
		}
		if err := q.DeleteUserSessionsExcept(ctx, dbq.DeleteUserSessionsExceptParams{UserID: u.ID, KeepID: keep}); err != nil {
			return err
		}
		s.audit(ctx, q, userActor(u), action, &u, ip, data)
		return s.securityEmail(ctx, tx, u, "Your password was changed", "The password of your Workflow account was changed", ip)
	})
	return cred, err
}

// TOTPSetup is what someone needs to add the account to an authenticator app.
type TOTPSetup struct {
	Secret string
	URI    string
	QRCode string // data: URI of an SVG image
}

func (s *Service) beginTOTP(ctx context.Context, u dbq.User) (TOTPSetup, error) {
	secret := NewTOTPSecret()
	if err := s.Q.SetTOTPPending(ctx, dbq.SetTOTPPendingParams{UserID: u.ID, TotpPending: s.seal(secret)}); err != nil {
		return TOTPSetup{}, err
	}
	uri := OTPAuthURI(s.issuer(), u.Email, secret)
	qr, err := QRCodeDataURI(uri)
	if err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: secret, URI: uri, QRCode: qr}, nil
}

// confirmTOTP turns two-factor on with the pending secret once a code from it checks out, and
// returns fresh recovery codes.
func (s *Service) confirmTOTP(ctx context.Context, acc account, code, ip string) ([]string, error) {
	secret, err := s.open(acc.cred.TotpPending)
	if err != nil {
		return nil, apperr.Invalid("code", "Start setting up two-factor authentication again.")
	}
	step, ok := VerifyTOTP(secret, code, time.Now())
	if !ok {
		return nil, apperr.Invalid("code", "That code is not valid. Check the time on your device and try the current code.")
	}
	codes := NewRecoveryCodes(recoveryCodeCount)
	err = db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.EnableTOTP(ctx, dbq.EnableTOTPParams{UserID: acc.user.ID, TotpSecret: s.seal(secret), Step: &step}); err != nil {
			return err
		}
		if err := s.replaceRecoveryCodes(ctx, q, acc.user.ID, codes); err != nil {
			return err
		}
		s.audit(ctx, q, userActor(acc.user), "user.totp_enabled", &acc.user, ip, nil)
		return s.securityEmail(ctx, tx, acc.user, "Two-factor authentication turned on", "Two-factor authentication was turned on for your Workflow account", ip)
	})
	return codes, err
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, q *dbq.Queries, userID uuid.UUID, codes []string) error {
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = HashRecoveryCode(c)
	}
	return q.InsertRecoveryCodes(ctx, dbq.InsertRecoveryCodesParams{UserID: userID, CodeHashes: hashes})
}

// LoginTOTPSetup starts the two-factor enrolment that WFM_LOCAL_REQUIRE_2FA demands at sign-in.
func (s *Service) LoginTOTPSetup(ctx context.Context, token, ip string) (TOTPSetup, error) {
	acc, err := s.challenge(ctx, token, StageTOTPEnroll, ip, false)
	if err != nil {
		return TOTPSetup{}, err
	}
	return s.beginTOTP(ctx, acc.user)
}

// LoginTOTPConfirm finishes enrolment during sign-in; the step carries the recovery codes.
func (s *Service) LoginTOTPConfirm(ctx context.Context, token, code, ip string) (Step, error) {
	acc, err := s.challenge(ctx, token, StageTOTPEnroll, ip, true)
	if err != nil {
		return Step{}, err
	}
	codes, err := s.confirmTOTP(ctx, acc, code, ip)
	if err != nil {
		return Step{}, err
	}
	acc.cred.TotpSecret = acc.cred.TotpPending
	st, err := s.advance(ctx, acc.user, acc.cred, token, true, ip)
	st.RecoveryCodes = codes
	return st, err
}

// ---------------------------------------------------------------------------
// The signed-in person (Profile)

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
	if err := allow(s.checks, ip); err != nil {
		return Strength{}, err
	}
	return CheckPassword(password, email, name), nil
}

// ---------------------------------------------------------------------------
// Invitation and reset links

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

// ---------------------------------------------------------------------------
// Administration

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

// Prune removes expired challenges and links older than a month (maintenance job).
func (s *Service) Prune(ctx context.Context) error {
	if _, err := s.Q.DeleteExpiredLoginChallenges(ctx); err != nil {
		return err
	}
	_, err := s.Q.PruneAccountTokens(ctx, time.Now().AddDate(0, -1, 0))
	return err
}

// ---------------------------------------------------------------------------
// Lockouts for addresses without an account

// phantomLocks mirrors the database lockout for emails that have no local account, so that
// "too many attempts" cannot reveal which addresses have one. In memory, per process.
type phantomLocks struct {
	mu    sync.Mutex
	m     map[string]*phantom
	swept time.Time
}

type phantom struct {
	count int
	last  time.Time
	until time.Time
}

func (p *phantomLocks) lockedFor(key string, now time.Time) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ph := p.m[key]; ph != nil && ph.until.After(now) {
		return ph.until.Sub(now)
	}
	return 0
}

// fail counts a failure and reports whether it locked the address, as the threshold failure
// on a real account answers "locked" too.
func (p *phantomLocks) fail(key string, now time.Time, attempts int, window time.Duration) bool {
	if attempts <= 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.Sub(p.swept) > window {
		p.swept = now
		for k, ph := range p.m {
			if now.Sub(ph.last) > window && !ph.until.After(now) {
				delete(p.m, k)
			}
		}
	}
	ph := p.m[key]
	if ph == nil || now.Sub(ph.last) > window {
		ph = &phantom{}
		p.m[key] = ph
	}
	ph.count++
	ph.last = now
	if ph.count >= attempts {
		ph.until, ph.count = now.Add(window), 0
		return true
	}
	return false
}
