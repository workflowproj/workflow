import { queryOptions } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { jqlString } from "@/features/board/jql";

export type Release = Schemas["Release"];

export const releasesQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["releases", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/releases", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

/** The JQL for a release's issues, optionally only the unfinished ones. */
export function releaseJql(projectKey: string, name: string, openOnly = false) {
  return `project = ${projectKey} AND fixVersion = ${jqlString(name)}${openOnly ? " AND statusCategory != Done" : ""} ORDER BY rank`;
}

export const releaseIssuesQuery = (projectKey: string, name: string) => {
  const jql = releaseJql(projectKey, name);
  return queryOptions({
    queryKey: ["search", jql, "release"],
    queryFn: () => unwrap(api.GET("/search", { params: { query: { jql, limit: 200 } } })),
  });
};
