// Package servicedesk turns service projects (the "service" project template) into a help desk:
// a customer portal where people raise requests through request types' forms and follow them,
// replies to customers kept apart from internal notes, agents' queues, SLA clocks measured in
// business-hours calendars, approvals, and emails to customers and approvers.
//
// Customers (organisation role "customer") hold no project permissions (see authz), so the agent
// UI and API refuse them. Everything they do goes through this package: it checks that they may
// raise requests in the project, or may see the request (they reported it, it is shared with
// them, or they are asked to approve it), and only then acts for them with a portal actor limited
// to that project. What it returns to them never includes internal notes, agents' attachments or
// history. A request the portal does not know (or one the caller may not see) is "not found".
package servicedesk

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/config"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/identity/local"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/notify"
	"github.com/workflowproj/workflow/internal/tracker"
)

type Service struct {
	Pool    *pgxpool.Pool
	Q       *dbq.Queries
	Tracker *tracker.Service
	Notify  *notify.Service
	Local   *local.Service
	Jobs    jobs.Enqueuer
	Cfg     *config.Config
	Log     *slog.Logger
	Now     func() time.Time
}

func New(pool *pgxpool.Pool, tr *tracker.Service, nt *notify.Service, loc *local.Service, enq jobs.Enqueuer, cfg *config.Config, log *slog.Logger) *Service {
	return &Service{Pool: pool, Q: dbq.New(pool), Tracker: tr, Notify: nt, Local: loc, Jobs: enq, Cfg: cfg, Log: log, Now: time.Now}
}

func (s *Service) tx(ctx context.Context, fn func(tx pgx.Tx, q *dbq.Queries) error) error {
	return db.Tx(ctx, s.Pool, fn)
}

// Statuses the service desk relies on by name (they come with the service template).
const (
	statusWaitingForSupport  = "Waiting for support"
	statusWaitingForCustomer = "Waiting for customer"
	statusPendingApproval    = "Pending approval"
)

// desk loads a service project for an agent who holds perm in it.
func (s *Service) desk(ctx context.Context, actor *authz.Actor, ref string, perm authz.Permission) (dbq.Project, dbq.ServiceDesk, authz.Access, error) {
	p, access, err := s.Tracker.ProjectFor(ctx, actor, ref, perm)
	if err != nil {
		return p, dbq.ServiceDesk{}, access, err
	}
	if !tracker.IsServiceProject(p) {
		return p, dbq.ServiceDesk{}, access, apperr.NotFound("service desk")
	}
	d, err := s.Q.GetServiceDesk(ctx, p.ID)
	if err != nil {
		return p, d, access, apperr.NotFound("service desk")
	}
	return p, d, access, nil
}

// PortalURL is the portal home of a desk.
func (s *Service) PortalURL(p dbq.Project) string { return s.Cfg.PublicURL + "/portal/" + p.Key }

// RequestURL opens a request in the portal.
func (s *Service) RequestURL(key string) string { return s.Cfg.PublicURL + "/portal/requests/" + key }

// ---------------------------------------------------------------------------
// Settings

func (s *Service) deskView(ctx context.Context, p dbq.Project, d dbq.ServiceDesk) (api.ServiceDesk, error) {
	n, err := s.Q.CountServiceCustomers(ctx, p.ID)
	if err != nil {
		return api.ServiceDesk{}, err
	}
	notes, err := s.releaseNotesProjects(ctx, s.Q, p.ID)
	if err != nil {
		return api.ServiceDesk{}, err
	}
	return api.ServiceDesk{
		ProjectKey: p.Key, PortalName: d.PortalName, PortalDescription: d.PortalDescription,
		PortalAccess: api.PortalAccess(d.PortalAccess), SelfSignup: d.SelfSignup, PortalUrl: s.PortalURL(p),
		CustomerCount: int(n), OpenProject: p.OpenRoleID != nil, ReleaseNotesProjects: notes,
	}, nil
}

// Desk returns a service project's portal settings (agents).
func (s *Service) Desk(ctx context.Context, actor *authz.Actor, ref string) (api.ServiceDesk, error) {
	p, d, _, err := s.desk(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return api.ServiceDesk{}, err
	}
	return s.deskView(ctx, p, d)
}

// UpdateDesk changes the portal settings (project admins).
func (s *Service) UpdateDesk(ctx context.Context, actor *authz.Actor, ref string, in api.ServiceDeskPatch) (api.ServiceDesk, error) {
	p, d, _, err := s.desk(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return api.ServiceDesk{}, err
	}
	params := dbq.UpdateServiceDeskParams{ProjectID: p.ID, PortalName: d.PortalName, PortalDescription: d.PortalDescription, PortalAccess: d.PortalAccess, SelfSignup: d.SelfSignup}
	if in.PortalName != nil {
		if params.PortalName = strings.TrimSpace(*in.PortalName); params.PortalName == "" {
			return api.ServiceDesk{}, apperr.Invalid("portalName", "the portal needs a name")
		}
	}
	if in.PortalDescription != nil {
		params.PortalDescription = strings.TrimSpace(*in.PortalDescription)
	}
	if in.PortalAccess != nil {
		if !in.PortalAccess.Valid() {
			return api.ServiceDesk{}, apperr.Invalid("portalAccess", "portal access is customers or organisation")
		}
		params.PortalAccess = string(*in.PortalAccess)
	}
	if in.SelfSignup != nil {
		params.SelfSignup = *in.SelfSignup
	}
	if in.ReleaseNotesProjectKeys != nil {
		if err := s.setReleaseNotesProjects(ctx, actor, p, *in.ReleaseNotesProjectKeys); err != nil {
			return api.ServiceDesk{}, err
		}
	}
	d, err = s.Q.UpdateServiceDesk(ctx, params)
	if err != nil {
		return api.ServiceDesk{}, err
	}
	audit := map[string]any{"portalName": d.PortalName, "portalAccess": d.PortalAccess, "selfSignup": d.SelfSignup}
	if in.ReleaseNotesProjectKeys != nil {
		audit["releaseNotesProjects"] = *in.ReleaseNotesProjectKeys
	}
	tracker.Audit(ctx, s.Q, actor, "service_desk.update", "project", p.ID.String(), &p.ID, audit)
	return s.deskView(ctx, p, d)
}

