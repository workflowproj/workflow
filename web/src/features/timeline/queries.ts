import { keepPreviousData, queryOptions, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import type { Issue } from "@/api/types";
import { useToast } from "@/components/ui";
import { epicProgress, withDates, type TimelineData, type TimelineLink } from "./model";

/** projects is a comma list ("" = every project the viewer can browse). */
export const timelineQuery = (projects: string, jql: string, doneDays: number) =>
  queryOptions({
    queryKey: ["timeline", projects, jql, doneDays],
    queryFn: () => unwrap(api.GET("/timeline", { params: { query: { projects: projects || undefined, jql: jql || undefined, doneDays } } })),
    placeholderData: keepPreviousData,
  });

/**
 * useEpicProgress counts done children per epic with a search, so the fill doesn't depend on
 * the timeline's filters or on whether done issues are shown. Lives under ["timeline"] so
 * live events refresh it too.
 */
export function useEpicProgress(epicKeys: string[]) {
  const keys = [...epicKeys].sort();
  return useQuery({
    queryKey: ["timeline", "progress", keys.join(",")],
    enabled: keys.length > 0,
    staleTime: 15_000,
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const jql = `parent in (${keys.join(", ")})`;
      const children: Issue[] = [];
      for (let offset = 0; offset < 2000; offset += 500) {
        const page = await unwrap(api.GET("/search", { params: { query: { jql, limit: 500, offset } } }));
        children.push(...page.issues);
        if (children.length >= page.total || page.issues.length === 0) break;
      }
      return epicProgress(children);
    },
  });
}

/**
 * useTimelineEdits changes dates and dependencies with optimistic updates of the timeline
 * cached under `queryKey`. A 409 (someone else changed the issue) reloads; other errors
 * roll back and explain.
 */
export function useTimelineEdits(queryKey: readonly unknown[]) {
  const qc = useQueryClient();
  const toast = useToast();
  const current = () => qc.getQueryData<TimelineData>(queryKey);
  const set = (fn: (d: TimelineData) => TimelineData) => qc.setQueryData<TimelineData>(queryKey, (d) => (d ? fn(d) : d));
  const settle = () => qc.invalidateQueries({ queryKey: ["timeline"] });

  const schedule = async (issue: Issue, startDate: string | null, dueDate: string | null) => {
    const before = current()?.issues.find((i) => i.key === issue.key) ?? issue;
    qc.cancelQueries({ queryKey: ["timeline"] });
    set((d) => withDates(d, issue.key, startDate, dueDate));
    try {
      const saved = await unwrap(
        api.PATCH("/issues/{issueKey}", { params: { path: { issueKey: issue.key } }, body: { startDate, dueDate, version: before.version } }),
      );
      set((d) => withDates(d, issue.key, saved.startDate ?? null, saved.dueDate ?? null, saved.version));
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) toast.info(`${issue.key} was changed by someone else, so the timeline has been reloaded. Try again.`);
      else {
        set((d) => withDates(d, issue.key, before.startDate ?? null, before.dueDate ?? null, before.version));
        toast.error(err);
      }
    } finally {
      settle();
    }
  };

  const link = async (sourceKey: string, targetKey: string) => {
    const temp: TimelineLink = { id: `pending-${sourceKey}-${targetKey}`, sourceKey, targetKey, type: "blocks" };
    set((d) => ({ ...d, links: [...d.links, temp] }));
    try {
      await unwrap(api.POST("/issues/{issueKey}/links", { params: { path: { issueKey: sourceKey } }, body: { type: "blocks", targetKey } }));
      toast.success(`${sourceKey} now blocks ${targetKey}`);
    } catch (err) {
      set((d) => ({ ...d, links: d.links.filter((l) => l.id !== temp.id) }));
      toast.error(err);
    } finally {
      settle();
      qc.invalidateQueries({ queryKey: ["issue", sourceKey] });
      qc.invalidateQueries({ queryKey: ["issue", targetKey] });
    }
  };

  const unlink = async (l: TimelineLink) => {
    set((d) => ({ ...d, links: d.links.filter((x) => x.id !== l.id) }));
    try {
      await unwrap(api.DELETE("/issue-links/{linkId}", { params: { path: { linkId: l.id } } }));
      toast.success(`${l.sourceKey} no longer blocks ${l.targetKey}`);
    } catch (err) {
      set((d) => ({ ...d, links: [...d.links, l] }));
      toast.error(err);
    } finally {
      settle();
      qc.invalidateQueries({ queryKey: ["issue", l.sourceKey] });
      qc.invalidateQueries({ queryKey: ["issue", l.targetKey] });
    }
  };

  return { schedule, link, unlink };
}
