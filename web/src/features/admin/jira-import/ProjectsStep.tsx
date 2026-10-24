import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, FolderKanban, Search } from "lucide-react";
import { ApiError } from "@/api/client";
import { Badge, Button, Checkbox, EmptyState, Input, PageSpinner, Select, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Card } from "@/components/ui";
import { jiraProjectsQuery, useImportActions } from "./api";
import { keyError, TEMPLATES, toggleProject, updateTarget, type JiraConfig, type JiraImport } from "./model";

/** ProjectsStep chooses the Jira projects to import and what each becomes here. */
export function ProjectsStep({ imp, onNext }: { imp: JiraImport; onNext: () => void }) {
  const projects = useQuery({ ...jiraProjectsQuery(imp.id), enabled: imp.hasCredentials });
  const actions = useImportActions(imp.id);
  const [cfg, setCfg] = useState<JiraConfig>(imp.config);
  const [q, setQ] = useState("");
  const list = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return (projects.data ?? []).filter((p) => !needle || p.key.toLowerCase().includes(needle) || p.name.toLowerCase().includes(needle));
  }, [projects.data, q]);
  const selected = new Set(cfg.projects.map((t) => t.jiraId));
  // Projects an earlier import of this site created keep their key, name and template.
  const existing = new Map<string, string>([
    ...(projects.data ?? []).filter((p) => p.importedAs).map((p) => [p.id, p.importedAs!] as [string, string]),
    ...imp.targets.filter((t) => t.existingKey).map((t) => [t.jiraId, t.existingKey!] as [string, string]),
  ]);
  const invalid = cfg.projects.some((t) => !existing.has(t.jiraId) && keyError(t.key));
  const save = async () => {
    await actions.saveConfig.mutateAsync(cfg);
    await actions.scan.mutateAsync();
    onNext();
  };
  const error = actions.saveConfig.error ?? actions.scan.error;
  const fieldErrors = error instanceof ApiError ? error.fieldErrors() : {};

  if (!imp.hasCredentials) {
    return <ErrorBox error={new Error("The Jira credentials were deleted. Enter a token on the Connect step to list projects.")} />;
  }
  return (
    <div className="flex flex-col gap-4">
      <Card
        title="Jira projects"
        actions={
          <div className="relative w-56">
            <Search className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" aria-hidden />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter projects" aria-label="Filter projects" className="pl-8" />
          </div>
        }
      >
        {projects.error && <ErrorBox error={projects.error} className="m-3" />}
        {projects.isLoading ? (
          <PageSpinner label="Loading projects from Jira" />
        ) : list.length === 0 ? (
          <EmptyState icon={<FolderKanban className="size-8" />} title={q ? "No matching projects" : "No projects"} body="The account can only list projects it can browse." />
        ) : (
          <ul className="max-h-80 divide-y divide-line overflow-y-auto">
            {list.map((p) => (
              <li key={p.id} className="flex items-center gap-3 px-4 py-2">
                <Checkbox
                  checked={selected.has(p.id)}
                  onChange={() => setCfg(toggleProject(cfg, p))}
                  aria-label={`Import ${p.key}`}
                  label={
                    <span className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-xs text-muted">{p.key}</span>
                      <span className="font-medium">{p.name}</span>
                    </span>
                  }
                />
                <span className="ml-auto flex shrink-0 items-center gap-2 text-xs text-muted">
                  {p.teamManaged && <Badge>Team-managed</Badge>}
                  {p.type && <span className="hidden sm:inline">{p.type.replace("_", " ")}</span>}
                  {p.importedAs && <Badge tone="success">Imported as {p.importedAs}</Badge>}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Card>

      {cfg.projects.length > 0 && (
        <Card title={`Targets (${cfg.projects.length})`}>
          <div className="overflow-x-auto">
            <table className={table}>
              <thead>
                <tr>
                  <th>Jira</th>
                  <th className="w-32">Key here</th>
                  <th>Name</th>
                  <th className="w-52">Template</th>
                  <th className="w-48">Access</th>
                </tr>
              </thead>
              <tbody>
                {cfg.projects.map((t, i) => {
                  const fixed = existing.get(t.jiraId);
                  const err = fixed ? undefined : keyError(t.key) ?? fieldErrors[`projects[${i}].key`];
                  return (
                    <tr key={t.jiraId}>
                      <td>
                        <span className="font-mono text-xs text-muted">{t.jiraKey}</span>
                        <span className="block truncate text-[13px]">{t.jiraName}</span>
                      </td>
                      <td>
                        {fixed ? (
                          <span className="flex flex-col">
                            <span className="font-mono text-sm">{fixed}</span>
                            <span className="text-[11px] text-muted">updated in place</span>
                          </span>
                        ) : (
                          <Input
                            aria-label={`Key for ${t.jiraKey}`}
                            value={t.key}
                            maxLength={10}
                            invalid={!!err}
                            title={err}
                            className="font-mono uppercase"
                            onChange={(e) => setCfg(updateTarget(cfg, t.jiraId, { key: e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, "") }))}
                          />
                        )}
                      </td>
                      <td>
                        <Input aria-label={`Name for ${t.jiraKey}`} value={t.name} disabled={!!fixed} onChange={(e) => setCfg(updateTarget(cfg, t.jiraId, { name: e.target.value }))} />
                      </td>
                      <td>
                        <Select aria-label={`Template for ${t.jiraKey}`} value={t.template} disabled={!!fixed} onChange={(e) => setCfg(updateTarget(cfg, t.jiraId, { template: e.target.value }))}>
                          {TEMPLATES.map((tp) => (
                            <option key={tp.id} value={tp.id}>
                              {tp.label}
                            </option>
                          ))}
                        </Select>
                      </td>
                      <td>
                        <Select aria-label={`Access for ${t.jiraKey}`} value={t.open ? "open" : "private"} disabled={!!fixed} onChange={(e) => setCfg(updateTarget(cfg, t.jiraId, { open: e.target.value === "open" }))}>
                          <option value="open">Open</option>
                          <option value="private">Private</option>
                        </Select>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <p className="border-t border-line px-4 py-2.5 text-xs text-muted">
            Issues keep their numbers: with the same key, PLAT-123 stays PLAT-123. With another key the old one keeps resolving. Jira permissions are not imported; add members to private projects
            afterwards.
          </p>
        </Card>
      )}
      {error && <ErrorBox error={error} />}
      <div className="flex justify-end">
        <Button variant="primary" icon={<ArrowRight className="size-4" />} disabled={cfg.projects.length === 0 || invalid} loading={actions.saveConfig.isPending || actions.scan.isPending} onClick={() => save().catch(() => undefined)}>
          Save and analyse
        </Button>
      </div>
    </div>
  );
}
