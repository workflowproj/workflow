import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import type { IssueDetail } from "@/api/types";
import { useToast } from "@/components/ui";
import { afterTransition } from "@/features/issues/issue-api";

export type RiskRegister = Schemas["RiskRegister"];
export type RiskEntry = Schemas["RiskEntry"];
export type DecisionLog = Schemas["DecisionLog"];
export type DecisionEntry = Schemas["DecisionEntry"];

const path = (projectKey: string) => ({ params: { path: { projectKey } } });

/** The project's risk register: every risk, the totals of open ones and the matrix. */
export const riskRegisterQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["risks", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/risks", path(projectKey))),
    staleTime: 15_000,
  });

/** The project's decision log, with what supersedes what and the implementing work. */
export const decisionLogQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["decisions", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/decisions", path(projectKey))),
    staleTime: 15_000,
  });

/** useSupersede marks a decision as superseded by a newer one (link and transition in one step). */
export function useSupersede(issueKey: string, projectKey: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    scope: { id: `issue:${issueKey}` },
    mutationFn: ({ by, comment }: { by: string; comment?: string }) => {
      const version = qc.getQueryData<IssueDetail>(["issue", issueKey])?.version;
      return unwrap(api.POST("/issues/{issueKey}/supersede", { params: { path: { issueKey } }, body: { supersededByKey: by, version, comment: comment || undefined } }));
    },
    onSuccess: (issue, { by }) => {
      afterTransition(qc, issueKey, issue);
      qc.invalidateQueries({ queryKey: ["issue", by] });
      qc.invalidateQueries({ queryKey: ["decisions", projectKey] });
      toast.success(`${issueKey} is superseded by ${by}`);
    },
  });
}
