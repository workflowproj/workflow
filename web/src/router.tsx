import { createRootRouteWithContext, createRoute, createRouter, lazyRouteComponent, Outlet, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { meQuery, projectQuery } from "@/api/queries";
import { AppShell } from "@/layout/AppShell";
import { NotFound } from "@/layout/NotFound";
import { PageSpinner } from "@/components/ui";
import { LoginPage } from "@/pages/LoginPage";
import { ProjectsPage } from "@/pages/ProjectsPage";
import { ProjectLayout } from "@/pages/project/ProjectLayout";
import { BoardPage } from "@/pages/project/BoardPage";
import { BacklogPage } from "@/pages/project/BacklogPage";
import { ProjectIssuesPage } from "@/pages/project/ProjectIssuesPage";
import { SearchPage } from "@/pages/issues/SearchPage";
import { IssuePage } from "@/pages/issues/IssuePage";
import { SettingsLayout } from "@/pages/settings/SettingsLayout";
import { AdminLayout } from "@/pages/admin/AdminLayout";

/*
 * Route tree (code-based). Pages read params and search with getRouteApi(<id>), e.g.
 *   const route = getRouteApi("/app/projects/$projectKey/board");
 *   const { projectKey } = route.useParams(); const { issue } = route.useSearch();
 * Route ids are the full paths prefixed with "/app" for signed-in pages (see router.test.ts).
 * Settings, administration, reports, dashboards and personal pages load on demand to keep the first load small.
 */

const str = (v: unknown) => (typeof v === "string" && v !== "" ? v : undefined);

export type IssueSearch = { issue?: string };
const issueSearch = (s: Record<string, unknown>): IssueSearch => ({ issue: str(s.issue) });

const rootRoute = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Outlet,
  notFoundComponent: NotFound,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (s: Record<string, unknown>): { error?: string; returnTo?: string; notice?: string; email?: string } => ({
    error: str(s.error),
    returnTo: str(s.returnTo),
    notice: str(s.notice),
    email: str(s.email),
  }),
  component: LoginPage,
});
// Local accounts: invitation and password-reset links, and asking for a reset (signed out).
const forgotPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/forgot-password",
  validateSearch: (s: Record<string, unknown>): { email?: string } => ({ email: str(s.email) }),
  component: lazyRouteComponent(() => import("@/pages/auth/ForgotPasswordPage"), "ForgotPasswordPage"),
});
const tokenSearch = (s: Record<string, unknown>): { token?: string } => ({ token: str(s.token) });
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: "/invite", validateSearch: tokenSearch, component: lazyRouteComponent(() => import("@/pages/auth/SetPasswordPage"), "InvitePage") });
const resetPasswordRoute = createRoute({ getParentRoute: () => rootRoute, path: "/reset-password", validateSearch: tokenSearch, component: lazyRouteComponent(() => import("@/pages/auth/SetPasswordPage"), "ResetPasswordPage") });

// OAuth consent for AI assistants, outside the app shell: /oauth/consent/<request> asks the
// signed-in person (signing in first if needed); /oauth/consent?error=… explains a refused request.
const consentErrorRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/oauth/consent",
  validateSearch: (s: Record<string, unknown>): { error?: string } => ({ error: str(s.error) }),
  component: lazyRouteComponent(() => import("@/pages/oauth/ConsentPage"), "ConsentErrorPage"),
});
const consentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/oauth/consent/$request",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: "/login", search: { returnTo: location.href } });
      throw e;
    }
  },
  component: lazyRouteComponent(() => import("@/pages/oauth/ConsentPage"), "ConsentPage"),
});

// Everything else requires a session; a 401 sends people to the login page and back.
// Customers only use the help centre: the agent app sends them to the portal.
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    let me;
    try {
      me = await context.queryClient.ensureQueryData(meQuery);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        throw redirect({ to: "/login", search: { returnTo: location.href } });
      }
      throw e;
    }
    if (me.orgRole === "customer") throw redirect({ to: location.pathname.startsWith("/profile") ? "/portal/profile" : "/portal" });
  },
  component: AppShell,
});

