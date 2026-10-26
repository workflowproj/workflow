import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// Named events the server streams (domain events plus development activity).
const EVENT_TYPES = [
  "issue.created", "issue.updated", "issue.transitioned", "issue.deleted", "comment.created",
  "board.updated", "project.updated", "sprint.started", "sprint.completed", "release.released",
  "branch.created", "commit.pushed", "pr.opened", "pr.merged", "pr.closed", "pipeline.completed", "deployment.completed",
];

export type LiveEvent = { type: string; projectId: string; issueId?: string; issueKey?: string; actorId?: string };

/** Which projects a stream follows: one key, several, or every project the viewer can browse. */
export type LiveScope = string | string[] | "all" | undefined;

/** eventsURL is the stream for a scope (undefined when there is nothing to follow). */
export function eventsURL(scope: LiveScope): string | undefined {
  if (!scope || (Array.isArray(scope) && scope.length === 0)) return undefined;
  if (scope === "all") return "/api/v1/events?projects=*";
  if (Array.isArray(scope)) return `/api/v1/events?projects=${[...new Set(scope)].sort().map(encodeURIComponent).join(",")}`;
  return `/api/v1/events?project=${encodeURIComponent(scope)}`;
}

/**
 * useLiveEvents subscribes to server-sent events for the scope and invalidates cached queries so
 * boards, backlogs, searches, timelines, dashboards and open issues refresh live. Bursts are
 * coalesced into one refetch after `debounceMs`. EventSource reconnects on its own (the server
 * sends `retry: 3000`).
 */
export function useLiveEvents(scope: LiveScope, { onEvent, debounceMs = 150 }: { onEvent?: (e: LiveEvent) => void; debounceMs?: number } = {}) {
  const qc = useQueryClient();
  const url = eventsURL(scope);
  useEffect(() => {
    if (!url) return;
    const es = new EventSource(url);
    let timer: ReturnType<typeof setTimeout> | undefined;
    const pending = new Set<string>();
    const flush = () => {
      for (const k of ["board", "backlog", "search", "sprints", "releases", "timeline", "metrics", "risks", "decisions"]) qc.invalidateQueries({ queryKey: [k] });
      for (const key of pending) {
        qc.invalidateQueries({ queryKey: ["issue", key] });
        qc.invalidateQueries({ queryKey: ["issue-comments", key] });
        qc.invalidateQueries({ queryKey: ["issue-history", key] });
      }
      pending.clear();
    };
    const handle = (msg: MessageEvent) => {
      let ev: LiveEvent;
      try {
        ev = JSON.parse(msg.data);
      } catch {
        return;
      }
      if (ev.issueKey) pending.add(ev.issueKey);
      clearTimeout(timer);
      timer = setTimeout(flush, debounceMs);
      onEvent?.(ev);
    };
    for (const t of EVENT_TYPES) es.addEventListener(t, handle);
    return () => {
      clearTimeout(timer);
      es.close();
    };
  }, [url, qc, onEvent, debounceMs]);
}

/** useProjectEvents follows one project (boards, backlogs, issue views). */
export function useProjectEvents(projectKey: string | undefined, onEvent?: (e: LiveEvent) => void) {
  useLiveEvents(projectKey, { onEvent });
}
