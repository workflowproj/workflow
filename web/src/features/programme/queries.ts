import { keepPreviousData, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { useToast } from "@/components/ui";

export type ProgrammeOverview = Schemas["ProgrammeOverview"];
export type ProgrammeInitiative = Schemas["ProgrammeInitiative"];
export type ProgrammeEpic = Schemas["ProgrammeEpic"];
export type PlanningFlag = Schemas["PlanningFlag"];

/** The programme's roadmap. Lives under ["programme"] so live events refresh it. */
export const programmeOverviewQuery = (programmeKey: string, doneDays = 90) =>
  queryOptions({
    queryKey: ["programme", programmeKey, "overview", doneDays],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/programme", { params: { path: { projectKey: programmeKey }, query: { doneDays } } })),
    placeholderData: keepPreviousData,
  });

export const programmeTeamsQuery = (programmeKey: string) =>
  queryOptions({
    queryKey: ["programme", programmeKey, "teams"],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/programme/teams", { params: { path: { projectKey: programmeKey } } })),
  });

/** The programmes a team project belongs to (those the viewer can browse). */
export const projectProgrammesQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["programme", "of", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/programmes", { params: { path: { projectKey } } })),
    staleTime: 60_000,
  });

/** useTeamMutations adds and removes a programme's team projects. */
export function useTeamMutations(programmeKey: string) {
  const qc = useQueryClient();
  const toast = useToast();
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["programme"] });
    qc.invalidateQueries({ queryKey: ["timeline"] });
  };
  const add = useMutation({
    mutationFn: (projectKey: string) => unwrap(api.POST("/projects/{projectKey}/programme/teams", { params: { path: { projectKey: programmeKey } }, body: { projectKey } })),
    onSuccess: (teams, key) => {
      qc.setQueryData(programmeTeamsQuery(programmeKey).queryKey, teams);
      refresh();
      toast.success(`${key} added to the programme`);
    },
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: (teamKey: string) => unwrap(api.DELETE("/projects/{projectKey}/programme/teams/{teamKey}", { params: { path: { projectKey: programmeKey, teamKey } } })),
    onSuccess: (r, key) => {
      refresh();
      toast.success(r.unlinkedEpics ? `${key} removed; ${r.unlinkedEpics} epic${r.unlinkedEpics === 1 ? "" : "s"} unlinked from initiatives` : `${key} removed from the programme`);
    },
    onError: toast.error,
  });
  return { add, remove };
}

/** useSetInitiative moves an epic under an initiative (or out from under one with null). */
export function useSetInitiative() {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: ({ epicKey, initiativeKey }: { epicKey: string; initiativeKey: string | null }) =>
      unwrap(api.PATCH("/issues/{issueKey}", { params: { path: { issueKey: epicKey } }, body: { parentKey: initiativeKey } })),
    onSuccess: (_, { epicKey, initiativeKey }) => {
      qc.invalidateQueries({ queryKey: ["programme"] });
      qc.invalidateQueries({ queryKey: ["timeline"] });
      qc.invalidateQueries({ queryKey: ["issue", epicKey] });
      if (initiativeKey) qc.invalidateQueries({ queryKey: ["issue", initiativeKey] });
      toast.success(initiativeKey ? `${epicKey} now belongs to ${initiativeKey}` : `${epicKey} removed from its initiative`);
    },
    onError: toast.error,
  });
}