// The help centre (customer portal): its own layout, for customers and anyone raising a request.
const portalRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/portal",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) throw redirect({ to: "/login", search: { returnTo: location.href } });
      throw e;
    }
  },
  component: lazyRouteComponent(() => import("@/pages/portal/PortalLayout"), "PortalLayout"),
});
const portalHomeRoute = createRoute({ getParentRoute: () => portalRoute, path: "/", component: lazyRouteComponent(() => import("@/pages/portal/PortalHome"), "PortalHome") });
const portalRequestsRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: "/requests",
  validateSearch: (s: Record<string, unknown>): { status?: "open" | "closed" | "all" } => ({ status: s.status === "closed" || s.status === "all" || s.status === "open" ? s.status : undefined }),
  component: lazyRouteComponent(() => import("@/pages/portal/PortalRequestsPage"), "PortalRequestsPage"),
});
const portalRequestRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: "/requests/$issueKey",
  validateSearch: (s: Record<string, unknown>): { created?: boolean } => ({ created: s.created === true || s.created === "true" ? true : undefined }),
  component: lazyRouteComponent(() => import("@/pages/portal/PortalRequestPage"), "PortalRequestPage"),
});
const portalApprovalsRoute = createRoute({ getParentRoute: () => portalRoute, path: "/approvals", component: lazyRouteComponent(() => import("@/pages/portal/PortalRequestsPage"), "PortalApprovalsPage") });
const portalProfileRoute = createRoute({ getParentRoute: () => portalRoute, path: "/profile", component: lazyRouteComponent(() => import("@/pages/portal/PortalProfilePage"), "PortalProfilePage") });
const portalDeskRoute = createRoute({ getParentRoute: () => portalRoute, path: "/$projectKey", component: lazyRouteComponent(() => import("@/pages/portal/PortalDeskPage"), "PortalDeskPage") });
const portalCreateRoute = createRoute({ getParentRoute: () => portalRoute, path: "/$projectKey/new/$requestTypeId", component: lazyRouteComponent(() => import("@/pages/portal/PortalCreatePage"), "PortalCreatePage") });
// Self sign-up (signed out), for desks that allow it.
const portalSignupRoute = createRoute({ getParentRoute: () => rootRoute, path: "/portal/signup/$projectKey", component: lazyRouteComponent(() => import("@/pages/portal/PortalSignupPage"), "PortalSignupPage") });

const indexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/projects" });
  },
});

const projectsRoute = createRoute({ getParentRoute: () => appRoute, path: "/projects", component: ProjectsPage });

const projectRoute = createRoute({ getParentRoute: () => appRoute, path: "/projects/$projectKey", component: ProjectLayout });
const projectIndexRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/",
  beforeLoad: async ({ context, params }) => {
    // Service projects open on their queues.
    const p = await context.queryClient.ensureQueryData(projectQuery(params.projectKey)).catch(() => null);
    throw redirect({ to: p?.template === "service" ? "/projects/$projectKey/queues" : "/projects/$projectKey/board", params });
  },
});
const boardRoute = createRoute({ getParentRoute: () => projectRoute, path: "/board", validateSearch: issueSearch, component: BoardPage });
const backlogRoute = createRoute({ getParentRoute: () => projectRoute, path: "/backlog", validateSearch: issueSearch, component: BacklogPage });
const projectIssuesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/issues",
  validateSearch: (s: Record<string, unknown>): { jql?: string; issue?: string } => ({ jql: str(s.jql), issue: str(s.issue) }),
  component: ProjectIssuesPage,
});
export type TimelineSearch = { projects?: string; jql?: string; issue?: string };
const timelineSearch = (s: Record<string, unknown>): TimelineSearch => ({ projects: str(s.projects), jql: str(s.jql), issue: str(s.issue) });
const projectTimelineRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/timeline",
  validateSearch: (s: Record<string, unknown>): { jql?: string; issue?: string } => ({ jql: str(s.jql), issue: str(s.issue) }),
  component: lazyRouteComponent(() => import("@/pages/project/ProjectTimelinePage"), "ProjectTimelinePage"),
});
const queuesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/queues",
  validateSearch: (s: Record<string, unknown>): { queue?: string; issue?: string } => ({ queue: str(s.queue), issue: str(s.issue) }),
  component: lazyRouteComponent(() => import("@/pages/project/QueuesPage"), "QueuesPage"),
});
const releasesRoute = createRoute({ getParentRoute: () => projectRoute, path: "/releases", validateSearch: issueSearch, component: lazyRouteComponent(() => import("@/pages/project/ReleasesPage"), "ReleasesPage") });
// Risks and decisions: the project's own section, outside planning (shown when it is turned on).
export type RisksSearch = { cell?: string; status?: "closed" | "all"; issue?: string };
const risksRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/risks",
  validateSearch: (s: Record<string, unknown>): RisksSearch => ({ cell: str(s.cell), status: s.status === "closed" || s.status === "all" ? s.status : undefined, issue: str(s.issue) }),
  component: lazyRouteComponent(() => import("@/pages/project/RisksPage"), "RisksPage"),
});
const decisionsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/decisions",
  validateSearch: (s: Record<string, unknown>): { status?: string; issue?: string } => ({ status: str(s.status), issue: str(s.issue) }),
  component: lazyRouteComponent(() => import("@/pages/project/DecisionsPage"), "DecisionsPage"),
});
const reportsRoute = createRoute({ getParentRoute: () => projectRoute, path: "/reports", component: lazyRouteComponent(() => import("@/pages/project/ReportsPage"), "ReportsPage") });

