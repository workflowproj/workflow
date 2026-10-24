package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/identity/local"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// Local accounts: password sign-in, two-factor, invitation and reset links (see identity/local).

// maxSecretLen caps what is hashed; longer input can never match a stored password.
const maxSecretLen = 1024

func clip(s string) string {
	if len(s) > maxSecretLen {
		return s[:maxSecretLen]
	}
	return s
}

func requestIP(ctx context.Context) string {
	if _, r := httpFrom(ctx); r != nil {
		return local.ClientIP(r)
	}
	return ""
}

// localErr adds Retry-After to throttling and lockout errors.
func localErr(ctx context.Context, err error) error {
	var re *local.RetryError
	if errors.As(err, &re) {
		if w, _ := httpFrom(ctx); w != nil {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(re.After.Seconds())))))
		}
	}
	return err
}

func (s *Server) challengeCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: local.ChallengeCookie, Value: token, Path: local.ChallengeCookiePath, MaxAge: maxAge, HttpOnly: true,
		Secure: strings.HasPrefix(s.Cfg.PublicURL, "https://"), SameSite: http.SameSiteStrictMode,
	})
}

func challengeToken(ctx context.Context) string {
	if _, r := httpFrom(ctx); r != nil {
		if c, err := r.Cookie(local.ChallengeCookie); err == nil {
			return c.Value
		}
	}
	return ""
}

var stageStatus = map[string]api.LocalLoginResultStatus{
	local.StageTOTP:           api.LocalLoginResultStatusTotpRequired,
	local.StagePasswordChange: api.LocalLoginResultStatusPasswordChangeRequired,
	local.StageTOTPEnroll:     api.LocalLoginResultStatusTotpEnrollmentRequired,
}

// loginResult starts the session once a sign-in is complete (the same session as single
// sign-on), or keeps the unfinished sign-in in the challenge cookie.
func (s *Server) loginResult(ctx context.Context, st local.Step, err error) (api.LocalLoginResult, error) {
	if err != nil {
		return api.LocalLoginResult{}, localErr(ctx, err)
	}
	w, r := httpFrom(ctx)
	if st.Stage != "" {
		s.challengeCookie(w, st.Challenge, 600)
		return api.LocalLoginResult{Status: stageStatus[st.Stage]}, nil
	}
	s.challengeCookie(w, "", -1)
	if c, err := r.Cookie(identity.SessionCookie); err == nil {
		_ = s.Q.DeleteSession(ctx, secure.Hash(c.Value)) // whoever was signed in in this browser before
	}
	if err := s.Auth.StartSession(w, r, st.User); err != nil {
		return api.LocalLoginResult{}, err
	}
	role, err := s.Tracker.EffectiveOrgRole(ctx, st.User)
	if err != nil {
		return api.LocalLoginResult{}, err
	}
	me := meView(st.User, role)
	res := api.LocalLoginResult{Status: api.LocalLoginResultStatusSignedIn, Me: &me}
	if st.RecoveryCodes != nil {
		res.RecoveryCodes = &st.RecoveryCodes
	}
	return res, nil
}

func (s *Server) LocalLogin(ctx context.Context, req api.LocalLoginRequestObject) (api.LocalLoginResponseObject, error) {
	st, err := s.Local.Login(ctx, req.Body.Email, clip(req.Body.Password), requestIP(ctx))
	res, err := s.loginResult(ctx, st, err)
	return api.LocalLogin200JSONResponse(res), err
}

func (s *Server) LocalLoginTotp(ctx context.Context, req api.LocalLoginTotpRequestObject) (api.LocalLoginTotpResponseObject, error) {
	st, err := s.Local.LoginTOTP(ctx, challengeToken(ctx), clip(req.Body.Code), requestIP(ctx))
	res, err := s.loginResult(ctx, st, err)
	return api.LocalLoginTotp200JSONResponse(res), err
}

func (s *Server) LocalLoginNewPassword(ctx context.Context, req api.LocalLoginNewPasswordRequestObject) (api.LocalLoginNewPasswordResponseObject, error) {
	st, err := s.Local.LoginNewPassword(ctx, challengeToken(ctx), clip(req.Body.NewPassword), requestIP(ctx))
	res, err := s.loginResult(ctx, st, err)
	return api.LocalLoginNewPassword200JSONResponse(res), err
}

func totpView(t local.TOTPSetup) api.TotpSetup {
	return api.TotpSetup{Secret: t.Secret, OtpauthUri: t.URI, QrCode: t.QRCode}
}

func (s *Server) LocalLoginTotpSetup(ctx context.Context, _ api.LocalLoginTotpSetupRequestObject) (api.LocalLoginTotpSetupResponseObject, error) {
	t, err := s.Local.LoginTOTPSetup(ctx, challengeToken(ctx), requestIP(ctx))
	if err != nil {
		return nil, localErr(ctx, err)
	}
	return api.LocalLoginTotpSetup200JSONResponse(totpView(t)), nil
}

