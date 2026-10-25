package integration

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/identity/local"
	"github.com/geoffo-dev/workflow-management/internal/mail"
)

// ---------------------------------------------------------------------------
// Helpers for local accounts

// mailTo runs queued jobs and returns the emails sent to an address, oldest first.
func (e *Env) mailTo(addr string) []mail.Sent {
	e.t.Helper()
	e.DrainJobs()
	return e.App.Mailer.(*mail.Memory).Messages(addr)
}

var linkRe = regexp.MustCompile(`https?://\S+/(invite|reset-password)\?token=(\S+)`)

// linkToken extracts the token of the invitation or reset link in an email.
func linkToken(t *testing.T, m mail.Sent, path string) string {
	t.Helper()
	match := linkRe.FindStringSubmatch(m.Text)
	if match == nil || match[1] != path {
		t.Fatalf("no /%s link in %q:\n%s", path, m.Subject, m.Text)
	}
	tok, err := url.QueryUnescape(match[2])
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// passwordLogin posts email and password, optionally from a given client address.
func (c *Client) passwordLogin(email, password string, ip ...string) Response {
	c.e.t.Helper()
	if len(ip) > 0 {
		return c.do("POST", "/api/v1/auth/local/login", obj{"email": email, "password": password}, "X-Forwarded-For", ip[0])
	}
	return c.do("POST", "/api/v1/auth/local/login", obj{"email": email, "password": password})
}

func wantStatus(t *testing.T, r Response, status int, loginStatus string) obj {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d, want %d: %s", r.Status, status, r.Body)
	}
	body := r.JSON()
	if loginStatus != "" && str(body, "status") != loginStatus {
		t.Fatalf("login status %q, want %q: %s", str(body, "status"), loginStatus, r.Body)
	}
	return body
}

func wantCode(t *testing.T, r Response, status int, code string) {
	t.Helper()
	if r.Status != status || str(r.JSON(), "code") != code {
		t.Fatalf("got %d %s, want %d with code %q", r.Status, r.Body, status, code)
	}
}

