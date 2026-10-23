import { queryOptions, useQueries, useQuery } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { issueTypesQuery } from "@/api/queries";
import type { StatusCategory } from "@/api/types";

/*
 * Queries for configuration pages. Keys follow api/queries.ts: the first element names the
 * resource so mutations can invalidate by prefix.
 */

export type Workflow = Schemas["Workflow"];
export type WorkflowDetail = Schemas["WorkflowDetail"];
export type Role = Schemas["Role"];

export const rolesQuery = queryOptions({
  queryKey: ["roles"],
  queryFn: () => unwrap(api.GET("/roles")),
  staleTime: 60_000,
});

export const permissionsQuery = queryOptions({
  queryKey: ["permissions"],
  queryFn: () => unwrap(api.GET("/permissions")),
  staleTime: Infinity,
});

export const workflowsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["workflows", projectKey],
    queryFn: () => unwrap(api.GET("/workflows", { params: { query: { project: projectKey } } })),
    staleTime: 30_000,
  });

export const workflowQuery = (id: string) =>
  queryOptions({
    queryKey: ["workflow", id],
    queryFn: () => unwrap(api.GET("/workflows/{workflowId}", { params: { path: { workflowId: id } } })),
    staleTime: 30_000,
  });

export const boardsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["boards", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/boards", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

export type ProjectStatus = { id: string; name: string; category: StatusCategory; workflows: string[] };

/**
 * useProjectStatuses lists the statuses the project's issue types can be in, gathered from the
 * current version of each workflow the project uses (in workflow order, deduplicated).
 */
export function useProjectStatuses(projectKey: string) {
  const types = useQuery(issueTypesQuery(projectKey));
  const ids = [...new Set((types.data ?? []).map((t) => t.workflowId))];
  const workflows = useQueries({ queries: ids.map((id) => workflowQuery(id)) });
  const loading = types.isLoading || workflows.some((w) => w.isLoading);
  const error = types.error ?? workflows.find((w) => w.error)?.error;
  const byId = new Map<string, ProjectStatus>();
  for (const w of workflows) {
    if (!w.data) continue;
    for (const s of w.data.version.definition.statuses) {
      const cur = byId.get(s.id);
      if (cur) cur.workflows.push(w.data.name);
      else byId.set(s.id, { id: s.id, name: s.name, category: s.category, workflows: [w.data.name] });
    }
  }
  const order: Record<StatusCategory, number> = { todo: 0, in_progress: 1, done: 2 };
  const statuses = [...byId.values()].sort((a, b) => order[a.category] - order[b.category]);
  return { statuses, loading, error };
}
