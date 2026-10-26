import { queryOptions } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

/**
 * Service desk queries. Keys: ["service-desk", projectKey], ["request-types", projectKey],
 * ["customers", projectKey, q], ["queues", projectKey], ["queue", queueId, ...], ["slas", projectKey],
 * ["issue", key, "request"] (under the issue, so live events refresh it), and for the portal
 * ["portal", ...].
 */

export type ServiceDesk = Schemas["ServiceDesk"];
export type RequestType = Schemas["RequestType"];
export type RequestTypeInput = Schemas["RequestTypeInput"];
export type RequestFormField = Schemas["RequestFormField"];
export type Queue = Schemas["Queue"];
export type QueueItem = Schemas["QueueItem"];
export type Sla = Schemas["Sla"];
export type SlaConfig = Schemas["SlaConfig"];
export type SlaCalendar = Schemas["SlaCalendar"];
export type SlaGoal = Schemas["SlaGoal"];
export type ServiceRequest = Schemas["ServiceRequest"];
export type Approval = Schemas["Approval"];
export type ServiceCustomer = Schemas["ServiceCustomer"];
export type PortalDesk = Schemas["PortalDesk"];
export type PortalDeskDetail = Schemas["PortalDeskDetail"];
export type PortalRequestType = Schemas["PortalRequestType"];
export type PortalRequest = Schemas["PortalRequest"];
export type PortalRequestDetail = Schemas["PortalRequestDetail"];
export type PortalFormField = Schemas["PortalFormField"];

const project = (projectKey: string) => ({ params: { path: { projectKey } } });

export const serviceDeskQuery = (projectKey: string) =>
  queryOptions({ queryKey: ["service-desk", projectKey], queryFn: () => unwrap(api.GET("/projects/{projectKey}/service-desk", project(projectKey))), staleTime: 30_000 });

export const requestTypesQuery = (projectKey: string) =>
  queryOptions({ queryKey: ["request-types", projectKey], queryFn: () => unwrap(api.GET("/projects/{projectKey}/request-types", project(projectKey))), staleTime: 30_000 });

export const customersQuery = (projectKey: string, q: string) =>
  queryOptions({
    queryKey: ["customers", projectKey, q],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/customers", { params: { path: { projectKey }, query: { q: q || undefined, limit: 200 } } })),
  });

export const queuesQuery = (projectKey: string) =>
  queryOptions({ queryKey: ["queues", projectKey], queryFn: () => unwrap(api.GET("/projects/{projectKey}/queues", project(projectKey))), refetchInterval: 60_000 });

export const queueIssuesQuery = (queueId: string, offset = 0) =>
  queryOptions({
    queryKey: ["queue", queueId, offset],
    queryFn: () => unwrap(api.GET("/queues/{queueId}/issues", { params: { path: { queueId }, query: { limit: 50, offset } } })),
    refetchInterval: 60_000,
  });

export const slaConfigQuery = (projectKey: string) =>
  queryOptions({ queryKey: ["slas", projectKey], queryFn: () => unwrap(api.GET("/projects/{projectKey}/slas", project(projectKey))) });

export const serviceRequestQuery = (issueKey: string) =>
  queryOptions({
    queryKey: ["issue", issueKey, "request"],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/request", { params: { path: { issueKey } } })),
    // SLA clocks keep counting between changes.
    refetchInterval: 60_000,
  });

// ---------------------------------------------------------------------------
// The portal

export const portalDesksQuery = queryOptions({ queryKey: ["portal", "desks"], queryFn: () => unwrap(api.GET("/portal/projects")) });

export const portalDeskQuery = (projectKey: string) =>
  queryOptions({ queryKey: ["portal", "desk", projectKey], queryFn: () => unwrap(api.GET("/portal/projects/{projectKey}", project(projectKey))) });

export const portalReleaseNotesQuery = (projectKey: string) =>
  queryOptions({ queryKey: ["portal", "whats-new", projectKey], queryFn: () => unwrap(api.GET("/portal/projects/{projectKey}/whats-new", project(projectKey))) });

export type RequestFilter = "open" | "closed" | "all";

export const portalRequestsQuery = (status: RequestFilter) =>
  queryOptions({ queryKey: ["portal", "requests", status], queryFn: () => unwrap(api.GET("/portal/requests", { params: { query: { status } } })) });

export const portalRequestQuery = (issueKey: string) =>
  queryOptions({
    queryKey: ["portal", "request", issueKey],
    queryFn: () => unwrap(api.GET("/portal/requests/{issueKey}", { params: { path: { issueKey } } })),
    refetchInterval: 30_000,
  });

export const portalApprovalsQuery = queryOptions({
  queryKey: ["portal", "approvals"],
  queryFn: () => unwrap(api.GET("/portal/approvals")),
  refetchInterval: 60_000,
});
