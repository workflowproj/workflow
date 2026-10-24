import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronRight, Cloud, Plus, Server } from "lucide-react";
import { Badge, Button, Card, PageHeader, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { When } from "@/features/settings/common";
import { jiraImportsQuery } from "@/features/admin/jira-import/api";
import { ConnectForm } from "@/features/admin/jira-import/ConnectForm";
import { siteName, STATUS_LABEL, STATUS_TONE } from "@/features/admin/jira-import/model";

/** JiraImportAdmin lists imports from Jira and connects a new site (step 1 of the wizard). */
export function JiraImportAdmin() {
  const q = useQuery(jiraImportsQuery);
  const navigate = useNavigate();
  const [connecting, setConnecting] = useState(false);
  const list = q.data ?? [];
  const showForm = connecting || (!q.isLoading && list.length === 0);
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Import from Jira"
        subtitle="Bring projects over from Jira Cloud or Jira Data Center with their history: issues keep their keys, and sprints, cycle time and flow charts cover the imported past."
        actions={
          !showForm &&
          !q.isLoading && (
            <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setConnecting(true)}>
              New import
            </Button>
          )
        }
      />
      {q.error && <ErrorBox error={q.error} />}
      {showForm && (
        <Card title="Connect to Jira">
          <div className="p-4">
            <ConnectForm
              onConnected={(imp) => navigate({ to: "/admin/import/$importId", params: { importId: imp.id } })}
              onCancel={list.length > 0 ? () => setConnecting(false) : undefined}
            />
          </div>
        </Card>
      )}
      {q.isLoading ? (
        <PageSpinner />
      ) : (
        list.length > 0 && (
          <Card title="Imports">
            <ul className="divide-y divide-line">
              {list.map((imp) => (
                <li key={imp.id}>
                  <Link
                    to="/admin/import/$importId"
                    params={{ importId: imp.id }}
                    className="flex items-center gap-3 px-4 py-3 hover:bg-surface-2/60"
                  >
                    {imp.flavor === "cloud" ? <Cloud className="size-5 shrink-0 text-faint" aria-hidden /> : <Server className="size-5 shrink-0 text-faint" aria-hidden />}
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">{siteName(imp.baseUrl)}</p>
                      <p className="truncate text-xs text-muted">
                        {imp.config.projects.length > 0 ? imp.config.projects.map((p) => p.jiraKey).join(", ") : "No projects chosen yet"} · created <When iso={imp.createdAt} />
                      </p>
                    </div>
                    {!imp.hasCredentials && <Badge tone="neutral">No credentials</Badge>}
                    <Badge tone={imp.stalled ? "warning" : STATUS_TONE[imp.status]}>{imp.stalled ? "Interrupted" : STATUS_LABEL[imp.status]}</Badge>
                    <ChevronRight className="size-4 shrink-0 text-faint" aria-hidden />
                  </Link>
                </li>
              ))}
            </ul>
          </Card>
        )
      )}
    </div>
  );
}
