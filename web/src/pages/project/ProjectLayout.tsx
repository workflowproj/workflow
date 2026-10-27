import { getRouteApi, Outlet } from "@tanstack/react-router";
import { LifeBuoy } from "lucide-react";
import { hasPermission, useProject } from "@/api/queries";
import { PageSpinner, TabLinks } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ProjectAvatar } from "@/layout/AppShell";
import { useProjectEvents } from "@/lib/events";

const route = getRouteApi("/app/projects/$projectKey");

/** ProjectLayout is the project header and tabs; it also keeps the project's live updates open. */
export function ProjectLayout() {
  const { projectKey } = route.useParams();
  const { data: project, isLoading, error } = useProject(projectKey);
  useProjectEvents(project?.key);
  if (isLoading) return <PageSpinner />;
  if (error || !project) return <div className="p-6"><ErrorBox error={error ?? new Error("Project not found")} /></div>;
  const params = { projectKey: project.key };
  const service = project.template === "service";
  // Risks and decisions sit beside the issues, outside planning, when the project keeps them.
  const section = project.risksDecisions
    ? [
        { label: "Risks", to: "/projects/$projectKey/risks" as const, params },
        { label: "Decisions", to: "/projects/$projectKey/decisions" as const, params },
      ]
    : [];
  const tabs = service
    ? [
        // Service projects: queues first, and customers instead of the planning views.
        { label: "Queues", to: "/projects/$projectKey/queues" as const, params },
        { label: "Board", to: "/projects/$projectKey/board" as const, params },
        { label: "Issues", to: "/projects/$projectKey/issues" as const, params },
        ...section,
        { label: "Customers", to: "/projects/$projectKey/customers" as const, params },
        { label: "Reports", to: "/projects/$projectKey/reports" as const, params },
      ]
    : [
        { label: "Board", to: "/projects/$projectKey/board" as const, params },
        { label: "Backlog", to: "/projects/$projectKey/backlog" as const, params },
        { label: "Timeline", to: "/projects/$projectKey/timeline" as const, params },
        { label: "Issues", to: "/projects/$projectKey/issues" as const, params },
        ...section,
        { label: "Releases", to: "/projects/$projectKey/releases" as const, params },
        { label: "Reports", to: "/projects/$projectKey/reports" as const, params },
      ];
  if (hasPermission(project, "project.admin")) tabs.push({ label: "Settings", to: "/projects/$projectKey/settings" as never, params });
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 border-b border-line bg-surface px-6 pt-4">
        <div className="mb-3 flex items-center gap-3">
          <ProjectAvatar projectKey={project.key} size={32} />
          <div className="min-w-0">
            <h1 className="truncate text-lg leading-tight font-semibold">{project.name}</h1>
            <p className="text-xs text-muted">
              <span className="font-mono">{project.key}</span> · {project.access === "private" ? "Private" : "Open"} {service ? "service project" : "project"}
            </p>
          </div>
          {service && (
            <a href={`/portal/${project.key}`} target="_blank" rel="noreferrer" className="ml-auto hidden items-center gap-1.5 text-[13px] text-muted hover:text-ink sm:inline-flex">
              <LifeBuoy aria-hidden className="size-4" /> Help centre
            </a>
          )}
        </div>
        <div className="-mb-px [&>nav]:border-b-0">
          <TabLinks tabs={tabs} />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  );
}
