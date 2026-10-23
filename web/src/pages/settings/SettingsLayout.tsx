import { getRouteApi, Link, Outlet } from "@tanstack/react-router";
import { hasPermission, useProject } from "@/api/queries";
import { EmptyState } from "@/components/ui";

const route = getRouteApi("/app/projects/$projectKey/settings");

const sections = [
  { label: "General", to: "/projects/$projectKey/settings" as const, exact: true },
  { label: "Members", to: "/projects/$projectKey/settings/members" as const },
  { label: "Issue types", to: "/projects/$projectKey/settings/issue-types" as const },
  { label: "Workflows", to: "/projects/$projectKey/settings/workflows" as const },
  { label: "Boards", to: "/projects/$projectKey/settings/boards" as const },
  { label: "Repositories", to: "/projects/$projectKey/settings/repositories" as const },
  { label: "Automation", to: "/projects/$projectKey/settings/automation" as const },
  { label: "Webhooks", to: "/projects/$projectKey/settings/webhooks" as const },
];

export function SettingsLayout() {
  const { projectKey } = route.useParams();
  const { data: project } = useProject(projectKey);
  if (project && !hasPermission(project, "project.admin")) {
    return <EmptyState title="Project administrators only" body="You need the project admin permission to change these settings." />;
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
            className="rounded-md px-3 py-1.5 text-sm whitespace-nowrap text-muted hover:bg-surface-2 hover:text-ink"
            activeProps={{ className: "!bg-accent-soft !text-accent font-medium" }}
          >
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
