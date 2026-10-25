package local

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/secure"
)

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