const customersRoute = createRoute({ getParentRoute: () => projectRoute, path: "/customers", component: lazyRouteComponent(() => import("@/pages/project/CustomersPage"), "CustomersPage") });
const settingsRoute = createRoute({ getParentRoute: () => projectRoute, path: "/settings", component: SettingsLayout });
const settingsIndexRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/", component: lazyRouteComponent(() => import("@/pages/settings/GeneralSettings"), "GeneralSettings") });
const membersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/members", component: lazyRouteComponent(() => import("@/pages/settings/MembersSettings"), "MembersSettings") });
const issueTypesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/issue-types", component: lazyRouteComponent(() => import("@/pages/settings/IssueTypesSettings"), "IssueTypesSettings") });
const workflowsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/workflows", component: lazyRouteComponent(() => import("@/pages/settings/WorkflowsSettings"), "WorkflowsSettings") });
const workflowEditorRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/workflows/$workflowId", component: lazyRouteComponent(() => import("@/pages/settings/WorkflowEditorPage"), "WorkflowEditorPage") });
const boardsSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/boards", component: lazyRouteComponent(() => import("@/pages/settings/BoardsSettings"), "BoardsSettings") });
const repositoriesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/repositories", component: lazyRouteComponent(() => import("@/pages/settings/RepositoriesSettings"), "RepositoriesSettings") });
const automationRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/automation", component: lazyRouteComponent(() => import("@/pages/settings/AutomationSettings"), "AutomationSettings") });
const chatSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/chat", component: lazyRouteComponent(() => import("@/pages/settings/ChatSettings"), "ChatSettings") });
const projectWebhooksRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/webhooks", component: lazyRouteComponent(() => import("@/pages/settings/WebhooksSettings"), "WebhooksSettings") });
const portalSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/portal", component: lazyRouteComponent(() => import("@/pages/settings/ServiceDeskSettings"), "PortalSettings") });
const requestTypesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/request-types", component: lazyRouteComponent(() => import("@/pages/settings/RequestTypesSettings"), "RequestTypesSettings") });
const queuesSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/queues", component: lazyRouteComponent(() => import("@/pages/settings/ServiceDeskSettings"), "QueuesSettings") });
const slaSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/slas", component: lazyRouteComponent(() => import("@/pages/settings/SlaSettings"), "SlaSettings") });

const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/issues",
  validateSearch: (s: Record<string, unknown>): { jql?: string; filter?: string; issue?: string } => ({ jql: str(s.jql), filter: str(s.filter), issue: str(s.issue) }),
  component: SearchPage,
});
const timelineRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/timeline",
  validateSearch: timelineSearch,
  component: lazyRouteComponent(() => import("@/pages/TimelinePage"), "TimelinePage"),
});
export type TimesheetSearch = { from?: string; period?: "month"; projects?: string; user?: string };
const timesheetRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/timesheet",
  validateSearch: (s: Record<string, unknown>): TimesheetSearch => ({ from: str(s.from), period: s.period === "month" ? "month" : undefined, projects: str(s.projects), user: str(s.user) }),
  component: lazyRouteComponent(() => import("@/pages/TimesheetPage"), "TimesheetPage"),
});
export type DashboardSearch = { edit?: boolean; issue?: string };
const dashboardsRoute = createRoute({ getParentRoute: () => appRoute, path: "/dashboards", component: lazyRouteComponent(() => import("@/pages/DashboardsPage"), "DashboardsPage") });
const dashboardRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/dashboards/$dashboardId",
  validateSearch: (s: Record<string, unknown>): DashboardSearch => ({ edit: s.edit === true || s.edit === "true" || s.edit === "1" ? true : undefined, issue: str(s.issue) }),
  component: lazyRouteComponent(() => import("@/pages/DashboardPage"), "DashboardPage"),
});
const browseRoute = createRoute({ getParentRoute: () => appRoute, path: "/browse/$issueKey", component: IssuePage });
const notificationsRoute = createRoute({ getParentRoute: () => appRoute, path: "/notifications", component: lazyRouteComponent(() => import("@/pages/me/NotificationsPage"), "NotificationsPage") });
const profileRoute = createRoute({ getParentRoute: () => appRoute, path: "/profile", component: lazyRouteComponent(() => import("@/pages/me/ProfilePage"), "ProfilePage") });

