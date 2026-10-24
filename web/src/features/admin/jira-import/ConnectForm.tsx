import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, Cloud, PlugZap, Server } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Field, Input } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout } from "@/features/settings/common";
import { cn } from "@/lib/cn";

type Flavor = Schemas["JiraFlavor"];

const flavors: { id: Flavor; name: string; detail: string; icon: typeof Cloud }[] = [
  { id: "cloud", name: "Jira Cloud", detail: "your-site.atlassian.net · email and API token", icon: Cloud },
  { id: "datacenter", name: "Jira Data Center / Server", detail: "Self-hosted · personal access token", icon: Server },
];

/**
 * ConnectForm tests and saves a connection to a Jira site. The token is sent once and stored
 * encrypted; it is never shown again.
 */
export function ConnectForm({ onConnected, onCancel }: { onConnected: (imp: Schemas["JiraImport"]) => void; onCancel?: () => void }) {
  const [flavor, setFlavor] = useState<Flavor>("cloud");
  const [baseUrl, setBaseUrl] = useState("");
  const [email, setEmail] = useState("");
  const [token, setToken] = useState("");
  const body = (): Schemas["JiraConnectionInput"] => ({ flavor, baseUrl: baseUrl.trim(), token: token.trim(), ...(flavor === "cloud" ? { email: email.trim() } : {}) });
  const test = useMutation({ mutationFn: () => unwrap(api.POST("/admin/jira-imports/test-connection", { body: body() })) });
  const create = useMutation({ mutationFn: () => unwrap(api.POST("/admin/jira-imports", { body: body() })), onSuccess: onConnected });
  const error = create.error ?? test.error;
  const fe = error instanceof ApiError ? error.fieldErrors() : {};
  const ready = baseUrl.trim() !== "" && token.trim() !== "" && (flavor !== "cloud" || email.trim() !== "");
  const reset = () => {
    test.reset();
    create.reset();
  };

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (ready) create.mutate();
      }}
    >
      <fieldset>
        <legend className="mb-1.5 text-[13px] font-medium">Jira edition</legend>
        <div className="grid gap-2 sm:grid-cols-2">
          {flavors.map((f) => (
            <button
              type="button"
              key={f.id}
              aria-pressed={flavor === f.id}
              onClick={() => (setFlavor(f.id), reset())}
              className={cn(
                "flex items-start gap-3 rounded-md border p-3 text-left transition-colors",
                flavor === f.id ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong",
              )}
            >
              <f.icon className={cn("mt-0.5 size-5 shrink-0", flavor === f.id ? "text-accent" : "text-faint")} aria-hidden />
              <span>
                <span className="block text-sm font-medium">{f.name}</span>
                <span className="mt-0.5 block text-xs text-muted">{f.detail}</span>
              </span>
            </button>
          ))}
        </div>
      </fieldset>
      <Field
        label="Site URL"
        required
        error={fe.baseUrl}
        hint={flavor === "cloud" ? "For example https://acme.atlassian.net" : "The base URL people open Jira at, e.g. https://jira.example.com"}
      >
        {(id) => (
          <Input
            id={id}
            type="url"
            value={baseUrl}
            onChange={(e) => (setBaseUrl(e.target.value), reset())}
            placeholder={flavor === "cloud" ? "https://acme.atlassian.net" : "https://jira.example.com"}
            autoComplete="off"
          />
        )}
      </Field>
      {flavor === "cloud" && (
        <Field label="Account email" required error={fe.email} hint="The Atlassian account the API token belongs to. It needs Browse permission on the projects to import.">
          {(id) => <Input id={id} type="email" value={email} onChange={(e) => (setEmail(e.target.value), reset())} autoComplete="off" />}
        </Field>
      )}
      <Field
        label={flavor === "cloud" ? "API token" : "Personal access token"}
        required
        error={fe.token}
        hint={
          flavor === "cloud"
            ? "Create one at id.atlassian.com → Security → API tokens. It is stored encrypted, and you can delete it once the import is done."
            : "Create one in Jira under Profile → Personal Access Tokens. It is stored encrypted, and you can delete it once the import is done."
        }
      >
        {(id) => <Input id={id} type="password" value={token} onChange={(e) => (setToken(e.target.value), reset())} autoComplete="new-password" />}
      </Field>
      {test.data && !error && (
        <Callout title={`Connected to ${test.data.serverTitle}`} icon={<CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" />}>
          Signed in as {test.data.user.displayName}
          {test.data.user.email ? ` (${test.data.user.email})` : ""} · {test.data.deploymentType} {test.data.version}
        </Callout>
      )}
      {error && Object.keys(fe).length === 0 && <ErrorBox error={error} />}
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="primary" type="submit" loading={create.isPending} disabled={!ready}>
          Connect
        </Button>
        <Button icon={<PlugZap className="size-4" />} loading={test.isPending} disabled={!ready} onClick={() => test.mutate()}>
          Test connection
        </Button>
        {onCancel && (
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}
