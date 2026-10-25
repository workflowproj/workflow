import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, ListFilter, Pencil, Plus, Trash2 } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, EmptyState, Field, IconButton, Input, Modal, PageHeader, PageSpinner, Textarea, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, CopyField, SettingsSection, Switch } from "@/features/settings/common";
import { queuesQuery, serviceDeskQuery, type Queue, type ServiceDesk } from "@/features/servicedesk/api";

// ---------------------------------------------------------------------------
// Help centre (portal) settings

const portalRoute = getRouteApi("/app/projects/$projectKey/settings/portal");

/** PortalSettings: the help centre's name and description, who may raise requests, and self sign-up. */
export function PortalSettings() {
  const { projectKey } = portalRoute.useParams();
  const desk = useQuery(serviceDeskQuery(projectKey));
  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <PageHeader title="Help centre" subtitle="What customers see when they raise requests, and who may raise them." />
      {desk.error && <ErrorBox error={desk.error} />}
      {desk.isLoading ? <PageSpinner /> : desk.data && <PortalForm key={desk.data.projectKey} desk={desk.data} />}
    </div>
  );
}

function PortalForm({ desk }: { desk: ServiceDesk }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(desk.portalName);
  const [description, setDescription] = useState(desk.portalDescription);
  const [access, setAccess] = useState(desk.portalAccess);
  const [selfSignup, setSelfSignup] = useState(desk.selfSignup);
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PATCH("/projects/{projectKey}/service-desk", {
          params: { path: { projectKey: desk.projectKey } },
          body: { portalName: name.trim(), portalDescription: description.trim(), portalAccess: access, selfSignup },
        }),
      ),
    onSuccess: (d) => {
      qc.setQueryData(["service-desk", desk.projectKey], d);
      qc.invalidateQueries({ queryKey: ["portal"] });
      toast.success("Help centre saved");
    },
  });
  const errs = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const dirty = name !== desk.portalName || description !== desk.portalDescription || access !== desk.portalAccess || selfSignup !== desk.selfSignup;
  const signupUrl = new URL(`/portal/signup/${desk.projectKey}`, desk.portalUrl).toString();
  return (
    <>
      {desk.openProject && (
        <Callout tone="warning" title="This project is open to the whole organisation">
          Every organisation member can work here as an agent and read internal notes. To keep requests between the people you support and your team, make the project private in{" "}
          <Link to="/projects/$projectKey/settings" params={{ projectKey: desk.projectKey }} className="font-medium text-accent hover:underline">
            General settings
          </Link>
          .
        </Callout>
      )}
      <SettingsSection title="Address" description="Share this link with the people you support.">
        <div className="p-4">
          <CopyField value={desk.portalUrl} label="Copy link" />
        </div>
      </SettingsSection>
      <form
        className="flex flex-col gap-5"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <SettingsSection title="Details">
          <div className="flex flex-col gap-4 p-4">
            <Field label="Name" required error={errs.portalName}>
              {(id) => <Input id={id} value={name} maxLength={100} onChange={(e) => setName(e.target.value)} required />}
            </Field>
            <Field label="Description" hint="Shown under the name on the help centre's front page." error={errs.portalDescription}>
              {(id) => <Textarea id={id} rows={3} value={description} maxLength={2000} onChange={(e) => setDescription(e.target.value)} />}
            </Field>
          </div>
        </SettingsSection>
        <SettingsSection title="Who can raise requests">
          <div className="flex flex-col gap-4 p-4">
            <fieldset className="flex flex-col gap-2">
              <legend className="sr-only">Who can raise requests</legend>
              {(
                [
                  ["customers", "Customers on the list", "The people on the Customers page, plus the project's own members."],
                  ["organisation", "Customers and everyone in the organisation", "Organisation members (not guests) can raise requests without being added as customers."],
                ] as const
              ).map(([v, label, hint]) => (
                <label
                  key={v}
                  className="flex cursor-pointer items-start gap-2.5 rounded-md border border-line px-3 py-2.5 has-checked:border-accent has-checked:bg-accent-soft/40"
                >
                  <input type="radio" name="portal-access" value={v} checked={access === v} onChange={() => setAccess(v)} className="mt-1 accent-accent" />
                  <span>
                    <span className="block text-sm font-medium">{label}</span>
                    <span className="block text-[13px] text-muted">{hint}</span>
                  </span>
                </label>
              ))}
            </fieldset>
            <div className="flex flex-col gap-1.5 border-t border-line pt-4">
              <Switch checked={selfSignup} onChange={setSelfSignup} label="Let people sign up as customers" />
              <p className="text-[13px] text-muted">
                Anyone with the sign-up link can create a customer account after confirming their email address. Needs password sign-in (local accounts) to be enabled.
              </p>
              {desk.selfSignup && <CopyField value={signupUrl} label="Copy sign-up link" />}
            </div>
          </div>
        </SettingsSection>
        {save.error && !Object.keys(errs).length ? <ErrorBox error={save.error} /> : null}
        <div>
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!dirty || !name.trim()}>
            Save changes
          </Button>
        </div>
      </form>
    </>
  );
}

// ---------------------------------------------------------------------------
// Queues

const queuesRoute = getRouteApi("/app/projects/$projectKey/settings/queues");

