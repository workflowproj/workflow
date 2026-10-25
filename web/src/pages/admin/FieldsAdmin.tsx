import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, TextCursorInput, Trash2 } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { fieldsQuery } from "@/api/queries";
import { Badge, Button, EmptyState, IconButton, PageHeader, PageSpinner, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { FIELD_TYPES, FieldDialog } from "@/features/admin/FieldDialog";

type FieldDef = Schemas["FieldDefinition"];

export function FieldsAdmin() {
  const fields = useQuery(fieldsQuery);
  const [editing, setEditing] = useState<FieldDef | "new" | null>(null);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const remove = useMutation({
    mutationFn: (f: FieldDef) => unwrap(api.DELETE("/fields/{fieldId}", { params: { path: { fieldId: f.id } } })),
    onSuccess: (_, f) => {
      qc.invalidateQueries({ queryKey: ["fields"] });
      toast.success(`Deleted ${f.name}`);
    },
    onError: toast.error,
  });
  const list = fields.data ?? [];

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Custom fields"
        subtitle="Extra fields on issues, searchable with JQL and usable in workflow screens, conditions and automation."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
            New field
          </Button>
        }
      />
      {fields.error && <ErrorBox error={fields.error} />}
      {fields.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<TextCursorInput className="size-8" />}
            title="No custom fields"
            body="Add fields such as “Customer impact” (select) or “Environment” (text) and limit them to some projects or issue types."
            action={<Button onClick={() => setEditing("new")}>New field</Button>}
          />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>Field</th>
                <th>Type</th>
                <th>Options</th>
                <th>Applies to</th>
                <th className="w-20">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {list.map((f) => (
                <tr key={f.id} className="hover:bg-surface-2/60">
                  <td>
                    <span className="block font-medium">{f.name}</span>
                    <code className="font-mono text-xs text-muted">{f.key}</code>
                  </td>
                  <td>
                    <Badge>{FIELD_TYPES.find((t) => t.id === f.type)?.label ?? f.type}</Badge>
                  </td>
                  <td className="max-w-56">
                    {f.options.length ? (
                      <span className="flex flex-wrap gap-1">
                        {f.options.slice(0, 4).map((o) => (
                          <span key={o} className="rounded bg-surface-2 px-1.5 py-0.5 text-xs">
                            {o}
                          </span>
                        ))}
                        {f.options.length > 4 && <span className="text-xs text-muted">+{f.options.length - 4}</span>}
                      </span>
                    ) : (
                      <span className="text-faint">—</span>
                    )}
                  </td>
                  <td className="text-[13px] text-muted">
                    <span className="block">{f.projectKeys?.length ? f.projectKeys.join(", ") : "All projects"}</span>
                    <span className="block text-xs">{f.issueTypeKeys?.length ? f.issueTypeKeys.join(", ") : "All issue types"}</span>
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <IconButton label={`Edit ${f.name}`} size="sm" onClick={() => setEditing(f)}>
                      <Pencil className="size-4" />
                    </IconButton>
                    <IconButton
                      label={`Delete ${f.name}`}
                      size="sm"
                      onClick={async () => {
                        if (
                          await confirm({
                            title: `Delete ${f.name}?`,
                            body: "The field disappears from issues, screens and search. Stored values are kept on issues but no longer shown.",
                            confirmLabel: "Delete field",
                            danger: true,
                          })
                        )
                          remove.mutate(f);
                      }}
                    >
                      <Trash2 className="size-4" />
                    </IconButton>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing && <FieldDialog field={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
    </div>
  );
}
