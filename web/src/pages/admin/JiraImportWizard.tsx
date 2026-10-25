import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { Badge, PageHeader, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { jiraImportQuery } from "@/features/admin/jira-import/api";
import { ConnectionPanel } from "@/features/admin/jira-import/ConnectionPanel";
import { DryRunStep } from "@/features/admin/jira-import/DryRunStep";
import { ImportStep } from "@/features/admin/jira-import/ImportStep";
import { MappingStep } from "@/features/admin/jira-import/MappingStep";
import { currentStep, reachable, siteName, STATUS_LABEL, STATUS_TONE, type StepKey } from "@/features/admin/jira-import/model";
import { Stepper } from "@/features/admin/jira-import/parts";
import { ProjectsStep } from "@/features/admin/jira-import/ProjectsStep";

const route = getRouteApi("/app/admin/import/$importId");

/** JiraImportWizard walks an import through its steps: connect, projects, mapping, dry run, import. */
export function JiraImportWizard() {
  const { importId } = route.useParams();
  const q = useQuery(jiraImportQuery(importId));
  const [chosen, setChosen] = useState<StepKey | null>(null);
  if (q.isLoading) return <PageSpinner />;
  if (q.error || !q.data) return <ErrorBox error={q.error ?? new Error("Import not found")} />;
  const imp = q.data;
  const step = chosen && reachable(imp, chosen) ? chosen : currentStep(imp);
  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        breadcrumbs={
          <Link to="/admin/import" className="hover:text-ink">
            Import from Jira
          </Link>
        }
        title={siteName(imp.baseUrl)}
        subtitle={`${imp.flavor === "cloud" ? "Jira Cloud" : "Jira Data Center / Server"} · ${imp.serverTitle}`}
        actions={<Badge tone={imp.stalled ? "warning" : STATUS_TONE[imp.status]}>{imp.stalled ? "Interrupted" : STATUS_LABEL[imp.status]}</Badge>}
      />
      <Stepper imp={imp} step={step} onStep={setChosen} />
      {step === "connect" && <ConnectionPanel imp={imp} />}
      {step === "projects" && <ProjectsStep key={imp.id} imp={imp} onNext={() => setChosen("mapping")} />}
      {step === "mapping" && <MappingStep imp={imp} onNext={() => setChosen("dryrun")} />}
      {step === "dryrun" && <DryRunStep imp={imp} onStarted={() => setChosen("import")} />}
      {step === "import" && <ImportStep imp={imp} />}
    </div>
  );
}
