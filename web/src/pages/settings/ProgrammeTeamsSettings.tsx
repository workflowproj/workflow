import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { EyeOff, Network, Plus, Trash2 } from "lucide-react";
import { projectsQuery, useProject } from "@/api/queries";
import { Button, EmptyState, IconButton, PageHeader, PageSpinner, Select, useConfirm } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ProjectAvatar } from "@/layout/AppShell";
import { SettingsSection } from "@/features/settings/common";
import { programmeTeamsQuery, useTeamMutations } from "@/features/programme/queries";
import { isProgramme } from "@/lib/project";

const route = getRouteApi("/app/projects/$projectKey/settings/teams");

/**
 * ProgrammeTeamsSettings chooses the team projects of a programme: their epics can then belong to
 * the programme's initiatives. Adding a team gives nobody access to it.
 */
export function ProgrammeTeamsSettings() {
  const { projectKey } = route.useParams();
  const { data: project } = useProject(projectKey);
  const { data, isLoading, error } = useQuery({ ...programmeTeamsQuery(projectKey), enabled: isProgramme(project) });
  const { data: projects = [] } = useQuery(projectsQuery);
  const { add, remove } = useTeamMutations(projectKey);
  const confirm = useConfirm();
  const [picked, setPicked] = useState("");
  if (project && !isProgramme(project)) {
    return <EmptyState title="Only programmes have teams" body="Create a project from the Programme template to plan initiatives across team projects." />;
  }
  const teams = data?.teams ?? [];
  const taken = new Set([projectKey, ...teams.map((t) => t.key)]);
  const candidates = projects.filter((p) => !taken.has(p.key) && p.template !== "service" && !isProgramme(p));
  const choice = candidates.some((p) => p.key === picked) ? picked : "";
  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <PageHeader
        title="Teams"
        subtitle="The team projects that work toward this programme's initiatives. Their epics can belong to an initiative; their stories, boards and sprints stay in the team."
      />
      {error && <ErrorBox error={error} />}
      {isLoading || !data ? (
        <PageSpinner />
      ) : (
        <SettingsSection
          title="Team projects"
          description="Adding a team gives nobody access to it: people see the teams, epics and work they can already browse."
        >
          <form
            className="flex flex-wrap items-end gap-2 border-b border-line p-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (choice) add.mutate(choice, { onSuccess: () => setPicked("") });
            }}
          >
            <label className="flex min-w-0 flex-1 flex-col gap-1 text-[13px] font-medium">
              Add a team project
              <Select value={choice} onChange={(e) => setPicked(e.target.value)} disabled={candidates.length === 0}>
                <option value="">{candidates.length ? "Choose a project…" : "No other projects to add"}</option>
                {candidates.map((p) => (
                  <option key={p.key} value={p.key}>
                    {p.name} ({p.key})
                  </option>
                ))}
              </Select>
            </label>
            <Button type="submit" variant="primary" icon={<Plus className="size-4" aria-hidden />} disabled={!choice} loading={add.isPending}>
              Add team
            </Button>
          </form>
          {teams.length === 0 && data.hiddenCount === 0 ? (
            <EmptyState icon={<Network className="size-8" />} title="No teams yet" body="Add the projects whose epics deliver this programme's initiatives." />
          ) : (
            <ul className="divide-y divide-line" aria-label="Team projects">
              {teams.map((t) => (
                <li key={t.key} className="flex items-center gap-3 px-4 py-2.5">
                  <ProjectAvatar projectKey={t.key} />
                  <Link to="/projects/$projectKey" params={{ projectKey: t.key }} className="min-w-0 flex-1 truncate text-sm font-medium hover:underline">
                    {t.name} <span className="font-mono text-xs font-normal text-muted">{t.key}</span>
                  </Link>
                  <IconButton
                    label={`Remove ${t.key} from the programme`}
                    disabled={remove.isPending}
                    onClick={async () => {
                      if (
                        await confirm({
                          title: `Remove ${t.name}?`,
                          body: `${t.key}'s epics that belong to this programme's initiatives are unlinked from them. The epics and their work stay in ${t.key}.`,
                          confirmLabel: "Remove team",
                          danger: true,
                        })
                      )
                        remove.mutate(t.key);
                    }}
                  >
                    <Trash2 className="size-4" />
                  </IconButton>
                </li>
              ))}
              {data.hiddenCount > 0 && (
                <li className="flex items-center gap-3 px-4 py-2.5 text-[13px] text-muted">
                  <EyeOff className="size-4 shrink-0" aria-hidden />
                  {data.hiddenCount === 1 ? "1 more team you can't browse." : `${data.hiddenCount} more teams you can't browse.`}
                </li>
              )}
            </ul>
          )}
        </SettingsSection>
      )}
    </div>
  );
}
