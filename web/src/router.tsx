import { createRootRouteWithContext, createRoute, createRouter, lazyRouteComponent, Outlet, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { meQuery } from "@/api/queries";
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
  validateSearch: (s: Record<string, unknown>): { error?: string; returnTo?: string } => ({ error: str(s.error), returnTo: str(s.returnTo) }),
  component: LoginPage,
});

// Everything else requires a session; a 401 sends people to the login page and back.
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        throw redirect({ to: "/login", search: { returnTo: location.href } });
      }
      throw e;
    }
  },
  component: AppShell,
});

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
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/projects/$projectKey/board", params });
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
const releasesRoute = createRoute({ getParentRoute: () => projectRoute, path: "/releases", validateSearch: issueSearch, component: lazyRouteComponent(() => import("@/pages/project/ReleasesPage"), "ReleasesPage") });
const reportsRoute = createRoute({ getParentRoute: () => projectRoute, path: "/reports", component: lazyRouteComponent(() => import("@/pages/project/ReportsPage"), "ReportsPage") });

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
const adminImportRoute = createRoute({ getParentRoute: () => adminRoute, path: "/import", component: lazyRouteComponent(() => import("@/pages/admin/JiraImportAdmin"), "JiraImportAdmin") });
const adminImportWizardRoute = createRoute({ getParentRoute: () => adminRoute, path: "/import/$importId", component: lazyRouteComponent(() => import("@/pages/admin/JiraImportWizard"), "JiraImportWizard") });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    indexRoute,
    projectsRoute,
    projectRoute.addChildren([
      projectIndexRoute,
      boardRoute,
      backlogRoute,
      projectTimelineRoute,
      projectIssuesRoute,
      releasesRoute,
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
      ]),
    ]),
    searchRoute,
    timelineRoute,
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