/** QueuesSettings: a desk's queues, their order and their JQL. */
export function QueuesSettings() {
  const { projectKey } = queuesRoute.useParams();
  const queues = useQuery(queuesQuery(projectKey));
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [editing, setEditing] = useState<Queue | "new" | null>(null);
  const list = queues.data ?? [];
  const invalidate = () => qc.invalidateQueries({ queryKey: ["queues", projectKey] });
  const move = useMutation({
    mutationFn: async ({ from, to }: { from: number; to: number }) => {
      const next = [...list];
      const [q] = next.splice(from, 1);
      next.splice(to, 0, q);
      // Renumber and save the queues whose position changed.
      await Promise.all(
        next.map((x, i) =>
          x.position === (i + 1) * 10
            ? null
            : unwrap(api.PATCH("/queues/{queueId}", { params: { path: { queueId: x.id } }, body: { name: x.name, jql: x.jql, position: (i + 1) * 10 } })),
        ),
      );
    },
    onSettled: invalidate,
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: (q: Queue) => unwrap(api.DELETE("/queues/{queueId}", { params: { path: { queueId: q.id } } })),
    onSuccess: (_, q) => {
      invalidate();
      toast.success(`${q.name} deleted`);
    },
    onError: toast.error,
  });
  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <PageHeader
        title="Queues"
        subtitle="Saved searches of this project's requests that agents work through, in this order."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
            New queue
          </Button>
        }
      />
      {queues.error && <ErrorBox error={queues.error} />}
      {queues.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<ListFilter className="size-8" />}
            title="No queues"
            body="Add a queue for each kind of work, e.g. unassigned requests or those about to miss their SLA."
          />
        </div>
      ) : (
        <ol className="flex flex-col divide-y divide-line rounded-lg border border-line bg-surface">
          {list.map((q, i) => (
            <li key={q.id} className="flex items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="flex items-baseline gap-2 text-sm font-medium">
                  {q.name}
                  <span className="text-xs font-normal text-muted">
                    {q.count} request{q.count === 1 ? "" : "s"}
                  </span>
                </p>
                <code className="mt-0.5 block truncate font-mono text-xs text-muted" title={q.jql}>
                  {q.jql}
                </code>
              </div>
              <div className="flex shrink-0 items-center">
                <IconButton size="sm" label={`Move ${q.name} up`} disabled={i === 0 || move.isPending} onClick={() => move.mutate({ from: i, to: i - 1 })}>
                  <ArrowUp className="size-4" />
                </IconButton>
                <IconButton size="sm" label={`Move ${q.name} down`} disabled={i === list.length - 1 || move.isPending} onClick={() => move.mutate({ from: i, to: i + 1 })}>
                  <ArrowDown className="size-4" />
                </IconButton>
                <IconButton size="sm" label={`Edit ${q.name}`} onClick={() => setEditing(q)}>
                  <Pencil className="size-4" />
                </IconButton>
                <IconButton
                  size="sm"
                  label={`Delete ${q.name}`}
                  onClick={async () => {
                    if (
                      await confirm({
                        title: `Delete ${q.name}?`,
                        body: "The queue goes away for every agent; its requests are not changed.",
                        confirmLabel: "Delete",
                        danger: true,
                      })
                    )
                      remove.mutate(q);
                  }}
                >
                  <Trash2 className="size-4" />
                </IconButton>
              </div>
            </li>
          ))}
        </ol>
      )}
      <QueueDialog
        projectKey={projectKey}
        queue={editing === "new" ? null : editing}
        open={editing !== null}
        nextPosition={(list.at(-1)?.position ?? 0) + 10}
        onClose={() => setEditing(null)}
      />
    </div>
  );
}

function QueueDialog({ projectKey, queue, open, nextPosition, onClose }: { projectKey: string; queue: Queue | null; open: boolean; nextPosition: number; onClose: () => void }) {
  if (!open) return null;
  return <QueueForm key={queue?.id ?? "new"} projectKey={projectKey} queue={queue} nextPosition={nextPosition} onClose={onClose} />;
}

function QueueForm({ projectKey, queue, nextPosition, onClose }: { projectKey: string; queue: Queue | null; nextPosition: number; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(queue?.name ?? "");
  const [jql, setJql] = useState(queue?.jql ?? "resolution = Unresolved ORDER BY created ASC");
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), jql: jql.trim(), position: queue?.position ?? nextPosition };
      return queue
        ? unwrap(api.PATCH("/queues/{queueId}", { params: { path: { queueId: queue.id } }, body }))
        : unwrap(api.POST("/projects/{projectKey}/queues", { params: { path: { projectKey } }, body }));
    },
    onSuccess: (q) => {
      qc.invalidateQueries({ queryKey: ["queues", projectKey] });
      qc.invalidateQueries({ queryKey: ["queue", q.id] });
      toast.success(queue ? "Queue saved" : `${q.name} added`);
      onClose();
    },
  });
  const errs = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  return (
    <Modal
      open
      onClose={onClose}
      size="lg"
      title={queue ? `Edit ${queue.name}` : "New queue"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="queue-form" loading={save.isPending} disabled={!name.trim()}>
            {queue ? "Save" : "Add queue"}
          </Button>
        </>
      }
    >
      <form
        id="queue-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name" required error={errs.name}>
          {(id) => <Input id={id} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} autoFocus required />}
        </Field>
        <Field
          label="Query"
          error={errs.jql}
          hint={
            <>
              Searched within this project; leave out the project. For example <code className="font-mono">assignee is EMPTY AND resolution = Unresolved</code>,{" "}
              <code className="font-mono">"Time to resolution" &lt; remaining("1h")</code> or <code className="font-mono">approval = pending()</code>. ORDER BY{" "}
              <code className="font-mono">"Time to resolution"</code> puts the most urgent first.
            </>
          }
        >
          {(id) => <Textarea id={id} rows={4} value={jql} onChange={(e) => setJql(e.target.value)} className="font-mono text-[13px]" spellCheck={false} />}
        </Field>
        {save.error && !Object.keys(errs).length ? <ErrorBox error={save.error} /> : null}
      </form>
    </Modal>
  );
}