func totpNow(t *testing.T, secret string, offset time.Duration) string {
	t.Helper()
	code, err := local.TOTPCode(secret, time.Now().Add(offset))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// adminUser finds a user in the administration list.
func (c *Client) adminUser(email string) obj {
	c.e.t.Helper()
	var users []obj
	c.get("/api/v1/admin/users?q="+url.QueryEscape(email), &users)
	for _, u := range users {
		if str(u, "email") == email {
			return u
		}
	}
	c.e.t.Fatalf("user %s not listed", email)
	return nil
}

func (c *Client) auditActions() map[string]int {
	var events []obj
	c.get("/api/v1/admin/audit?limit=500", &events)
	out := map[string]int{}
	for _, ev := range events {
		out[str(ev, "action")]++
	}
	return out
}

// ---------------------------------------------------------------------------

func TestLocalAccountLifecycle(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")

	var providers obj
	e.anon().get("/api/v1/auth/providers", &providers)
	eq(t, providers["localLogin"], any(true), "local login advertised")

	// An admin adds a person (with a group) and invites them.
	var group obj
	admin.must("POST", "/api/v1/admin/groups", obj{"displayName": "Contractors"}, 201, &group)
	var carla obj
	admin.must("POST", "/api/v1/admin/users", obj{"displayName": "Carla Contractor", "email": "Carla@Contractor.test", "groupIds": []string{str(group, "id")}}, 201, &carla)
	id := str(carla, "id")
	eq(t, carla["localAccount"], any(nil), "not a local account until invited")
	var members obj
	admin.get("/api/v1/groups/"+str(group, "id"), &members)
	eq(t, len(members["members"].([]any)), 1, "added to the group")

	var inv obj
	admin.must("POST", "/api/v1/admin/users/"+id+"/invitation", nil, 200, &inv)
	exp, _ := time.Parse(time.RFC3339, str(inv, "expiresAt"))
	if d := time.Until(exp); d < 167*time.Hour || d > 169*time.Hour {
		t.Fatalf("invitation expires in %s", d)
	}
	listed := admin.adminUser("carla@contractor.test")
	eq(t, str(listed, "localAccount", "status"), "invited", "listed as invited")
	msgs := e.mailTo("carla@contractor.test")
	eq(t, len(msgs), 1, "invitation email")
	eq(t, msgs[0].Subject, "You're invited to Workflow", "subject")
	if !strings.Contains(msgs[0].HTML, "Accept invitation") || !strings.Contains(msgs[0].Text, "Admin added you") {
		t.Fatalf("invitation email:\n%s", msgs[0].Text)
	}
	first := linkToken(t, msgs[0], "invite")

	// Resending replaces the first link.
	admin.must("POST", "/api/v1/admin/users/"+id+"/invitation", nil, 200, nil)
	msgs = e.mailTo("carla@contractor.test")
	eq(t, len(msgs), 2, "second invitation")
	token := linkToken(t, msgs[1], "invite")
	c := e.anon()
	wantCode(t, c.do("POST", "/api/v1/auth/local/link", obj{"token": first}), 404, "invalid_link")
	wantCode(t, c.do("POST", "/api/v1/auth/local/link", obj{"token": token + "x"}), 404, "invalid_link")
	var info obj
	c.must("POST", "/api/v1/auth/local/link", obj{"token": token}, 200, &info)
	eq(t, str(info, "purpose"), "invite", "purpose")
	eq(t, str(info, "email"), "carla@contractor.test", "email")

	// The policy applies; the strength check explains it while typing.
	var check obj
	c.must("POST", "/api/v1/auth/local/password-check", obj{"password": "carla-contractor-2024", "email": "carla@contractor.test", "displayName": "Carla Contractor"}, 200, &check)
	eq(t, check["acceptable"], any(false), "name in password")
	c.must("POST", "/api/v1/auth/local/password-check", obj{"password": "Blue-Kettle-Morning-42"}, 200, &check)
	eq(t, check["acceptable"], any(true), "good password")
	r := c.do("POST", "/api/v1/auth/local/link/redeem", obj{"token": token, "password": "password1234"})
	eq(t, r.Status, 400, "common password refused")
	eq(t, str(r.JSON()["errors"].([]any)[0].(obj), "field"), "password", "field error")

	// Accepting sets the password; the link works once.
	c.must("POST", "/api/v1/auth/local/link/redeem", obj{"token": token, "password": "Blue-Kettle-Morning-42"}, 200, &info)
	eq(t, str(info, "email"), "carla@contractor.test", "redeemed for")
	wantCode(t, c.do("POST", "/api/v1/auth/local/link/redeem", obj{"token": token, "password": "Another-Fine-Password-9"}), 404, "invalid_link")
	eq(t, str(admin.adminUser("carla@contractor.test"), "localAccount", "status"), "active", "now active")

	// Sign in. Wrong passwords and unknown accounts get the same answer.
	bad := c.passwordLogin("carla@contractor.test", "Blue-Kettle-Morning-43")
	unknown := c.passwordLogin("nobody@contractor.test", "Blue-Kettle-Morning-42")
	wantCode(t, bad, 401, "invalid_credentials")
	eq(t, string(unknown.Body), string(bad.Body), "no account enumeration")
	c.must("GET", "/api/v1/me", nil, 401, nil)
	body := wantStatus(t, c.passwordLogin(" CARLA@contractor.test ", "Blue-Kettle-Morning-42"), 200, "signed_in")
	eq(t, str(body, "me", "email"), "carla@contractor.test", "me in the result")
	var me obj
	c.get("/api/v1/me", &me)
	eq(t, str(me, "displayName"), "Carla Contractor", "session works")
	if lastLogin := str(admin.adminUser("carla@contractor.test"), "lastLoginAt"); lastLogin == "" {
		t.Fatal("last sign-in not recorded")
	}

	// Two-factor: set up with the QR code / secret, confirm with a code.
	var sec obj
	c.get("/api/v1/me/security", &sec)
	eq(t, sec["localAccount"], any(true), "local account")
	eq(t, sec["totpEnabled"], any(false), "2FA off")
	var setup obj
	c.must("POST", "/api/v1/me/totp/setup", nil, 200, &setup)
	secret := str(setup, "secret")
	if !strings.HasPrefix(str(setup, "qrCode"), "data:image/svg+xml;base64,") || !strings.HasPrefix(str(setup, "otpauthUri"), "otpauth://totp/") {
		t.Fatalf("setup = %v", setup)
	}
	c.must("POST", "/api/v1/me/totp/confirm", obj{"code": "000000"}, 400, nil)
	var rc struct {
		Codes []string `json:"codes"`
	}
	c.must("POST", "/api/v1/me/totp/confirm", obj{"code": totpNow(t, secret, 0)}, 200, &rc)
	eq(t, len(rc.Codes), 10, "recovery codes")
	c.get("/api/v1/me/security", &sec)
	eq(t, sec["totpEnabled"], any(true), "2FA on")
	eq(t, fmt.Sprint(sec["recoveryCodesRemaining"]), "10", "codes remaining")
	eq(t, admin.adminUser("carla@contractor.test")["localAccount"].(obj)["totpEnabled"], any(true), "admin sees 2FA")

	// Sign in with a code: the password alone is not a session.
	c2 := e.anon()
	wantStatus(t, c2.passwordLogin("carla@contractor.test", "Blue-Kettle-Morning-42"), 200, "totp_required")
	c2.must("GET", "/api/v1/me", nil, 401, nil)
	wantCode(t, c2.do("POST", "/api/v1/auth/local/login/totp", obj{"code": "123456"}), 401, "invalid_code")
	next := totpNow(t, secret, 30*time.Second) // the enrolment used the current step
	wantStatus(t, c2.do("POST", "/api/v1/auth/local/login/totp", obj{"code": next}), 200, "signed_in")
	c2.get("/api/v1/me", nil)
	// A code cannot be used twice.
	c3 := e.anon()
	wantStatus(t, c3.passwordLogin("carla@contractor.test", "Blue-Kettle-Morning-42"), 200, "totp_required")
	wantCode(t, c3.do("POST", "/api/v1/auth/local/login/totp", obj{"code": next}), 401, "invalid_code")
	// Without the challenge cookie there is nothing to complete.
	wantCode(t, e.anon().do("POST", "/api/v1/auth/local/login/totp", obj{"code": next}), 401, "sign_in_expired")

	// A recovery code works once.
	wantStatus(t, c3.do("POST", "/api/v1/auth/local/login/totp", obj{"code": strings.ToUpper(rc.Codes[0])}), 200, "signed_in")
	c4 := e.anon()
	wantStatus(t, c4.passwordLogin("carla@contractor.test", "Blue-Kettle-Morning-42"), 200, "totp_required")
	wantCode(t, c4.do("POST", "/api/v1/auth/local/login/totp", obj{"code": rc.Codes[0]}), 401, "invalid_code")
	c.get("/api/v1/me/security", &sec)
	eq(t, fmt.Sprint(sec["recoveryCodesRemaining"]), "9", "one code used")
	if len(e.mailTo("carla@contractor.test")) != 4 { // invitation x2, 2FA on, recovery code used
		t.Fatalf("emails: %v", e.mailTo("carla@contractor.test"))
	}

	// Changing the password keeps this session and ends the others.
	c.must("POST", "/api/v1/me/password", obj{"currentPassword": "wrong-password-here", "newPassword": "Green-Teapot-Evening-77"}, 400, nil)
	c.must("POST", "/api/v1/me/password", obj{"currentPassword": "Blue-Kettle-Morning-42", "newPassword": "Blue-Kettle-Morning-42"}, 400, nil)
	c.must("POST", "/api/v1/me/password", obj{"currentPassword": "Blue-Kettle-Morning-42", "newPassword": "Green-Teapot-Evening-77"}, 204, nil)
	c.get("/api/v1/me", nil)
	c2.must("GET", "/api/v1/me", nil, 401, nil)
	c3.must("GET", "/api/v1/me", nil, 401, nil)
	msgs = e.mailTo("carla@contractor.test")
	eq(t, msgs[len(msgs)-1].Subject, "Your password was changed", "password changed email")
	wantCode(t, e.anon().passwordLogin("carla@contractor.test", "Blue-Kettle-Morning-42"), 401, "invalid_credentials")

	// Forgot password: the same answer for everyone; only the account gets an email.
	f := e.anon()
	f.must("POST", "/api/v1/auth/local/forgot-password", obj{"email": "nobody@contractor.test"}, 202, nil)
	eq(t, len(e.mailTo("nobody@contractor.test")), 0, "no email for unknown address")
	f.must("POST", "/api/v1/auth/local/forgot-password", obj{"email": "carla@contractor.test"}, 202, nil)
	msgs = e.mailTo("carla@contractor.test")
	reset := msgs[len(msgs)-1]
	eq(t, reset.Subject, "Reset your Workflow password", "reset email")
	rtoken := linkToken(t, reset, "reset-password")
	f.must("POST", "/api/v1/auth/local/link", obj{"token": rtoken}, 200, &info)
	eq(t, str(info, "purpose"), "reset", "reset link")
	// A reset link cannot be replayed as an invitation (purpose is signed) and expires.
	f.must("POST", "/api/v1/auth/local/link/redeem", obj{"token": rtoken, "password": "Purple-Lantern-Night-31"}, 200, nil)
	c.must("GET", "/api/v1/me", nil, 401, nil) // a reset ends every session
	wantCode(t, f.do("POST", "/api/v1/auth/local/link", obj{"token": rtoken}), 404, "invalid_link")
	e.exec(`UPDATE local_credentials SET totp_last_step = NULL`)
	wantStatus(t, f.passwordLogin("carla@contractor.test", "Purple-Lantern-Night-31"), 200, "totp_required")
	wantStatus(t, f.do("POST", "/api/v1/auth/local/login/totp", obj{"code": totpNow(t, secret, 0)}), 200, "signed_in")

	// An expired reset link is refused.
	f.must("POST", "/api/v1/auth/local/forgot-password", obj{"email": "carla@contractor.test"}, 202, nil)
	msgs = e.mailTo("carla@contractor.test")
	expired := linkToken(t, msgs[len(msgs)-1], "reset-password")
	e.exec(`UPDATE account_tokens SET expires_at = now() - interval '1 second' WHERE used_at IS NULL`)
	wantCode(t, f.do("POST", "/api/v1/auth/local/link/redeem", obj{"token": expired, "password": "Orange-Window-Dawn-55"}), 404, "invalid_link")

	// Disabling two-factor needs the password and a code; tokens cannot do it.
	f.must("POST", "/api/v1/me/totp/disable", obj{"password": "Purple-Lantern-Night-31", "code": "000000"}, 400, nil)
	var pat obj
	f.must("POST", "/api/v1/me/tokens", obj{"name": "cli", "scopes": []string{"write"}, "expiresInDays": 1}, 201, &pat)
	e.withToken(str(pat, "token")).must("POST", "/api/v1/me/password", obj{"currentPassword": "Purple-Lantern-Night-31", "newPassword": "Orange-Window-Dawn-55"}, 403, nil)
	f.must("POST", "/api/v1/me/totp/disable", obj{"password": "Purple-Lantern-Night-31", "code": rc.Codes[1]}, 204, nil)
	f.get("/api/v1/me/security", &sec)
	eq(t, sec["totpEnabled"], any(false), "2FA off again")
	wantStatus(t, e.anon().passwordLogin("carla@contractor.test", "Purple-Lantern-Night-31"), 200, "signed_in")

	actions := admin.auditActions()
	for _, a := range []string{"user.create", "user.invitation_sent", "user.invitation_accepted", "user.login", "user.login_failed", "user.totp_enabled",
		"user.recovery_code_used", "user.password_changed", "user.password_reset_requested", "user.password_reset", "user.totp_disabled"} {
		if actions[a] == 0 {
			t.Errorf("audit lacks %s: %v", a, actions)
		}
	}
	var logins []obj
	admin.get("/api/v1/admin/audit?action=user.login", &logins)
	if str(logins[0], "ip") == "" {
		t.Errorf("sign-in audit without address: %v", logins[0])
	}
}

func TestLocalAccountAdministration(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	var dan obj
	admin.must("POST", "/api/v1/admin/users", obj{"displayName": "Dan Temp", "email": "dan@example.com"}, 201, &dan)
	id := str(dan, "id")

	// A temporary password is shown once and must be replaced at the first sign-in.
	var tmp obj
	admin.must("POST", "/api/v1/admin/users/"+id+"/temporary-password", nil, 200, &tmp)
	temp := str(tmp, "password")
	eq(t, str(admin.adminUser("dan@example.com"), "localAccount", "status"), "password_change_required", "status")
	c := e.anon()
	step := wantStatus(t, c.passwordLogin("dan@example.com", temp), 200, "password_change_required")
	eq(t, str(step, "displayName"), "Dan Temp", "name for the strength check")
	eq(t, step["me"], any(nil), "not signed in yet")
	c.must("GET", "/api/v1/me", nil, 401, nil)
	c.must("POST", "/api/v1/auth/local/login/new-password", obj{"newPassword": "short"}, 400, nil)
	c.must("POST", "/api/v1/auth/local/login/new-password", obj{"newPassword": temp}, 400, nil)
	wantStatus(t, c.do("POST", "/api/v1/auth/local/login/new-password", obj{"newPassword": "Silver-Bicycle-Rain-88"}), 200, "signed_in")
	c.get("/api/v1/me", nil)
	eq(t, str(admin.adminUser("dan@example.com"), "localAccount", "status"), "active", "changed")

	// Invitations are for people without a password; resets for those with one.
	admin.must("POST", "/api/v1/admin/users/"+id+"/invitation", nil, 409, nil)
	admin.must("POST", "/api/v1/admin/users/"+id+"/password-reset", nil, 202, nil)
	msgs := e.mailTo("dan@example.com")
	linkToken(t, msgs[len(msgs)-1], "reset-password")

	// Old hashes are upgraded at sign-in.
	weak, _ := local.HashPassword("Silver-Bicycle-Rain-88", local.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32})
	e.exec(`UPDATE local_credentials SET password_hash = $1 WHERE user_id = $2`, weak, id)
	wantStatus(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88"), 200, "signed_in")
	var stored string
	if err := e.Pool.QueryRow(context.Background(), `SELECT password_hash FROM local_credentials WHERE user_id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "m=19456,t=2,p=1") {
		t.Fatalf("not rehashed: %s", stored)
	}

	// Lockout: ten failures (from any addresses) lock the account; the right password then
	// does not help until it expires or an admin unlocks it.
	for i := 1; i <= 9; i++ {
		wantCode(t, e.anon().passwordLogin("dan@example.com", "not-the-password", fmt.Sprintf("10.1.0.%d", i)), 401, "invalid_credentials")
	}
	r := e.anon().passwordLogin("dan@example.com", "not-the-password", "10.1.0.10")
	wantCode(t, r, 429, "locked")
	if s, _ := strconv.Atoi(r.Header.Get("Retry-After")); s < 800 || s > 900 {
		t.Fatalf("Retry-After = %q", r.Header.Get("Retry-After"))
	}
	wantCode(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88", "10.2.0.1"), 429, "locked")
	if str(admin.adminUser("dan@example.com"), "localAccount", "lockedUntil") == "" {
		t.Fatal("lock not shown to admins")
	}
	// Unknown addresses lock the same way, so a lockout reveals nothing.
	for i := 1; i <= 9; i++ {
		wantCode(t, e.anon().passwordLogin("ghost@example.com", "not-the-password", fmt.Sprintf("10.3.0.%d", i)), 401, "invalid_credentials")
	}
	wantCode(t, e.anon().passwordLogin("ghost@example.com", "not-the-password", "10.3.0.10"), 429, "locked")
	admin.must("POST", "/api/v1/admin/users/"+id+"/unlock", nil, 204, nil)
	wantStatus(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88", "10.2.0.1"), 200, "signed_in")

	// Expired lockouts end by themselves.
	for i := 1; i <= 10; i++ {
		e.anon().passwordLogin("dan@example.com", "not-the-password", fmt.Sprintf("10.4.0.%d", i))
	}
	e.exec(`UPDATE local_credentials SET locked_until = now() - interval '1 second' WHERE user_id = $1`, id)
	wantStatus(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88", "10.4.0.99"), 200, "signed_in")

	// Two-factor reset by an admin (lost phone).
	var setup obj
	c.must("POST", "/api/v1/me/totp/setup", nil, 200, &setup)
	c.must("POST", "/api/v1/me/totp/confirm", obj{"code": totpNow(t, str(setup, "secret"), 0)}, 200, nil)
	wantStatus(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88"), 200, "totp_required")
	admin.must("DELETE", "/api/v1/admin/users/"+id+"/totp", nil, 204, nil)
	wantStatus(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88"), 200, "signed_in")

	// Deactivated accounts are refused (only the right password learns why).
	admin.must("PATCH", "/api/v1/admin/users/"+id, obj{"active": false}, 200, nil)
	wantCode(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88"), 403, "deactivated")
	wantCode(t, e.anon().passwordLogin("dan@example.com", "wrong-password-again"), 401, "invalid_credentials")
	admin.must("POST", "/api/v1/admin/users/"+id+"/invitation", nil, 400, nil)
	admin.must("PATCH", "/api/v1/admin/users/"+id, obj{"active": true}, 200, nil)

	// Removing the password makes them a single sign-on user again.
	admin.must("DELETE", "/api/v1/admin/users/"+id+"/password", nil, 204, nil)
	eq(t, admin.adminUser("dan@example.com")["localAccount"], any(nil), "no longer local")
	wantCode(t, e.anon().passwordLogin("dan@example.com", "Silver-Bicycle-Rain-88"), 401, "invalid_credentials")

	// Invitations can be revoked.
	var eve obj
	admin.must("POST", "/api/v1/admin/users", obj{"displayName": "Eve Invitee", "email": "eve@example.com"}, 201, &eve)
	admin.must("POST", "/api/v1/admin/users/"+str(eve, "id")+"/invitation", nil, 200, nil)
	tok := linkToken(t, e.mailTo("eve@example.com")[0], "invite")
	admin.must("DELETE", "/api/v1/admin/users/"+str(eve, "id")+"/invitation", nil, 204, nil)
	wantCode(t, e.anon().do("POST", "/api/v1/auth/local/link", obj{"token": tok}), 404, "invalid_link")
	eq(t, admin.adminUser("eve@example.com")["localAccount"], any(nil), "revoked invitation")

	// Service accounts never get passwords; only admins manage local accounts.
	var bot obj
	admin.must("POST", "/api/v1/admin/users", obj{"kind": "service", "displayName": "CI"}, 201, &bot)
	wantCode(t, admin.do("POST", "/api/v1/admin/users/"+str(bot, "id")+"/temporary-password", nil), 400, "service_account")
	member := e.login("member@example.com", "Member")
	member.must("POST", "/api/v1/admin/users/"+str(eve, "id")+"/invitation", nil, 403, nil)

	actions := admin.auditActions()
	for _, a := range []string{"user.temporary_password_set", "user.password_reset_sent", "user.locked", "user.unlocked", "user.totp_reset", "user.password_removed", "user.invitation_revoked"} {
		if actions[a] == 0 {
			t.Errorf("audit lacks %s", a)
		}
	}
}

func TestLocalSignInThrottling(t *testing.T) {
	e := newEnv(t, withEnv("WFM_LOCAL_LOCKOUT_ATTEMPTS", "0"))
	admin := e.login("admin@example.com", "Admin")
	var u obj
	admin.must("POST", "/api/v1/admin/users", obj{"displayName": "Fay", "email": "fay@example.com"}, 201, &u)
	admin.must("POST", "/api/v1/admin/users/"+str(u, "id")+"/temporary-password", nil, 200, nil)

	// untilThrottled tries until an attempt gets 429 (with Retry-After) and returns how many got
	// through first. Buckets refill while the test runs, so a slow run may get one or two more.
	untilThrottled := func(limit int, try func(i int) Response) int {
		t.Helper()
		for i := 0; i < limit; i++ {
			r := try(i)
			if r.Status == 429 {
				wantCode(t, r, 429, "rate_limited")
				if s, _ := strconv.Atoi(r.Header.Get("Retry-After")); s < 1 {
					t.Fatalf("Retry-After = %q", r.Header.Get("Retry-After"))
				}
				return i
			}
			wantCode(t, r, 401, "invalid_credentials")
		}
		t.Fatalf("not throttled after %d attempts", limit)
		return 0
	}

	// Per email and address: a burst of 10, then 429.
	if n := untilThrottled(14, func(int) Response { return e.anon().passwordLogin("fay@example.com", "wrong", "192.0.2.1") }); n < 10 {
		t.Fatalf("throttled after %d attempts", n)
	}
	// Another address may still try that email.
	wantCode(t, e.anon().passwordLogin("fay@example.com", "wrong", "192.0.2.2"), 401, "invalid_credentials")

	// Per address: 30 attempts a minute whatever the email, across the sign-in endpoints.
	if n := untilThrottled(40, func(i int) Response {
		return e.anon().passwordLogin(fmt.Sprintf("p%d@example.com", i), "wrong", "192.0.2.3")
	}); n < 30 {
		t.Fatalf("throttled after %d attempts", n)
	}
	wantCode(t, e.anon().do("POST", "/api/v1/auth/local/forgot-password", obj{"email": "x@example.com"}, "X-Forwarded-For", "192.0.2.3"), 429, "rate_limited")
}

func TestLocalAccountsAndIdentityProviders(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com", "Admin")
	var conn obj
	admin.must("POST", "/api/v1/admin/scim-connections", obj{"name": "Okta"}, 201, &conn)
	okta := e.scim(str(conn, "token"))

	// A SCIM-managed person cannot get a password.
	sam := okta.call("POST", "/Users", obj{"userName": "sam@example.com", "emails": []obj{{"value": "sam@example.com", "primary": true}}, "displayName": "Sam Scim", "active": true}, 201)
	samID := str(sam, "id")
	for _, path := range []string{"/invitation", "/temporary-password"} {
		wantCode(t, admin.do("POST", "/api/v1/admin/users/"+samID+path, nil), 400, "managed_by_identity_provider")
	}
	e.exec(`INSERT INTO local_credentials (user_id, password_hash) VALUES ($1, $2)`, samID, "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNoaGFzaA")
	wantCode(t, e.anon().passwordLogin("sam@example.com", "anything-at-all"), 401, "invalid_credentials")

	// A local account that an identity provider later takes over (Google directory sync matches
	// people by email and sets source = 'google') loses its password.
	var lou obj
	admin.must("POST", "/api/v1/admin/users", obj{"displayName": "Lou Local", "email": "lou@example.com"}, 201, &lou)
	var tmp obj
	admin.must("POST", "/api/v1/admin/users/"+str(lou, "id")+"/temporary-password", nil, 200, &tmp)
	wantStatus(t, e.anon().passwordLogin("lou@example.com", str(tmp, "password")), 200, "password_change_required")
	e.exec(`UPDATE users SET source = 'google', google_id = 'g-lou' WHERE id = $1`, str(lou, "id"))
	var left int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM local_credentials WHERE user_id = $1`, str(lou, "id")).Scan(&left); err != nil || left != 0 {
		t.Fatalf("credentials left after takeover: %d (%v)", left, err)
	}
	wantCode(t, e.anon().passwordLogin("lou@example.com", str(tmp, "password")), 401, "invalid_credentials")
	eq(t, admin.adminUser("lou@example.com")["localAccount"], any(nil), "no local account under directory sync")

	// A person who signed in just in time (or was pre-provisioned) can be made a local account too.
	jit := e.login("jit@example.com", "Jit Person")
	admin.must("POST", "/api/v1/admin/users/"+jit.id()+"/invitation", nil, 200, nil)
	eq(t, str(admin.adminUser("jit@example.com"), "localAccount", "status"), "invited", "dev/JIT person invited")
}

// An install without single sign-on starts with `wfm create-admin`.
func TestLocalBootstrapAdmin(t *testing.T) {
	e := newEnv(t, withEnv("WFM_DEV_LOGIN", "false"))
	ctx := context.Background()
	if _, _, err := e.App.Local.BootstrapAdmin(ctx, "not-an-email", ""); err == nil {
		t.Fatal("invalid email accepted")
	}
	u, pw, err := e.App.Local.BootstrapAdmin(ctx, "Owner@Example.com", "Olive Owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, u.OrgRole, "admin", "admin")
	c := e.anon()
	wantStatus(t, c.passwordLogin("owner@example.com", pw), 200, "password_change_required")
	body := wantStatus(t, c.do("POST", "/api/v1/auth/local/login/new-password", obj{"newPassword": "Granite-Willow-Beacon-44"}), 200, "signed_in")
	eq(t, str(body, "me", "orgRole"), "admin", "signed in as admin")
	c.must("GET", "/api/v1/admin/users", nil, 200, nil)

	// Running it again for an existing member promotes them and issues a new temporary password.
	var m obj
	c.must("POST", "/api/v1/admin/users", obj{"displayName": "Max Member", "email": "max@example.com"}, 201, &m)
	u2, pw2, err := e.App.Local.BootstrapAdmin(ctx, "max@example.com", "")
	if err != nil || u2.OrgRole != "admin" || u2.DisplayName != "Max Member" || pw2 == "" {
		t.Fatalf("existing person: %+v %v", u2, err)
	}
	if _, _, err := e.App.Local.BootstrapAdmin(ctx, "owner@example.com", ""); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.anon().passwordLogin("owner@example.com", "Granite-Willow-Beacon-44"), 401, "invalid_credentials") // replaced
}

func TestLocalLoginDisabled(t *testing.T) {
	e := newEnv(t, withEnv("WFM_LOCAL_LOGIN", "false"))
	admin := e.login("admin@example.com", "Admin") // development sign-in is unaffected
	var providers obj
	e.anon().get("/api/v1/auth/providers", &providers)
	eq(t, providers["localLogin"], any(false), "not advertised")
	wantCode(t, e.anon().passwordLogin("admin@example.com", "whatever-password"), 404, "local_login_disabled")
	wantCode(t, e.anon().do("POST", "/api/v1/auth/local/forgot-password", obj{"email": "admin@example.com"}), 404, "local_login_disabled")
	wantCode(t, admin.do("POST", "/api/v1/admin/users/"+admin.id()+"/invitation", nil), 404, "local_login_disabled")
	wantCode(t, e.anon().do("POST", "/api/v1/auth/local/password-check", obj{"password": "anything-long-enough"}), 404, "local_login_disabled")
	var sec obj
	admin.get("/api/v1/me/security", &sec)
	eq(t, sec["localLoginEnabled"], any(false), "profile knows")
}

func TestLocalRequireTwoFactor(t *testing.T) {
	e := newEnv(t, withEnv("WFM_LOCAL_REQUIRE_2FA", "true"))
	admin := e.login("admin@example.com", "Admin")
	var u obj
	admin.must("POST", "/api/v1/admin/users", obj{"displayName": "Gil", "email": "gil@example.com"}, 201, &u)
	var tmp obj
	admin.must("POST", "/api/v1/admin/users/"+str(u, "id")+"/temporary-password", nil, 200, &tmp)

	c := e.anon()
	wantStatus(t, c.passwordLogin("gil@example.com", str(tmp, "password")), 200, "password_change_required")
	wantStatus(t, c.do("POST", "/api/v1/auth/local/login/new-password", obj{"newPassword": "Copper-Garden-Storm-19"}), 200, "totp_enrollment_required")
	c.must("GET", "/api/v1/me", nil, 401, nil)
	var setup obj
	c.must("POST", "/api/v1/auth/local/login/totp-setup", nil, 200, &setup)
	c.must("POST", "/api/v1/auth/local/login/totp-setup/confirm", obj{"code": "000000"}, 400, nil)
	body := wantStatus(t, c.do("POST", "/api/v1/auth/local/login/totp-setup/confirm", obj{"code": totpNow(t, str(setup, "secret"), 0)}), 200, "signed_in")
	eq(t, len(body["recoveryCodes"].([]any)), 10, "recovery codes after enrolment")
	c.get("/api/v1/me", nil)
	var sec obj
	c.get("/api/v1/me/security", &sec)
	eq(t, sec["require2fa"], any(true), "required")
	wantCode(t, c.do("POST", "/api/v1/me/totp/disable", obj{"password": "Copper-Garden-Storm-19", "code": body["recoveryCodes"].([]any)[0]}), 400, "two_factor_required")

	// After an admin resets it, the next sign-in enrols again.
	admin.must("DELETE", "/api/v1/admin/users/"+str(u, "id")+"/totp", nil, 204, nil)
	wantStatus(t, e.anon().passwordLogin("gil@example.com", "Copper-Garden-Storm-19"), 200, "totp_enrollment_required")
}
