// Package identity handles sign-in (generic OIDC; Google is one provider), sessions, personal
// access tokens and provisioning. Every provisioning path - just-in-time on first login,
// Google directory sync and SCIM - writes through Provisioner, the same pattern as ssosync.
package identity

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/config"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
)

type Provisioner struct {
	Pool *pgxpool.Pool
	Q    *dbq.Queries
	Cfg  *config.Config
	Log  *slog.Logger
}

func NewProvisioner(pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) *Provisioner {
	return &Provisioner{Pool: pool, Q: dbq.New(pool), Cfg: cfg, Log: log}
}

func (p *Provisioner) IsBootstrapAdmin(email string) bool {
	return slices.Contains(p.Cfg.BootstrapAdmins, strings.ToLower(email))
}

// JITEnabled reports whether first sign-in may create users. "auto" enables it only when no
// other provisioning source (directory sync or a SCIM connection) is configured.
func (p *Provisioner) JITEnabled(ctx context.Context) bool {
	switch p.Cfg.JITProvisioning {
	case config.JITEnabled:
		return true
	case config.JITDisabled:
		return false
	}
	if p.Cfg.GoogleDirectory.Enabled || p.Cfg.Keycloak.Enabled {
		return false
	}
	n, err := p.Q.CountActiveScimConnections(ctx)
	return err == nil && n == 0
}

// Deactivate disables a user and immediately revokes their sessions and tokens.
func (p *Provisioner) Deactivate(ctx context.Context, q *dbq.Queries, userID uuid.UUID) (dbq.User, error) {
	u, err := q.SetUserActive(ctx, dbq.SetUserActiveParams{ID: userID, Active: false})
	if err != nil {
		return u, err
	}
	if err := q.DeleteUserSessions(ctx, userID); err != nil {
		return u, err
	}
	return u, q.RevokeUserTokens(ctx, userID)
}

// MarkDeleted handles a user removed upstream: deactivated now, anonymised after the retention period.
func (p *Provisioner) MarkDeleted(ctx context.Context, q *dbq.Queries, userID uuid.UUID) error {
	if err := q.MarkUserDeleted(ctx, userID); err != nil {
		return err
	}
	if err := q.DeleteUserSessions(ctx, userID); err != nil {
		return err
	}
	return q.RevokeUserTokens(ctx, userID)
}

// Claims are the identity facts from a sign-in.
type Claims struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	GivenName     string
	FamilyName    string
	Picture       string
	HostedDomain  string
	IsGoogle      bool
}

// LoginError is a sign-in refusal with a code the login page explains.
type LoginError struct{ Code string }

func (e *LoginError) Error() string { return "sign-in refused: " + e.Code }

