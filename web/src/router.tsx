import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { meQuery } from "@/api/queries";
import { AppShell } from "@/layout/AppShell";
import { NotFound } from "@/layout/NotFound";
import { LoginPage } from "@/pages/LoginPage";
import { ProjectsPage } from "@/pages/ProjectsPage";
import { ProjectLayout } from "@/pages/project/ProjectLayout";
import { BoardPage } from "@/pages/project/BoardPage";
import { BacklogPage } from "@/pages/project/BacklogPage";
import { ProjectIssuesPage } from "@/pages/project/ProjectIssuesPage";
import { ReleasesPage } from "@/pages/project/ReleasesPage";
import { ReportsPage } from "@/pages/project/ReportsPage";
import { SearchPage } from "@/pages/issues/SearchPage";
import { IssuePage } from "@/pages/issues/IssuePage";
import { SettingsLayout } from "@/pages/settings/SettingsLayout";
import { GeneralSettings } from "@/pages/settings/GeneralSettings";
import { MembersSettings } from "@/pages/settings/MembersSettings";
import { IssueTypesSettings } from "@/pages/settings/IssueTypesSettings";
import { WorkflowsSettings } from "@/pages/settings/WorkflowsSettings";
import { WorkflowEditorPage } from "@/pages/settings/WorkflowEditorPage";
import { BoardsSettings } from "@/pages/settings/BoardsSettings";
import { RepositoriesSettings } from "@/pages/settings/RepositoriesSettings";
import { AutomationSettings } from "@/pages/settings/AutomationSettings";
import { WebhooksSettings } from "@/pages/settings/WebhooksSettings";
import { AdminLayout } from "@/pages/admin/AdminLayout";
import { UsersAdmin } from "@/pages/admin/UsersAdmin";
import { GroupsAdmin } from "@/pages/admin/GroupsAdmin";
import { RolesAdmin } from "@/pages/admin/RolesAdmin";
import { FieldsAdmin } from "@/pages/admin/FieldsAdmin";
import { ProvisioningAdmin } from "@/pages/admin/ProvisioningAdmin";
import { ScmAdmin } from "@/pages/admin/ScmAdmin";
import { WebhooksAdmin } from "@/pages/admin/WebhooksAdmin";
import { AuditAdmin } from "@/pages/admin/AuditAdmin";
import { NotificationsPage } from "@/pages/me/NotificationsPage";
import { ProfilePage } from "@/pages/me/ProfilePage";

/*
 * Route tree (code-based). Pages read params and search with getRouteApi(<id>), e.g.
 *   const route = getRouteApi("/app/projects/$projectKey/board");
 *   const { projectKey } = route.useParams(); const { issue } = route.useSearch();
 * Route ids are the full paths prefixed with "/app" for signed-in pages (see router.test.ts).
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
const releasesRoute = createRoute({ getParentRoute: () => projectRoute, path: "/releases", validateSearch: issueSearch, component: ReleasesPage });
const reportsRoute = createRoute({ getParentRoute: () => projectRoute, path: "/reports", component: ReportsPage });

const settingsRoute = createRoute({ getParentRoute: () => projectRoute, path: "/settings", component: SettingsLayout });
const settingsIndexRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/", component: GeneralSettings });
const membersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/members", component: MembersSettings });
const issueTypesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/issue-types", component: IssueTypesSettings });
const workflowsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/workflows", component: WorkflowsSettings });
const workflowEditorRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/workflows/$workflowId", component: WorkflowEditorPage });
const boardsSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/boards", component: BoardsSettings });
const repositoriesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/repositories", component: RepositoriesSettings });
const automationRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/automation", component: AutomationSettings });
const projectWebhooksRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/webhooks", component: WebhooksSettings });

const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/issues",
  validateSearch: (s: Record<string, unknown>): { jql?: string; filter?: string; issue?: string } => ({ jql: str(s.jql), filter: str(s.filter), issue: str(s.issue) }),
  component: SearchPage,
});
const browseRoute = createRoute({ getParentRoute: () => appRoute, path: "/browse/$issueKey", component: IssuePage });
const notificationsRoute = createRoute({ getParentRoute: () => appRoute, path: "/notifications", component: NotificationsPage });
const profileRoute = createRoute({ getParentRoute: () => appRoute, path: "/profile", component: ProfilePage });

const adminRoute = createRoute({ getParentRoute: () => appRoute, path: "/admin", component: AdminLayout });
const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/admin/users" });
  },
});
const adminUsersRoute = createRoute({ getParentRoute: () => adminRoute, path: "/users", component: UsersAdmin });
const adminGroupsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/groups", component: GroupsAdmin });
const adminRolesRoute = createRoute({ getParentRoute: () => adminRoute, path: "/roles", component: RolesAdmin });
const adminFieldsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/fields", component: FieldsAdmin });
const adminProvisioningRoute = createRoute({ getParentRoute: () => adminRoute, path: "/provisioning", component: ProvisioningAdmin });
const adminScmRoute = createRoute({ getParentRoute: () => adminRoute, path: "/scm", component: ScmAdmin });
const adminWebhooksRoute = createRoute({ getParentRoute: () => adminRoute, path: "/webhooks", component: WebhooksAdmin });
const adminAuditRoute = createRoute({ getParentRoute: () => adminRoute, path: "/audit", component: AuditAdmin });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    indexRoute,
    projectsRoute,
    projectRoute.addChildren([
      projectIndexRoute,
      boardRoute,
      backlogRoute,
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
        projectWebhooksRoute,
      ]),
    ]),
    searchRoute,
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
    ]),
  ]),
]);

export function makeRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
