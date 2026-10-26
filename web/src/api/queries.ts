import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "./client";

/**
 * Shared queries. Query keys are arrays whose first element names the resource; mutations
 * invalidate by prefix and live events (lib/events.ts) invalidate the same prefixes:
 *
 *   ["me"], ["projects"], ["project", key], ["issue-types", projectKey], ["statuses"],
 *   ["users", q, project], ["issue", key], ["issue-comments", key], ["issue-history", key],
 *   ["boards", projectKey], ["board", boardId, ...], ["backlog", boardId], ["search", jql, ...], ["sprints", projectKey],
 *   ["releases", projectKey], ["notifications"], ["fields"], ["labels", projectKey],
 *   ["timeline", projects, jql, doneDays], ["dashboards"], ["dashboards", id],
 *   ["metrics", kind, projects, jql, ...] (dashboard gadgets; see features/dashboards/api.ts),
 *   ["risks", projectKey], ["decisions", projectKey] (see features/risks/api.ts)
 */

export const meQuery = queryOptions({
  queryKey: ["me"],
  queryFn: () => unwrap(api.GET("/me")),
  staleTime: 60_000,
  retry: false,
});

export const providersQuery = queryOptions({
  queryKey: ["auth-providers"],
  queryFn: () => unwrap(api.GET("/auth/providers")),
  staleTime: Infinity,
});

export const projectsQuery = queryOptions({
  queryKey: ["projects"],
  queryFn: () => unwrap(api.GET("/projects")),
  staleTime: 30_000,
});

export const projectQuery = (key: string) =>
  queryOptions({
    queryKey: ["project", key],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}", { params: { path: { projectKey: key } } })),
    staleTime: 30_000,
  });

export const issueTypesQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["issue-types", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/issue-types", { params: { path: { projectKey } } })),
    staleTime: 60_000,
  });

export const statusesQuery = queryOptions({
  queryKey: ["statuses"],
  queryFn: () => unwrap(api.GET("/statuses")),
  staleTime: 60_000,
});

export const fieldsQuery = queryOptions({
  queryKey: ["fields"],
  queryFn: () => unwrap(api.GET("/fields")),
  staleTime: 60_000,
});

export const labelsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["labels", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/labels", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

export function useMe() {
  return useQuery(meQuery);
}

/**
 * useProject loads a project; with an empty key (a dialog kept mounted before it has an issue,
 * say) it waits instead of requesting /projects/.
 */
export function useProject(key: string) {
  return useQuery({ ...projectQuery(key), enabled: key !== "" });
}

export function useUsers(q: string, project?: string) {
  return useQuery({
    queryKey: ["users", q, project ?? ""],
    queryFn: () => unwrap(api.GET("/users", { params: { query: { q: q || undefined, project } } })),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}

/** hasPermission checks a project permission from Project.myPermissions. */
export function hasPermission(project: { myPermissions: string[] } | undefined, perm: string) {
  return !!project?.myPermissions.includes(perm);
}
