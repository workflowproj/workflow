package servicedesk

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/identity/local"
	"github.com/workflowproj/workflow/internal/tracker"
)

func (s *Service) customerView(ctx context.Context, q *dbq.Queries, u dbq.User) api.ServiceCustomer {
	v := api.ServiceCustomer{User: tracker.UserView(u), Account: api.ServiceCustomerAccountMember, Active: ptr(u.Active && u.DeletedAt == nil)}
	if u.OrgRole == authz.OrgCustomer {
		v.Account = api.ServiceCustomerAccountCustomer
	}
	if cred, err := q.GetLocalCredentials(ctx, u.ID); err == nil && cred.PasswordHash == nil {
		v.Invited = ptr(true)
	}
	return v
}

// ListCustomers lists the people on a desk's customer list (agents).
func (s *Service) ListCustomers(ctx context.Context, actor *authz.Actor, ref, q string, limit int) ([]api.ServiceCustomer, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListServiceCustomers(ctx, dbq.ListServiceCustomersParams{ProjectID: p.ID, Q: strings.TrimSpace(q), Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := []api.ServiceCustomer{}
	for _, r := range rows {
		v := s.customerView(ctx, s.Q, r.User)
		v.AddedAt = r.AddedAt
		out = append(out, v)
	}
	return out, nil
}

// AddCustomer puts someone on a desk's customer list by email. Someone new gets a customer
// account and, with local accounts on, an email invitation to choose a password (otherwise they
// sign in with single sign-on, matched by their email). An existing account is added as it is.
func (s *Service) AddCustomer(ctx context.Context, actor *authz.Actor, ref string, in api.ServiceCustomerInput, ip string) (api.ServiceCustomerAdded, error) {
	p, d, _, err := s.desk(ctx, actor, ref, authz.CustomerManage)
	if err != nil {
		return api.ServiceCustomerAdded{}, err
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || strings.ContainsAny(email, " \t\r\n<>") {
		return api.ServiceCustomerAdded{}, apperr.Invalid("email", "a valid email address is required")
	}
	name := strings.TrimSpace(deref(in.DisplayName))
	if name == "" {
		name = email[:at]
	}
	var out api.ServiceCustomerAdded
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := q.GetUserByEmail(ctx, email)
		switch {
		case db.IsNoRows(err):
			if u, err = q.CreateUser(ctx, dbq.CreateUserParams{
				Kind: "human", Email: email, Username: email, DisplayName: name, OrgRole: authz.OrgCustomer, Source: "admin", Active: true,
			}); err != nil {
				return err
			}
			tracker.Audit(ctx, q, actor, "user.create", "user", u.ID.String(), &p.ID, map[string]any{"email": email, "orgRole": authz.OrgCustomer, "serviceDesk": p.Key})
			if s.Cfg.Local.Enabled {
				if err := s.Local.InviteCustomer(ctx, tx, q, actor, u, local.Portal{Name: d.PortalName, URL: s.PortalURL(p)}, ip); err != nil {
					return err
				}
				out.Invited = true
			}
		case err != nil:
			return err
		case u.DeletedAt != nil || u.Kind != "human":
			return apperr.Invalid("email", "that account cannot use the portal")
		}
		if err := q.AddServiceCustomer(ctx, dbq.AddServiceCustomerParams{ProjectID: p.ID, UserID: u.ID, AddedBy: actor.UserID()}); err != nil {
			return err
		}
		tracker.Audit(ctx, q, actor, "service_desk.customer_add", "user", u.ID.String(), &p.ID, map[string]any{"email": email, "invited": out.Invited})
		out.Customer = s.customerView(ctx, q, u)
		out.Customer.AddedAt = s.Now()
		return nil
	})
	return out, err
}

// RemoveCustomer takes someone off a desk's customer list. Their account stays; requests they
// raised stay theirs.
func (s *Service) RemoveCustomer(ctx context.Context, actor *authz.Actor, ref string, userID uuid.UUID) error {
	p, _, _, err := s.desk(ctx, actor, ref, authz.CustomerManage)
	if err != nil {
		return err
	}
	n, err := s.Q.RemoveServiceCustomer(ctx, dbq.RemoveServiceCustomerParams{ProjectID: p.ID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("customer")
	}
	tracker.Audit(ctx, s.Q, actor, "service_desk.customer_remove", "user", userID.String(), &p.ID, nil)
	return nil
}

// SignupInfo describes a desk that lets people sign up (anyone may ask; "not found" otherwise).
func (s *Service) SignupInfo(ctx context.Context, ref string) (api.PortalSignupInfo, error) {
	p, d, err := s.signupDesk(ctx, ref)
	if err != nil {
		return api.PortalSignupInfo{}, err
	}
	return api.PortalSignupInfo{ProjectKey: p.Key, Name: d.PortalName, Description: d.PortalDescription}, nil
}

func (s *Service) signupDesk(ctx context.Context, ref string) (dbq.Project, dbq.ServiceDesk, error) {
	p, err := s.Tracker.ResolveProject(ctx, s.Q, ref)
	if err != nil || !tracker.IsServiceProject(p) || p.ArchivedAt != nil {
		return p, dbq.ServiceDesk{}, apperr.NotFound("service desk")
	}
	d, err := s.Q.GetServiceDesk(ctx, p.ID)
	if err != nil || !d.SelfSignup || !s.Cfg.Local.Enabled {
		return p, d, apperr.NotFound("service desk")
	}
	return p, d, nil
}

// Signup creates a customer account from a desk's portal (self sign-up, off by default). The
// address is confirmed by the emailed link to choose a password; the answer is the same whether
// or not the address already has an account.
func (s *Service) Signup(ctx context.Context, ref string, in api.PortalSignup, ip string) error {
	p, d, err := s.signupDesk(ctx, ref)
	if err != nil {
		return err
	}
	return s.Local.SignUpCustomer(ctx, in.Email, in.DisplayName, local.Portal{Name: d.PortalName, URL: s.PortalURL(p)}, ip, func(_ pgx.Tx, q *dbq.Queries, u dbq.User) error {
		return q.AddServiceCustomer(ctx, dbq.AddServiceCustomerParams{ProjectID: p.ID, UserID: u.ID})
	})
}