func (s *Server) LocalLoginTotpConfirm(ctx context.Context, req api.LocalLoginTotpConfirmRequestObject) (api.LocalLoginTotpConfirmResponseObject, error) {
	st, err := s.Local.LoginTOTPConfirm(ctx, challengeToken(ctx), clip(req.Body.Code), requestIP(ctx))
	res, err := s.loginResult(ctx, st, err)
	return api.LocalLoginTotpConfirm200JSONResponse(res), err
}

func (s *Server) LocalForgotPassword(ctx context.Context, req api.LocalForgotPasswordRequestObject) (api.LocalForgotPasswordResponseObject, error) {
	if err := s.Local.ForgotPassword(ctx, clip(req.Body.Email), requestIP(ctx)); err != nil {
		return nil, localErr(ctx, err)
	}
	return api.LocalForgotPassword202Response{}, nil
}

func linkView(l local.LinkInfo) api.LocalLink {
	v := api.LocalLink{Purpose: api.LocalLinkPurpose(l.Purpose), Email: l.Email, DisplayName: l.DisplayName}
	if !l.ExpiresAt.IsZero() {
		v.ExpiresAt = &l.ExpiresAt
	}
	return v
}

func (s *Server) LocalLinkInfo(ctx context.Context, req api.LocalLinkInfoRequestObject) (api.LocalLinkInfoResponseObject, error) {
	l, err := s.Local.Link(ctx, clip(req.Body.Token), requestIP(ctx))
	if err != nil {
		return nil, localErr(ctx, err)
	}
	return api.LocalLinkInfo200JSONResponse(linkView(l)), nil
}

func (s *Server) LocalRedeemLink(ctx context.Context, req api.LocalRedeemLinkRequestObject) (api.LocalRedeemLinkResponseObject, error) {
	l, err := s.Local.Redeem(ctx, clip(req.Body.Token), clip(req.Body.Password), requestIP(ctx))
	if err != nil {
		return nil, localErr(ctx, err)
	}
	return api.LocalRedeemLink200JSONResponse(linkView(l)), nil
}

func (s *Server) LocalCheckPassword(ctx context.Context, req api.LocalCheckPasswordRequestObject) (api.LocalCheckPasswordResponseObject, error) {
	st, err := s.Local.CheckPassword(clip(req.Body.Password), deref(req.Body.Email), deref(req.Body.DisplayName), requestIP(ctx))
	if err != nil {
		return nil, localErr(ctx, err)
	}
	return api.LocalCheckPassword200JSONResponse{Score: st.Score, Label: st.Label(), Problems: st.Problems, Acceptable: st.OK()}, nil
}

// ---------------------------------------------------------------------------
// The signed-in person

// sessionActor is the signed-in person on a browser session; tokens cannot change how
// someone signs in.
func sessionActor(ctx context.Context) (dbq.User, error) {
	a, err := actor(ctx)
	if err != nil {
		return dbq.User{}, err
	}
	if a.TokenID != nil {
		return dbq.User{}, apperr.Forbidden("tokens cannot change sign-in settings; use the web app")
	}
	return *a.User, nil
}

func (s *Server) GetMySecurity(ctx context.Context, _ api.GetMySecurityRequestObject) (api.GetMySecurityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sec, err := s.Local.Security(ctx, *a.User)
	if err != nil {
		return nil, err
	}
	return api.GetMySecurity200JSONResponse{
		LocalLoginEnabled: s.Cfg.Local.Enabled, LocalAccount: sec.LocalAccount, PasswordChangedAt: sec.PasswordChangedAt,
		TotpEnabled: sec.TOTPEnabled, TotpEnabledAt: sec.TOTPEnabledAt, RecoveryCodesRemaining: sec.RecoveryCodesRemaining,
		Require2fa: s.Cfg.Local.Require2FA,
	}, nil
}

func (s *Server) ChangeMyPassword(ctx context.Context, req api.ChangeMyPasswordRequestObject) (api.ChangeMyPasswordResponseObject, error) {
	u, err := sessionActor(ctx)
	if err != nil {
		return nil, err
	}
	keep := ""
	if _, r := httpFrom(ctx); r != nil {
		if c, err := r.Cookie(identity.SessionCookie); err == nil {
			keep = secure.Hash(c.Value)
		}
	}
	if err := s.Local.ChangePassword(ctx, u, clip(req.Body.CurrentPassword), clip(req.Body.NewPassword), keep, requestIP(ctx)); err != nil {
		return nil, localErr(ctx, err)
	}
	return api.ChangeMyPassword204Response{}, nil
}

