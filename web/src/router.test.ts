import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { makeRouter } from "./router";

describe("router", () => {
  it("exposes the route ids pages rely on", () => {
    const ids = Object.keys(makeRouter(new QueryClient()).routesById);
    for (const id of [
      "/login",
      "/app/projects/$projectKey",
      "/app/projects/$projectKey/board",
      "/app/projects/$projectKey/backlog",
      "/app/projects/$projectKey/issues",
      "/app/projects/$projectKey/settings",
      "/app/projects/$projectKey/settings/workflows/$workflowId",
      "/app/projects/$projectKey/settings/chat",
      "/app/issues",
      "/app/timeline",
      "/app/dashboards",
      "/app/dashboards/$dashboardId",
      "/app/projects/$projectKey/timeline",
      "/app/browse/$issueKey",
      "/app/admin/users",
      "/app/admin/connected-apps",
      "/oauth/consent",
      "/oauth/consent/$request",
    ]) {
      expect(ids).toContain(id);
    }
  });
});
