import { keepPreviousData, queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import type { IssueDetail } from "@/api/types";
import { useToast } from "@/components/ui";

export type IssuePatch = Omit<Schemas["IssuePatch"], "version">;
export type AvailableTransition = Schemas["AvailableTransition"];

const path = (issueKey: string) => ({ params: { path: { issueKey } } });

export const issueQuery = (key: string) =>
  queryOptions({
    queryKey: ["issue", key],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}", path(key))),
  });

// Under ["issue", key] so live events and edits that invalidate the issue refresh it too.
export const transitionsQuery = (key: string) =>
  queryOptions({
    queryKey: ["issue", key, "transitions"],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/transitions", path(key))),
    staleTime: 30_000,
  });

/**
 * Lead/cycle time and time in status. Under ["issue", key] so live events and transitions
 * refresh it; keyed by version too, so edits that touch it (a sprint change) do as well.
 */
export const metricsQuery = (key: string, version: number) =>
  queryOptions({
    queryKey: ["issue", key, "metrics", version],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/metrics", path(key))),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });

export const commentsQuery = (key: string) =>
  queryOptions({
    queryKey: ["issue-comments", key],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/comments", path(key))),
  });

export const historyQuery = (key: string) =>
  queryOptions({
    queryKey: ["issue-history", key],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/history", path(key))),
  });

export const sprintsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["sprints", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/sprints", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

export const releasesQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["releases", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/releases", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

export const repositoriesQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["project-repositories", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/repositories", { params: { path: { projectKey } } })),
    staleTime: 60_000,
  });

/** issueSearchQuery powers pickers (links, parent); results share the ["search"] prefix. */
export const issueSearchQuery = (jql: string, limit = 8) =>
  queryOptions({
    queryKey: ["search", jql, { limit, picker: true }],
    queryFn: () => unwrap(api.GET("/search", { params: { query: { jql, limit } } })),
    staleTime: 15_000,
  });

/** Lists that show issues and must refresh after an issue changes. */
export function invalidateIssueLists(qc: QueryClient) {
  for (const k of ["board", "backlog", "search"]) qc.invalidateQueries({ queryKey: [k] });
}

export function isConflict(err: unknown) {
  return err instanceof ApiError && err.status === 409 && err.code === "version_conflict";
}

const CONFLICT_MESSAGE = "Someone else changed this issue — reloaded";

type UpdateVars = { patch: IssuePatch; optimistic?: Partial<IssueDetail> };

/**
 * useUpdateIssue PATCHes only the changed fields plus the version the client last saw. Edits to
 * one issue run one after another (mutation scope) so each sends the version the previous one
 * produced. `optimistic` is merged into the cached issue straight away and rolled back on error;
 * a 409 version conflict reloads the issue.
 */
export function useUpdateIssue(issueKey: string) {
  const qc = useQueryClient();
  const toast = useToast();
  const key = ["issue", issueKey];
  return useMutation({
    scope: { id: `issue:${issueKey}` },
    mutationFn: async ({ patch }: UpdateVars) => {
      const version = qc.getQueryData<IssueDetail>(key)?.version;
      const res = await unwrap(api.PATCH("/issues/{issueKey}", { ...path(issueKey), body: { ...patch, version } }));
      // Keep the optimistic view until the server copy lands, then replace it wholesale.
      qc.setQueryData(key, res);
      return res;
    },
    onMutate: async ({ optimistic }: UpdateVars) => {
      if (!optimistic) return { previous: undefined };
      await qc.cancelQueries({ queryKey: key, exact: true });
      const previous = qc.getQueryData<IssueDetail>(key);
      if (previous) qc.setQueryData<IssueDetail>(key, { ...previous, ...optimistic });
      return { previous };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(key, ctx.previous);
      qc.invalidateQueries({ queryKey: key });
      if (isConflict(err)) toast.info(CONFLICT_MESSAGE);
      else toast.error(err);
    },
    onSuccess: (issue) => {
      invalidateIssueLists(qc);
      if (issue.parent) qc.invalidateQueries({ queryKey: ["issue", issue.parent.key], exact: true });
      qc.invalidateQueries({ queryKey: ["issue-history", issueKey] });
    },
  });
}

/** afterTransition refreshes everything a status change can affect. */
export function afterTransition(qc: QueryClient, issueKey: string, issue?: IssueDetail) {
  if (issue) qc.setQueryData(["issue", issueKey], issue);
  qc.invalidateQueries({ queryKey: ["issue", issueKey] });
  qc.invalidateQueries({ queryKey: ["issue-history", issueKey] });
  qc.invalidateQueries({ queryKey: ["issue-comments", issueKey] });
  if (issue?.parent) qc.invalidateQueries({ queryKey: ["issue", issue.parent.key], exact: true });
  invalidateIssueLists(qc);
}

/** useRunTransition runs a transition without a screen, sending the version for optimistic locking. */
export function useRunTransition(issueKey: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    scope: { id: `issue:${issueKey}` },
    mutationFn: (t: AvailableTransition) => {
      const version = qc.getQueryData<IssueDetail>(["issue", issueKey])?.version;
      return unwrap(api.POST("/issues/{issueKey}/transitions", { ...path(issueKey), body: { transitionId: t.id, version } }));
    },
    onSuccess: (issue) => afterTransition(qc, issueKey, issue),
    onError: (err) => {
      if (isConflict(err)) {
        qc.invalidateQueries({ queryKey: ["issue", issueKey] });
        toast.info(CONFLICT_MESSAGE);
      }
    },
  });
}
