import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import { whereOf, type BreakdownBy } from "./model";

/*
 * Dashboard queries. Keys: ["dashboards"], ["dashboards", id]; gadget data lives under
 * ["metrics", kind, projects, jql, ...], sprint data under ["sprints", projectKey, ...] and
 * issue lists under ["search", jql, ...], so live events refresh them (lib/events.ts).
 * Gadgets also poll, since a dashboard can span projects nobody is streaming events for.
 */

const REFRESH = 60_000;

export const dashboardsQuery = queryOptions({
  queryKey: ["dashboards"],
  queryFn: () => unwrap(api.GET("/dashboards")),
  staleTime: 15_000,
});

export const dashboardQuery = (dashboardId: string) =>
  queryOptions({
    queryKey: ["dashboards", dashboardId],
    queryFn: () => unwrap(api.GET("/dashboards/{dashboardId}", { params: { path: { dashboardId } } })),
    staleTime: 15_000,
  });

/** A query gadget's scope: projects (none = all the viewer can browse) and JQL. */
export type MetricScope = { projectKeys?: string[]; jql?: string };

const scopeParams = (s: MetricScope) => ({ projects: s.projectKeys?.length ? s.projectKeys.join(",") : undefined, jql: whereOf(s.jql) || undefined });
const scopeKey = (s: MetricScope) => [s.projectKeys?.join(",") ?? "", whereOf(s.jql)];

const live = { refetchInterval: REFRESH, placeholderData: keepPreviousData, staleTime: 15_000 } as const;

export const summaryQuery = (s: MetricScope, days: number) =>
  queryOptions({
    queryKey: ["metrics", "summary", ...scopeKey(s), days],
    queryFn: () => unwrap(api.GET("/metrics/summary", { params: { query: { ...scopeParams(s), days } } })),
    ...live,
  });

export const flowQuery = (s: MetricScope, days: number, by: "category" | "status") =>
  queryOptions({
    queryKey: ["metrics", "flow", ...scopeKey(s), days, by],
    queryFn: () => unwrap(api.GET("/metrics/flow", { params: { query: { ...scopeParams(s), days, by } } })),
    ...live,
  });

export const createdResolvedQuery = (s: MetricScope, days: number, interval: "day" | "week") =>
  queryOptions({
    queryKey: ["metrics", "created-resolved", ...scopeKey(s), days, interval],
    queryFn: () => unwrap(api.GET("/metrics/created-resolved", { params: { query: { ...scopeParams(s), days, interval } } })),
    ...live,
  });

export const breakdownQuery = (s: MetricScope, by: BreakdownBy) =>
  queryOptions({
    queryKey: ["metrics", "breakdown", ...scopeKey(s), by],
    queryFn: () => unwrap(api.GET("/metrics/breakdown", { params: { query: { ...scopeParams(s), by } } })),
    ...live,
  });

export const cycleTimeQuery = (s: MetricScope, days: number) =>
  queryOptions({
    queryKey: ["metrics", "cycle-time", ...scopeKey(s), days],
    queryFn: () => unwrap(api.GET("/metrics/cycle-time", { params: { query: { ...scopeParams(s), days } } })),
    ...live,
  });

/** Velocity and sprint reports sit under ["sprints", projectKey] with the project's sprint list. */
export const velocityQuery = (projectKey: string, count: number) =>
  queryOptions({
    queryKey: ["sprints", projectKey, "velocity", count],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/velocity", { params: { path: { projectKey }, query: { count } } })),
    ...live,
  });

export const sprintReportQuery = (projectKey: string, sprintId: string) =>
  queryOptions({
    queryKey: ["sprints", projectKey, "report", sprintId],
    queryFn: () => unwrap(api.GET("/sprints/{sprintId}/report", { params: { path: { sprintId } } })),
    ...live,
  });

export const issueListQuery = (jql: string, limit: number) =>
  queryOptions({
    queryKey: ["search", jql, "gadget", limit],
    queryFn: () => unwrap(api.GET("/search", { params: { query: { jql, limit } } })),
    ...live,
    retry: false,
  });