// SignIn resolves federated claims to a local user, provisioning just-in-time when allowed.
func (p *Provisioner) SignIn(ctx context.Context, c Claims, allowedDomains []string) (dbq.User, error) {
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if email == "" || !c.EmailVerified {
		return dbq.User{}, &LoginError{"email_unverified"}
	}
	if len(allowedDomains) > 0 {
		domain := c.HostedDomain // Google: trust only the hd claim, never the email suffix
		if !c.IsGoogle {
			domain = email[strings.LastIndexByte(email, '@')+1:]
		}
		if domain == "" || !slices.Contains(allowedDomains, strings.ToLower(domain)) {
			return dbq.User{}, &LoginError{"domain_not_allowed"}
		}
	}

	var user dbq.User
	err := db.Tx(ctx, p.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		var found bool
		if ident, err := q.GetIdentity(ctx, dbq.GetIdentityParams{Provider: c.Provider, Subject: c.Subject}); err == nil {
			if u, err := q.GetUser(ctx, ident.UserID); err == nil {
				user, found = u, true
			}
		}
		if !found && c.IsGoogle {
			if u, err := q.GetUserByGoogleID(ctx, &c.Subject); err == nil {
				user, found = u, true
			}
		}
		if !found {
			if u, err := q.GetUserByEmail(ctx, email); err == nil {
				user, found = u, true
			}
		}
		if found {
			var err error
			if user, err = p.claimPlaceholder(ctx, q, user, "jit", true); err != nil {
				return err
			}
			if !user.Active || user.DeletedAt != nil {
				return &LoginError{"deactivated"}
			}
			if user.Kind != "human" {
				return &LoginError{"deactivated"}
			}
			// Profiles of users managed by SCIM / directory sync stay authoritative upstream.
			if user.Source == "jit" || user.Source == "dev" || user.Source == "admin" {
				name := c.Name
				if name == "" {
					name = user.DisplayName
				}
				u, err := q.UpdateUserProfile(ctx, dbq.UpdateUserProfileParams{
					ID: user.ID, DisplayName: name, GivenName: nilIfEmpty(c.GivenName), FamilyName: nilIfEmpty(c.FamilyName), AvatarUrl: nilIfEmpty(c.Picture),
				})
				if err != nil {
					return err
				}
				user = u
			}
			if c.IsGoogle && user.GoogleID == nil {
				if err := q.LinkGoogleID(ctx, dbq.LinkGoogleIDParams{ID: user.ID, GoogleID: &c.Subject}); err != nil && !db.IsUniqueViolation(err) {
					return err
				}
			}
		} else {
			if !p.JITEnabled(ctx) {
				return &LoginError{"not_provisioned"}
			}
			n, err := q.CountUsers(ctx)
			if err != nil {
				return err
			}
			role := "member"
			if n == 0 || p.IsBootstrapAdmin(email) {
				role = "admin"
			}
			name := c.Name
			if name == "" {
				name = email[:strings.IndexByte(email, '@')]
			}
			var googleID *string
			if c.IsGoogle {
				googleID = &c.Subject
			}
			user, err = q.CreateUser(ctx, dbq.CreateUserParams{
				Kind: "human", Email: email, Username: email, DisplayName: name, GivenName: nilIfEmpty(c.GivenName),
				FamilyName: nilIfEmpty(c.FamilyName), AvatarUrl: nilIfEmpty(c.Picture), OrgRole: role, Source: "jit",
				GoogleID: googleID, Active: true,
			})
			if err != nil {
				return err
			}
			tracker.Audit(ctx, q, authz.SystemActor(), "user.provision_jit", "user", user.ID.String(), nil, map[string]any{"email": email, "provider": c.Provider})
		}
		if p.IsBootstrapAdmin(email) && user.OrgRole != "admin" {
			u, err := q.SetUserOrgRole(ctx, dbq.SetUserOrgRoleParams{ID: user.ID, OrgRole: "admin"})
			if err != nil {
				return err
			}
			user = u
		}
		return q.UpsertIdentity(ctx, dbq.UpsertIdentityParams{UserID: user.ID, Provider: c.Provider, Subject: c.Subject, Email: &email})
	})
	return user, err
}

// DevSignIn is the passwordless development login.
func (p *Provisioner) DevSignIn(ctx context.Context, email, name string) (dbq.User, error) {
	if !p.Cfg.DevLogin {
		return dbq.User{}, apperr.NotFound("route")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := p.Q.GetUserByEmail(ctx, email)
	if err == nil {
		if u, err = p.claimPlaceholder(ctx, p.Q, u, "dev", false); err != nil {
			return u, err
		}
		if !u.Active || u.DeletedAt != nil {
			return u, apperr.Forbidden("this account is deactivated")
		}
		if p.IsBootstrapAdmin(email) && u.OrgRole != "admin" {
			return p.Q.SetUserOrgRole(ctx, dbq.SetUserOrgRoleParams{ID: u.ID, OrgRole: "admin"})
		}
		return u, nil
	}
	n, err := p.Q.CountUsers(ctx)
	if err != nil {
		return dbq.User{}, err
	}
	role := "member"
	if n == 0 || p.IsBootstrapAdmin(email) {
		role = "admin"
	}
	if name == "" {
		name = email[:max(strings.IndexByte(email, '@'), 0)]
	}
	return p.Q.CreateUser(ctx, dbq.CreateUserParams{Kind: "human", Email: email, Username: email, DisplayName: name, OrgRole: role, Source: "dev", Active: true})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
