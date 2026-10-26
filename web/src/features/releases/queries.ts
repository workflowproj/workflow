import { queryOptions } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { jqlString } from "@/features/board/jql";

export type Release = Schemas["Release"];
export type ReleaseDetail = Schemas["ReleaseDetail"];
export type ReleaseReadiness = Schemas["ReleaseReadiness"];
export type ReleaseBurnup = Schemas["ReleaseBurnup"];

/*
 * Release queries live under ["releases", ...], so live events and release edits refresh them:
 * ["releases", projectKey] (the list), ["releases", "detail" | "readiness" | "burnup", id].
 */

export const releasesQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["releases", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/releases", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

const byId = (releaseId: string) => ({ params: { path: { releaseId } } });

export const releaseQuery = (releaseId: string) =>
  queryOptions({ queryKey: ["releases", "detail", releaseId], queryFn: () => unwrap(api.GET("/releases/{releaseId}", byId(releaseId))) });

export const readinessQuery = (releaseId: string) =>
  queryOptions({ queryKey: ["releases", "readiness", releaseId], queryFn: () => unwrap(api.GET("/releases/{releaseId}/readiness", byId(releaseId))) });

export const burnupQuery = (releaseId: string) =>
  queryOptions({ queryKey: ["releases", "burnup", releaseId], queryFn: () => unwrap(api.GET("/releases/{releaseId}/burnup", byId(releaseId))) });

export const releaseSettingsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["release-settings", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/release-settings", { params: { path: { projectKey } } })),
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

/** dateStatus says how a release stands against its date: "3 days left", "2 days late", "Due today". */
export function dateStatus(release: Pick<Release, "state" | "releaseDate">, today: string): { text: string; late: boolean } | null {
  if (release.state !== "unreleased" || !release.releaseDate) return null;
  const n = Math.round((Date.parse(release.releaseDate) - Date.parse(today)) / 86_400_000);
  if (n > 1) return { text: `${n} days left`, late: false };
  if (n === 1) return { text: "1 day left", late: false };
  if (n === 0) return { text: "Due today", late: false };
  return { text: `${-n} day${n === -1 ? "" : "s"} late`, late: true };
}

/** notesFileName names a release's notes for download: "release-notes-3.9.0.md". */
export function notesFileName(name: string) {
  const slug = name
    .trim()
    .replace(/[^\w.-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `release-notes-${slug || "release"}.md`;
}
