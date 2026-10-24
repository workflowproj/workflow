import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { busy, type JiraConfig, type JiraImport } from "./model";

export const jiraImportsQuery = queryOptions({
  queryKey: ["jira-imports"],
  queryFn: () => unwrap(api.GET("/admin/jira-imports")),
});

/** jiraImportQuery polls while a scan or import runs. */
export const jiraImportQuery = (id: string) =>
  queryOptions({
    queryKey: ["jira-imports", id],
    queryFn: () => unwrap(api.GET("/admin/jira-imports/{importId}", { params: { path: { importId: id } } })),
    refetchInterval: (q) => (q.state.data && busy(q.state.data) ? 1500 : false),
  });

export const jiraProjectsQuery = (id: string) =>
  queryOptions({
    queryKey: ["jira-imports", id, "jira-projects"],
    queryFn: () => unwrap(api.GET("/admin/jira-imports/{importId}/jira-projects", { params: { path: { importId: id } } })),
    staleTime: 60_000,
  });

const path = (id: string) => ({ params: { path: { importId: id } } });

/** useImportActions are the wizard's mutations; each stores the returned import in the cache. */
export function useImportActions(id: string) {
  const qc = useQueryClient();
  const store = (imp: JiraImport) => {
    qc.setQueryData(["jira-imports", id], imp);
    qc.invalidateQueries({ queryKey: ["jira-imports"], exact: true });
  };
  return {
    saveConfig: useMutation({
      mutationFn: (config: JiraConfig) => unwrap(api.PATCH("/admin/jira-imports/{importId}", { ...path(id), body: { config } })),
      onSuccess: store,
    }),
    updateToken: useMutation({
      mutationFn: (body: { email?: string; token: string }) => unwrap(api.PATCH("/admin/jira-imports/{importId}", { ...path(id), body })),
      onSuccess: store,
    }),
    scan: useMutation({ mutationFn: () => unwrap(api.POST("/admin/jira-imports/{importId}/scan", path(id))), onSuccess: store }),
    dryRun: useMutation({ mutationFn: () => unwrap(api.POST("/admin/jira-imports/{importId}/dry-run", path(id))) }),
    run: useMutation({ mutationFn: () => unwrap(api.POST("/admin/jira-imports/{importId}/run", path(id))), onSuccess: store }),
    cancel: useMutation({ mutationFn: () => unwrap(api.POST("/admin/jira-imports/{importId}/cancel", path(id))), onSuccess: store }),
    deleteCredentials: useMutation({ mutationFn: () => unwrap(api.DELETE("/admin/jira-imports/{importId}/credentials", path(id))), onSuccess: store }),
    remove: useMutation({
      mutationFn: () => unwrap(api.DELETE("/admin/jira-imports/{importId}", path(id))),
      onSuccess: () => qc.invalidateQueries({ queryKey: ["jira-imports"] }),
    }),
  };
}

export type ConnectionInput = Schemas["JiraConnectionInput"];
