import { useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Search, Trash2, UserPlus, UsersRound } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { hasPermission, useProject } from "@/api/queries";
import { Badge, Button, EmptyState, Field, IconButton, Input, Modal, PageHeader, PageSpinner, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { customerAdded } from "@/features/admin/invitations";
import { Callout, useDebounced, When } from "@/features/settings/common";
import { customersQuery, serviceDeskQuery, type ServiceCustomer } from "@/features/servicedesk/api";

const route = getRouteApi("/app/projects/$projectKey/customers");

/**
 * CustomersPage lists the people who may raise requests in a service project's help centre.
 * Agents with the customer.manage permission add them by email (a new customer account gets an
 * invitation) and remove them.
 */
export function CustomersPage() {
  const { projectKey } = route.useParams();
  const { data: project } = useProject(projectKey);
  const desk = useQuery(serviceDeskQuery(projectKey));
  const [q, setQ] = useState("");
  const dq = useDebounced(q.trim(), 250);
  const list = useQuery({ ...customersQuery(projectKey, dq), placeholderData: keepPreviousData });
  const [adding, setAdding] = useState(false);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const canManage = hasPermission(project, "customer.manage");
  const remove = useMutation({
    mutationFn: (c: ServiceCustomer) => unwrap(api.DELETE("/projects/{projectKey}/customers/{userId}", { params: { path: { projectKey, userId: c.user.id } } })),
    onSuccess: (_, c) => {
      qc.invalidateQueries({ queryKey: ["customers", projectKey] });
      qc.invalidateQueries({ queryKey: ["service-desk", projectKey] });
      toast.success(`${c.user.displayName} removed`);
    },
    onError: toast.error,
  });
  const customers = list.data ?? [];
  return (
    <div className="flex max-w-5xl flex-col gap-5 p-4 md:p-6">
      <PageHeader
        title="Customers"
        subtitle="People who can raise requests in the help centre and follow their own requests there."
        actions={
          canManage && (
            <Button variant="primary" icon={<UserPlus className="size-4" />} onClick={() => setAdding(true)}>
              Add customer
            </Button>
          )
        }
      />
      {desk.data && (
        <Callout title="Who can raise requests">
          <ul className="mt-1 list-disc space-y-0.5 pl-4">
            <li>Customers see only the help centre: the requests they raised or that were shared with them, and replies to the customer. Never internal notes.</li>
            <li>
              {desk.data.portalAccess === "organisation"
                ? "Every organisation member (not guests) can raise requests too, without being listed here."
                : "Organisation members raise requests only if listed here (or if they work in this project)."}
            </li>
            {desk.data.selfSignup && <li>Anyone may sign up from the help centre; they confirm their email address and are added here.</li>}
          </ul>
        </Callout>
      )}
      <div className="relative max-w-sm">
        <Search aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
        <Input type="search" aria-label="Search customers" placeholder="Search by name or email" value={q} onChange={(e) => setQ(e.target.value)} className="pl-8" />
      </div>
      {list.error && <ErrorBox error={list.error} />}
      {list.isLoading ? (
        <PageSpinner />
      ) : customers.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<UsersRound className="size-8" />}
            title={dq ? "No customer matches" : "No customers yet"}
            body={dq ? "Try another name or email address." : "Add the people you support by email; new ones get an invitation to choose a password."}
            action={canManage && !dq && <Button onClick={() => setAdding(true)}>Add customer</Button>}
          />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>Person</th>
                <th className="w-32">Account</th>
                <th className="hidden w-32 sm:table-cell">Added</th>
                {canManage && (
                  <th className="w-12">
                    <span className="sr-only">Remove</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {customers.map((c) => (
                <tr key={c.user.id}>
                  <td>
                    <span className="flex items-center gap-2.5">
                      <UserAvatar user={c.user} size={28} />
                      <span className="min-w-0">
                        <span className="block truncate font-medium">{c.user.displayName}</span>
                        <span className="block truncate text-xs text-muted">{c.user.email}</span>
                      </span>
                    </span>
                  </td>
                  <td>
                    <span className="flex flex-wrap gap-1">
                      <Badge tone={c.account === "customer" ? "neutral" : "accent"}>{c.account === "customer" ? "Customer" : "Member"}</Badge>
                      {c.invited && <Badge tone="warning">Invited</Badge>}
                      {c.active === false && <Badge tone="danger">Deactivated</Badge>}
                    </span>
                  </td>
                  <td className="hidden text-[13px] text-muted sm:table-cell">
                    <When iso={c.addedAt} />
                  </td>
                  {canManage && (
                    <td>
                      <IconButton
                        size="sm"
                        label={`Remove ${c.user.displayName}`}
                        disabled={remove.isPending}
                        onClick={async () => {
                          if (
                            await confirm({
                              title: `Remove ${c.user.displayName}?`,
                              body: "They can no longer raise requests here. Requests they raised stay, and they keep seeing them only if shared with them again.",
                              confirmLabel: "Remove",
                              danger: true,
                            })
                          )
                            remove.mutate(c);
                        }}
                      >
                        <Trash2 className="size-4" />
                      </IconButton>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <AddCustomerDialog projectKey={projectKey} open={adding} onClose={() => setAdding(false)} />
    </div>
  );
}

function AddCustomerDialog({ projectKey, open, onClose }: { projectKey: string; open: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const close = () => {
    setEmail("");
    setName("");
    add.reset();
    onClose();
  };
  const add = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/projects/{projectKey}/customers", { params: { path: { projectKey } }, body: { email: email.trim(), displayName: name.trim() || undefined } })),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["customers", projectKey] });
      qc.invalidateQueries({ queryKey: ["service-desk", projectKey] });
      // An invitation on a server without email reaches nobody: say so.
      if (r.invited && !r.emailed) toast.warning(customerAdded(r));
      else toast.success(customerAdded(r));
      close();
    },
  });
  const errs = add.error instanceof ApiError ? add.error.fieldErrors() : {};
  return (
    <Modal
      open={open}
      onClose={close}
      title="Add customer"
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button variant="primary" type="submit" form="add-customer" loading={add.isPending} disabled={!email.trim()}>
            Add customer
          </Button>
        </>
      }
    >
      <form
        id="add-customer"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (email.trim()) add.mutate();
        }}
      >
        <p className="text-sm text-muted">
          Someone without an account gets a customer account and an email inviting them to choose a password. People who already have an account (customers of another help centre,
          or organisation members) are just added.
        </p>
        <Field label="Email" required error={errs.email}>
          {(id) => <Input id={id} type="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} required />}
        </Field>
        <Field label="Name" hint="For new accounts; they can be asked to change it later." error={errs.displayName}>
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        {add.error && !Object.keys(errs).length ? <ErrorBox error={add.error} /> : null}
      </form>
    </Modal>
  );
}