// ---------------------------------------------------------------------------
// A new service project

// ProjectCreated sets up a project created from the service template, in its transaction
// (tracker.AfterCreateProject): the portal, starter request types, SLAs and queues.
func (s *Service) ProjectCreated(ctx context.Context, _ pgx.Tx, q *dbq.Queries, _ *authz.Actor, p dbq.Project) error {
	if !tracker.IsServiceProject(p) {
		return nil
	}
	if _, err := q.CreateServiceDesk(ctx, dbq.CreateServiceDeskParams{
		ProjectID: p.ID, PortalName: p.Name, PortalDescription: "Ask the " + p.Name + " team for help, report a problem or request something new.",
	}); err != nil {
		return err
	}
	types, err := q.ListIssueTypes(ctx, p.ID)
	if err != nil {
		return err
	}
	typeID := map[string]uuid.UUID{}
	for _, t := range types {
		typeID[t.Key] = t.ID
	}
	for i, rt := range starterRequestTypes {
		id, ok := typeID[rt.issueType]
		if !ok {
			continue
		}
		form, err := encodeForm(rt.fields)
		if err != nil {
			return err
		}
		if _, err := q.CreateRequestType(ctx, dbq.CreateRequestTypeParams{
			ProjectID: p.ID, Name: rt.name, Description: rt.description, Icon: rt.icon, IssueTypeID: id,
			PortalGroup: rt.group, Position: int32(i), Form: form,
		}); err != nil {
			return err
		}
	}
	if err := s.seedSLAs(ctx, q, p); err != nil {
		return err
	}
	for i, qu := range defaultQueues {
		if _, err := q.CreateQueue(ctx, dbq.CreateQueueParams{ProjectID: p.ID, Name: qu.name, Jql: qu.jql, Position: int32(i)}); err != nil {
			return err
		}
	}
	return nil
}

type starterType struct {
	name, description, icon, group, issueType string
	fields                                    []FormField
}

var starterRequestTypes = []starterType{
	{
		name: "Get IT help", description: "Ask for help with something that isn't working as expected, or a question.", icon: "help", group: "General",
		issueType: "service_request",
		fields: []FormField{
			{Field: "summary", Label: "Summary", Help: "A few words about what you need", Required: true},
			{Field: "description", Label: "Details", Help: "What happened, and what did you expect?"},
		},
	},
	{
		name: "Report a problem", description: "Something is broken or unavailable.", icon: "bug", group: "General", issueType: "incident",
		fields: []FormField{
			{Field: "summary", Label: "What is broken?", Required: true},
			{Field: "description", Label: "Details", Help: "Steps to reproduce, error messages, who is affected"},
			{Field: "priority", Label: "How urgent is it?"},
		},
	},
	{
		name: "Request new hardware", description: "A laptop, a monitor, a phone or other equipment.", icon: "laptop", group: "Requests", issueType: "service_request",
		fields: []FormField{
			{Field: "summary", Label: "What do you need?", Required: true},
			{Field: "description", Label: "Why do you need it?"},
			{Field: "dueDate", Label: "Needed by"},
		},
	},
	{
		name: "Request a change", description: "A change to a system or service you use.", icon: "change", group: "Requests", issueType: "change",
		fields: []FormField{
			{Field: "summary", Label: "Summary", Required: true},
			{Field: "description", Label: "What should change, and why?", Required: true},
		},
	},
}

type queueDef struct{ name, jql string }

var defaultQueues = []queueDef{
	{"Unassigned", `assignee is EMPTY AND resolution = Unresolved ORDER BY "Time to resolution" ASC, created ASC`},
	{"Assigned to me", `assignee = currentUser() AND resolution = Unresolved ORDER BY "Time to resolution" ASC, created ASC`},
	{"Waiting for customer", `status = "Waiting for customer" ORDER BY updated ASC`},
	{"SLA at risk", `resolution = Unresolved AND (("Time to first response" < remaining("1h") AND "Time to first response" > remaining("0m")) OR ("Time to resolution" < remaining("2h") AND "Time to resolution" > remaining("0m"))) ORDER BY "Time to resolution" ASC`},
	{"SLA breached", `resolution = Unresolved AND ("Time to first response" = breached() OR "Time to resolution" = breached()) ORDER BY "Time to resolution" ASC`},
	{"All open", `resolution = Unresolved ORDER BY created DESC`},
}
