import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { makeRouter } from "./router";

describe("router", () => {
  it("exposes the route ids pages rely on", () => {
    const ids = Object.keys(makeRouter(new QueryClient()).routesById);
    for (const id of [
      "/login",
      "/forgot-password",
      "/invite",
      "/reset-password",
      "/app/projects/$projectKey",
      "/app/projects/$projectKey/board",
      "/app/projects/$projectKey/backlog",
      "/app/projects/$projectKey/issues",
      "/app/projects/$projectKey/risks",
      "/app/projects/$projectKey/decisions",
      "/app/projects/$projectKey/settings",
      "/app/projects/$projectKey/settings/workflows/$workflowId",
      "/app/projects/$projectKey/settings/chat",
      "/app/issues",
      "/app/timeline",
      "/app/dashboards",
      "/app/dashboards/$dashboardId",
      "/app/timesheet",
      "/app/projects/$projectKey/timeline",
      "/app/browse/$issueKey",
      "/app/admin/users",
      "/app/admin/connected-apps",
      "/app/admin/email",
      "/app/admin/import",
      "/app/admin/import/$importId",
      "/oauth/consent",
      "/oauth/consent/$request",
      "/portal",
      "/portal/",
      "/portal/requests",
      "/portal/requests/$issueKey",
      "/portal/approvals",
      "/portal/profile",
      "/portal/$projectKey",
      "/portal/$projectKey/new/$requestTypeId",
      "/portal/signup/$projectKey",
      "/app/projects/$projectKey/queues",
      "/app/projects/$projectKey/customers",
      "/app/projects/$projectKey/settings/portal",
      "/app/projects/$projectKey/settings/request-types",
      "/app/projects/$projectKey/settings/queues",
      "/app/projects/$projectKey/settings/slas",
      "/app/projects/$projectKey/releases",
      "/app/projects/$projectKey/releases/$releaseId",
      "/app/projects/$projectKey/settings/releases",
      "/portal/$projectKey/whats-new",
    ]) {
      expect(ids).toContain(id);
    }
  });
});
