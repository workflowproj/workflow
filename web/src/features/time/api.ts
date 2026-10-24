import { useCallback } from "react";
import { keepPreviousData, queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { useToast } from "@/components/ui";
import { DEFAULT_UNITS, formatDuration, type TimeUnits } from "@/lib/duration";
import { invalidateIssueLists } from "@/features/issues/issue-api";

export type Worklog = Schemas["Worklog"];
export type WorklogInput = Schemas["WorklogInput"];
export type WorklogPatch = Schemas["WorklogPatch"];
export type Timesheet = Schemas["Timesheet"];
export type TimeTracking = Schemas["TimeTracking"];

/** The working day and week behind "1d" and "1w"; they only change with a server restart. */
export const timeSettingsQuery = queryOptions({
  queryKey: ["time-settings"],
  queryFn: () => unwrap(api.GET("/time-tracking/settings")),
  staleTime: Infinity,
});

export function useTimeUnits(): TimeUnits {
  const { data } = useQuery(timeSettingsQuery);
  return data ?? DEFAULT_UNITS;
}

/** useFormatDuration formats seconds with the server's working day and week. */
export function useFormatDuration() {
  const units = useTimeUnits();
  return useCallback((seconds: number | null | undefined) => formatDuration(seconds, units), [units]);
}

// Under ["issue", key] so live events and anything that refreshes the issue refresh it too.
export const worklogsQuery = (issueKey: string) =>
  queryOptions({
    queryKey: ["issue", issueKey, "worklogs"],
    queryFn: () => unwrap(api.GET("/issues/{issueKey}/worklogs", { params: { path: { issueKey } } })),
  });

export type TimesheetParams = { from: string; to: string; projects?: string; user?: string; tz?: string };

export const timesheetQuery = (p: TimesheetParams) =>
  queryOptions({
    queryKey: ["timesheet", p],
    queryFn: () => unwrap(api.GET("/worklogs", { params: { query: p } })),
    placeholderData: keepPreviousData,
  });

/** Everything that shows time tracking after a worklog changes. */
function useAfterWorklog(issueKey: string) {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["issue", issueKey] });
    qc.invalidateQueries({ queryKey: ["issue-history", issueKey] });
    qc.invalidateQueries({ queryKey: ["timesheet"] });
    qc.invalidateQueries({ queryKey: ["sprints"] });
    invalidateIssueLists(qc);
  };
}

type SaveVars = { id?: string; body: WorklogInput | WorklogPatch };

/** useSaveWorklog logs new work (no id) or changes a worklog. */
export function useSaveWorklog(issueKey: string) {
  const after = useAfterWorklog(issueKey);
  return useMutation({
    scope: { id: `issue:${issueKey}` },
    mutationFn: ({ id, body }: SaveVars) =>
      id
        ? unwrap(api.PATCH("/worklogs/{worklogId}", { params: { path: { worklogId: id } }, body: body as WorklogPatch }))
        : unwrap(api.POST("/issues/{issueKey}/worklogs", { params: { path: { issueKey } }, body: body as WorklogInput })),
    onSuccess: after,
  });
}

export function useDeleteWorklog(issueKey: string) {
  const after = useAfterWorklog(issueKey);
  const toast = useToast();
  return useMutation({
    scope: { id: `issue:${issueKey}` },
    mutationFn: (id: string) => unwrap(api.DELETE("/worklogs/{worklogId}", { params: { path: { worklogId: id } } })),
    onSuccess: after,
    onError: (err) => toast.error(err),
  });
}