func (s *Server) SetupMyTotp(ctx context.Context, _ api.SetupMyTotpRequestObject) (api.SetupMyTotpResponseObject, error) {
	u, err := sessionActor(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.Local.BeginTOTP(ctx, u)
	if err != nil {
		return nil, err
	}
	return api.SetupMyTotp200JSONResponse(totpView(t)), nil
}

func (s *Server) ConfirmMyTotp(ctx context.Context, req api.ConfirmMyTotpRequestObject) (api.ConfirmMyTotpResponseObject, error) {
	u, err := sessionActor(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := s.Local.ConfirmTOTP(ctx, u, clip(req.Body.Code), requestIP(ctx))
	if err != nil {
		return nil, localErr(ctx, err)
	}
	return api.ConfirmMyTotp200JSONResponse{Codes: codes}, nil
}

func (s *Server) DisableMyTotp(ctx context.Context, req api.DisableMyTotpRequestObject) (api.DisableMyTotpResponseObject, error) {
	u, err := sessionActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Local.DisableTOTP(ctx, u, clip(req.Body.Password), clip(req.Body.Code), requestIP(ctx)); err != nil {
		return nil, localErr(ctx, err)
	}
	return api.DisableMyTotp204Response{}, nil
}

func (s *Server) RegenerateMyRecoveryCodes(ctx context.Context, req api.RegenerateMyRecoveryCodesRequestObject) (api.RegenerateMyRecoveryCodesResponseObject, error) {
	u, err := sessionActor(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := s.Local.RegenerateRecoveryCodes(ctx, u, clip(req.Body.Code), requestIP(ctx))
	if err != nil {
		return nil, localErr(ctx, err)
	}
	return api.RegenerateMyRecoveryCodes200JSONResponse{Codes: codes}, nil
}

// ---------------------------------------------------------------------------
// Administration

// localStatuses describes which of the users are local accounts, for the user list.
func (s *Server) localStatuses(ctx context.Context, users []dbq.User) map[uuid.UUID]*api.LocalAccountStatus {
	sts, err := s.Local.Statuses(ctx, users)
	if err != nil {
		s.Log.Warn("local account status", "error", err)
		return nil
	}
	out := make(map[uuid.UUID]*api.LocalAccountStatus, len(sts))
	for id, st := range sts {
		v := &api.LocalAccountStatus{Status: api.LocalAccountStatusStatusActive, TotpEnabled: st.TOTPEnabled, LockedUntil: st.LockedUntil, PasswordChangedAt: st.PasswordChangedAt}
		switch {
		case !st.HasPassword:
			v.Status, v.InvitationExpiresAt = api.LocalAccountStatusStatusInvited, st.InvitationExpiresAt
		case st.MustChange:
			v.Status = api.LocalAccountStatusStatusPasswordChangeRequired
		}
		out[id] = v
	}
	return out
}

func (s *Server) AdminInviteUser(ctx context.Context, req api.AdminInviteUserRequestObject) (api.AdminInviteUserResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	exp, err := s.Local.Invite(ctx, a, req.UserId, requestIP(ctx))
	if err != nil {
		return nil, err
	}
	return api.AdminInviteUser200JSONResponse{ExpiresAt: exp}, nil
}

func (s *Server) AdminRevokeInvitation(ctx context.Context, req api.AdminRevokeInvitationRequestObject) (api.AdminRevokeInvitationResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Local.RevokeInvitation(ctx, a, req.UserId, requestIP(ctx)); err != nil {
		return nil, err
	}
	return api.AdminRevokeInvitation204Response{}, nil
}

func (s *Server) AdminSetTemporaryPassword(ctx context.Context, req api.AdminSetTemporaryPasswordRequestObject) (api.AdminSetTemporaryPasswordResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	pw, err := s.Local.SetTemporaryPassword(ctx, a, req.UserId, requestIP(ctx))
	if err != nil {
		return nil, err
	}
	return api.AdminSetTemporaryPassword200JSONResponse{Password: pw}, nil
}

func (s *Server) AdminSendPasswordReset(ctx context.Context, req api.AdminSendPasswordResetRequestObject) (api.AdminSendPasswordResetResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Local.SendPasswordReset(ctx, a, req.UserId, requestIP(ctx)); err != nil {
		return nil, err
	}
	return api.AdminSendPasswordReset202Response{}, nil
}

func (s *Server) AdminUnlockUser(ctx context.Context, req api.AdminUnlockUserRequestObject) (api.AdminUnlockUserResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Local.Unlock(ctx, a, req.UserId, requestIP(ctx)); err != nil {
		return nil, err
	}
	return api.AdminUnlockUser204Response{}, nil
}

func (s *Server) AdminResetTotp(ctx context.Context, req api.AdminResetTotpRequestObject) (api.AdminResetTotpResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Local.ResetTOTP(ctx, a, req.UserId, requestIP(ctx)); err != nil {
		return nil, err
	}
	return api.AdminResetTotp204Response{}, nil
}

func (s *Server) AdminRemovePassword(ctx context.Context, req api.AdminRemovePasswordRequestObject) (api.AdminRemovePasswordResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Local.RemovePassword(ctx, a, req.UserId, requestIP(ctx)); err != nil {
		return nil, err
	}
	return api.AdminRemovePassword204Response{}, nil
}
