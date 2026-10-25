package local

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

// Customers of service desks (organisation role "customer") are local accounts too: agents
// invite them by email and they choose a password, or they sign themselves up from a portal that
// allows it. They then sign in like anyone else and are sent to the portal.

// Portal describes the help desk an invitation or sign-up is for.
type Portal struct {
	Name string
	URL  string
}

// InviteCustomer emails a customer a link to choose a password, inside the caller's transaction
// (the service desk creates the account and adds them to the desk in the same one).
func (s *Service) InviteCustomer(ctx context.Context, tx pgx.Tx, q *dbq.Queries, by *authz.Actor, u dbq.User, portal Portal, ip string) error {
	if _, err := s.person(ctx, q, u.ID); err != nil {
		return err
	}
	cred, err := q.EnsureLocalCredentials(ctx, u.ID)
	if err != nil {
		return err
	}
	if cred.PasswordHash != nil {
		return nil // they can already sign in
	}
	if err := q.CloseAccountTokens(ctx, dbq.CloseAccountTokensParams{UserID: u.ID, Purpose: "invite"}); err != nil {
		return err
	}
	expires := time.Now().Add(InviteTTL)
	raw, link := s.newLink("invite")
	if _, err := q.CreateAccountToken(ctx, dbq.CreateAccountTokenParams{UserID: u.ID, Purpose: "invite", TokenHash: secure.Hash(raw), ExpiresAt: expires, CreatedBy: by.UserID()}); err != nil {
		return err
	}
	s.audit(ctx, q, by, "user.invitation_sent", &u, ip, map[string]any{"email": u.Email, "expiresAt": expires, "portal": portal.Name})
	return s.email(ctx, tx, customerInviteEmail(u.Email, u.DisplayName, by, portal, link, expires))
}

// SignUpCustomer creates a customer account from a portal's sign-up form and emails a link to
// choose a password, which confirms the address. It answers the same way, and takes about as
// long, whether or not the address already has an account: an existing account is emailed how
// to sign in instead. add runs in the same transaction for the person (new or existing) once the
// email is queued, to give them access to the desk. Throttled like a password reset.
func (s *Service) SignUpCustomer(ctx context.Context, email, name string, portal Portal, ip string, add func(tx pgx.Tx, q *dbq.Queries, u dbq.User) error) error {
	if err := s.enabled(); err != nil {
		return err
	}
	if err := allow(s.signInIP, ip); err != nil {
		return err
	}
	start := time.Now()
	defer func() { time.Sleep(time.Until(start.Add(forgotMinDuration))) }()
	email = normEmail(email)
	name = strings.TrimSpace(name)
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || len(email) > 254 || strings.ContainsAny(email, " \t\r\n<>") {
		return apperr.Invalid("email", "a valid email address is required")
	}
	if name == "" || len(name) > 120 {
		return apperr.Invalid("displayName", "your name is required (at most 120 characters)")
	}
	// People in the organisation's own sign-in domains get their account from the identity
	// provider; a sign-up must not claim their address first (as a portal-only customer).
	if domain := email[at+1:]; s.ssoDomain(domain) {
		return apperr.Invalid("email", "addresses at %s sign in with single sign-on: use Sign in instead", domain)
	}
	if ok, _ := s.forgotEmail.Allow(email); !ok {
		return nil
	}
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := q.GetUserByEmail(ctx, email)
		switch {
		case db.IsNoRows(err):
			if u, err = q.CreateUser(ctx, dbq.CreateUserParams{
				Kind: "human", Email: email, Username: email, DisplayName: name, OrgRole: authz.OrgCustomer, Source: "admin", Active: true,
			}); err != nil {
				return err
			}
			s.audit(ctx, q, nil, "user.create", &u, ip, map[string]any{"email": email, "orgRole": authz.OrgCustomer, "via": "portal sign-up", "portal": portal.Name})
			if err := s.InviteCustomer(ctx, tx, q, nil, u, portal, ip); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if !u.Active || u.DeletedAt != nil || u.Kind != "human" {
				return nil
			}
			if err := s.email(ctx, tx, existingAccountEmail(u.Email, u.DisplayName, portal, s.Cfg.PublicURL)); err != nil {
				return err
			}
			// Only customers are added to the desk this way; staff accounts already reach it.
			if u.OrgRole != authz.OrgCustomer {
				return nil
			}
		}
		return add(tx, q, u)
	})
}

// ssoDomain reports whether a domain is one the organisation signs in with through an identity
// provider (WFM_ALLOWED_DOMAINS or a provider's own).
func (s *Service) ssoDomain(domain string) bool {
	for _, p := range s.Cfg.OIDCProviders {
		for _, d := range p.AllowedDomains {
			if strings.EqualFold(d, domain) {
				return true
			}
		}
	}
	return false
}

func customerInviteEmail(to, name string, by *authz.Actor, portal Portal, link string, expires time.Time) email {
	intro := fmt.Sprintf("Thanks for signing up to the %s help centre, where you can raise requests and follow them.", portal.Name)
	if by != nil {
		intro = fmt.Sprintf("%s invited you to the %s help centre, where you can raise requests and follow them.", inviter(by), portal.Name)
	}
	return email{
		To: to, Subject: "Your " + portal.Name + " account", Heading: "Get help from " + portal.Name,
		Lines: []string{
			fmt.Sprintf("Hi %s,", name),
			intro + fmt.Sprintf(" Choose a password to finish setting up your account; you'll sign in with %s.", to),
			fmt.Sprintf("This link works once and expires on %s.", expires.UTC().Format("2 January 2006 at 15:04 MST")),
		},
		Action: "Choose a password", URL: link, Footer: ignoreFooter,
	}
}

func existingAccountEmail(to, name string, portal Portal, publicURL string) email {
	return email{
		To: to, Subject: "Your " + portal.Name + " account", Heading: "You already have an account",
		Lines: []string{
			fmt.Sprintf("Hi %s,", name),
			fmt.Sprintf("Someone (hopefully you) signed up to the %s help centre with this address, which already has an account. Sign in to raise requests; if you have forgotten your password, you can reset it from the sign-in page.", portal.Name),
		},
		Action: "Sign in", URL: portal.URL, Footer: "If you did not sign up, you can ignore this email. Forgot your password? " + publicURL + "/forgot-password",
	}
}
