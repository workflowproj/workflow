import { queryOptions } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

/** A saved timeline: a named group of projects, as the viewer sees it. */
export type TimelineView = Schemas["TimelineView"];

export const timelineViewsQuery = queryOptions({
  queryKey: ["timeline-views"],
  queryFn: () => unwrap(api.GET("/timeline-views")),
  staleTime: 30_000,
});

const LAST_VIEW_KEY = "wfm.timeline.view";

/** readLastView is the saved timeline last opened in this browser, if storage allows. */
export function readLastView(): string | undefined {
  try {
    return localStorage.getItem(LAST_VIEW_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

export function rememberView(id: string) {
  try {
    localStorage.setItem(LAST_VIEW_KEY, id);
  } catch {
    // Then /timeline opens on your first timeline instead.
  }
}

/**
 * pickView chooses the saved timeline /timeline opens on: the one last opened, if it's still
 * there; else your first own timeline; else the first shared one. None means there are no
 * timelines to show yet.
 */
export function pickView(views: TimelineView[], last?: string): TimelineView | undefined {
  return views.find((v) => v.id === last) ?? views.find((v) => v.mine) ?? views[0];
}

/** canChangeView: the owner changes a saved timeline; an org admin can also change a shared one. */
export function canChangeView(view: TimelineView, orgAdmin: boolean) {
  return view.mine || (orgAdmin && view.shared);
}

/** splitKeys reads a comma list of project keys, ignoring blanks. */
export function splitKeys(projects?: string) {
  return (projects ?? "")
    .split(",")
    .map((k) => k.trim())
    .filter(Boolean);
}
