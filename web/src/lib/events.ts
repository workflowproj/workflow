import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// Named events the server streams (domain events plus development activity).
const EVENT_TYPES = [
  "issue.created", "issue.updated", "issue.transitioned", "issue.deleted", "comment.created",
  "board.updated", "project.updated", "sprint.started", "sprint.completed", "release.released",
  "branch.created", "commit.pushed", "pr.opened", "pr.merged", "pr.closed", "pipeline.completed",
];

export type LiveEvent = { type: string; projectId: string; issueId?: string; issueKey?: string; actorId?: string };

/**
 * useProjectEvents subscribes to the project's server-sent events and invalidates cached
 * queries so boards, backlogs, searches and open issues refresh live. EventSource reconnects
 * on its own (the server sends `retry: 3000`).
 */
export function useProjectEvents(projectKey: string | undefined, onEvent?: (e: LiveEvent) => void) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!projectKey) return;
    const es = new EventSource(`/api/v1/events?project=${encodeURIComponent(projectKey)}`);
    let timer: ReturnType<typeof setTimeout> | undefined;
    const pending = new Set<string>();
    // Coalesce bursts (a drag can emit several events) into one refetch.
    const flush = () => {
      for (const k of ["board", "backlog", "search", "sprints", "releases", "timeline"]) qc.invalidateQueries({ queryKey: [k] });
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
      timer = setTimeout(flush, 150);
      onEvent?.(ev);
    };
    for (const t of EVENT_TYPES) es.addEventListener(t, handle);
    return () => {
      clearTimeout(timer);
      es.close();
    };
  }, [projectKey, qc, onEvent]);
}