const adminRoute = createRoute({ getParentRoute: () => appRoute, path: "/admin", component: AdminLayout });
const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/admin/users" });
  },
});
const adminUsersRoute = createRoute({ getParentRoute: () => adminRoute, path: "/users", component: lazyRouteComponent(() => import("@/pages/admin/UsersAdmin"), "UsersAdmin") });
const adminGroupsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/groups", component: lazyRouteComponent(() => import("@/pages/admin/GroupsAdmin"), "GroupsAdmin") });
const adminRolesRoute = createRoute({ getParentRoute: () => adminRoute, path: "/roles", component: lazyRouteComponent(() => import("@/pages/admin/RolesAdmin"), "RolesAdmin") });
const adminFieldsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/fields", component: lazyRouteComponent(() => import("@/pages/admin/FieldsAdmin"), "FieldsAdmin") });
const adminProvisioningRoute = createRoute({ getParentRoute: () => adminRoute, path: "/provisioning", component: lazyRouteComponent(() => import("@/pages/admin/ProvisioningAdmin"), "ProvisioningAdmin") });
const adminScmRoute = createRoute({ getParentRoute: () => adminRoute, path: "/scm", component: lazyRouteComponent(() => import("@/pages/admin/ScmAdmin"), "ScmAdmin") });
const adminWebhooksRoute = createRoute({ getParentRoute: () => adminRoute, path: "/webhooks", component: lazyRouteComponent(() => import("@/pages/admin/WebhooksAdmin"), "WebhooksAdmin") });
const adminAuditRoute = createRoute({ getParentRoute: () => adminRoute, path: "/audit", component: lazyRouteComponent(() => import("@/pages/admin/AuditAdmin"), "AuditAdmin") });
const adminConnectedAppsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/connected-apps", component: lazyRouteComponent(() => import("@/pages/admin/ConnectedAppsAdmin"), "ConnectedAppsAdmin") });
const adminEmailRoute = createRoute({ getParentRoute: () => adminRoute, path: "/email", component: lazyRouteComponent(() => import("@/pages/admin/EmailAdmin"), "EmailAdmin") });
const adminImportRoute = createRoute({ getParentRoute: () => adminRoute, path: "/import", component: lazyRouteComponent(() => import("@/pages/admin/JiraImportAdmin"), "JiraImportAdmin") });
const adminImportWizardRoute = createRoute({ getParentRoute: () => adminRoute, path: "/import/$importId", component: lazyRouteComponent(() => import("@/pages/admin/JiraImportWizard"), "JiraImportWizard") });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  consentErrorRoute,
  consentRoute,
  forgotPasswordRoute,
  inviteRoute,
  resetPasswordRoute,
  portalSignupRoute,
  portalRoute.addChildren([portalHomeRoute, portalRequestsRoute, portalRequestRoute, portalApprovalsRoute, portalProfileRoute, portalDeskRoute, portalCreateRoute]),
  appRoute.addChildren([
    indexRoute,
    projectsRoute,
    projectRoute.addChildren([
      projectIndexRoute,
      boardRoute,
      backlogRoute,
      projectTimelineRoute,
      projectIssuesRoute,
      queuesRoute,
      customersRoute,
      releasesRoute,
      risksRoute,
      decisionsRoute,
      reportsRoute,
      settingsRoute.addChildren([
        settingsIndexRoute,
        membersRoute,
        issueTypesRoute,
        workflowsRoute,
        workflowEditorRoute,
        boardsSettingsRoute,
        repositoriesRoute,
        automationRoute,
        chatSettingsRoute,
        projectWebhooksRoute,
        portalSettingsRoute,
        requestTypesRoute,
        queuesSettingsRoute,
        slaSettingsRoute,
      ]),
    ]),
    searchRoute,
    timelineRoute,
    timesheetRoute,
    dashboardsRoute,
    dashboardRoute,
    browseRoute,
    notificationsRoute,
    profileRoute,
    adminRoute.addChildren([
      adminIndexRoute,
      adminUsersRoute,
      adminGroupsRoute,
      adminRolesRoute,
      adminFieldsRoute,
      adminProvisioningRoute,
      adminScmRoute,
      adminWebhooksRoute,
      adminAuditRoute,
      adminConnectedAppsRoute,
      adminEmailRoute,
      adminImportRoute,
      adminImportWizardRoute,
    ]),
  ]),
]);

export function makeRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    defaultPendingComponent: () => <PageSpinner />,
    defaultPendingMs: 150,
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
