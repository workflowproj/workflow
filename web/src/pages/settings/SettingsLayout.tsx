import { getRouteApi, Link, Outlet, useMatchRoute } from "@tanstack/react-router";
import { Bot, FolderGit2, GitBranch, KanbanSquare, Layers, MessagesSquare, Settings, Users, Webhook } from "lucide-react";
import { hasPermission, useProject } from "@/api/queries";
import { EmptyState } from "@/components/ui";

const route = getRouteApi("/app/projects/$projectKey/settings");

const sections = [
  { label: "General", to: "/projects/$projectKey/settings" as const, exact: true, icon: Settings },
  { label: "Members", to: "/projects/$projectKey/settings/members" as const, icon: Users },
  { label: "Issue types", to: "/projects/$projectKey/settings/issue-types" as const, icon: Layers },
  { label: "Workflows", to: "/projects/$projectKey/settings/workflows" as const, icon: GitBranch },
  { label: "Boards", to: "/projects/$projectKey/settings/boards" as const, icon: KanbanSquare },
  { label: "Repositories", to: "/projects/$projectKey/settings/repositories" as const, icon: FolderGit2 },
  { label: "Automation", to: "/projects/$projectKey/settings/automation" as const, icon: Bot },
  { label: "Chat", to: "/projects/$projectKey/settings/chat" as const, icon: MessagesSquare },
  { label: "Webhooks", to: "/projects/$projectKey/settings/webhooks" as const, icon: Webhook },
];

export function SettingsLayout() {
  const { projectKey } = route.useParams();
  const { data: project } = useProject(projectKey);
  const matchRoute = useMatchRoute();
  if (project && !hasPermission(project, "project.admin")) {
    return <EmptyState title="Project administrators only" body="You need the project admin permission to change these settings." />;
  }
  // The workflow editor needs the whole width for its canvas; it has its own breadcrumb back.
  if (matchRoute({ to: "/projects/$projectKey/settings/workflows/$workflowId" })) {
    return (
      <div className="p-6">
        <Outlet />
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-6 p-6 md:flex-row">
      <nav aria-label="Project settings" className="flex shrink-0 gap-1 overflow-x-auto md:w-48 md:flex-col">
        {sections.map((s) => (
          <Link
            key={s.to}
            to={s.to}
            params={{ projectKey }}
            activeOptions={{ exact: s.exact ?? false }}
            className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm whitespace-nowrap text-muted hover:bg-surface-2 hover:text-ink"
            activeProps={{ className: "!bg-accent-soft !text-accent font-medium" }}
          >
            <s.icon className="size-4 shrink-0" aria-hidden />
            {s.label}
          </Link>
        ))}
      </nav>
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}
