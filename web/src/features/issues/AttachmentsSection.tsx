import { useCallback, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Download, File, FileArchive, FileCode, FileImage, FileText, Plus, Trash2, Upload } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { hasPermission, useMe, useProject } from "@/api/queries";
import type { IssueDetail } from "@/api/types";
import { IconButton, Modal, Spinner, useConfirm, useToast } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { formatBytes, formatDateTime, relativeTime } from "@/lib/format";
import { Section, useIssue, useIssueAction } from "./issue-context";

type Attachment = Schemas["Attachment"];

/**
 * useAttachmentUpload uploads files one by one (multipart POST) and refreshes the issue.
 * `uploading` lists the names still in flight.
 */
export function useAttachmentUpload(issueKey: string) {
  const qc = useQueryClient();
  const toast = useToast();
  const [uploading, setUploading] = useState<string[]>([]);
  const upload = useCallback(
    async (files: File[]) => {
      if (files.length === 0) return;
      setUploading((u) => [...u, ...files.map((f) => f.name)]);
      let ok = 0;
      for (const file of files) {
        try {
          await unwrap(
            api.POST("/issues/{issueKey}/attachments", {
              params: { path: { issueKey } },
              body: { file: file as unknown as string },
              bodySerializer: () => {
                const fd = new FormData();
                fd.append("file", file, file.name);
                return fd;
              },
            }),
          );
          ok++;
        } catch (err) {
          toast.error(err);
        } finally {
          setUploading((u) => {
            const i = u.indexOf(file.name);
            return i < 0 ? u : [...u.slice(0, i), ...u.slice(i + 1)];
          });
        }
      }
      if (ok > 0) {
        toast.success(ok === 1 ? `Attached ${files.length === 1 ? files[0].name : "1 file"}` : `Attached ${ok} files`);
        qc.invalidateQueries({ queryKey: ["issue", issueKey] });
        qc.invalidateQueries({ queryKey: ["issue-history", issueKey] });
      }
    },
    [issueKey, qc, toast],
  );
  return { upload, uploading };
}

/** AttachmentsSection lists files with thumbnails; files can be added by button or drag and drop. */
export function AttachmentsSection({ upload, uploading }: { upload: (files: File[]) => void; uploading: string[] }) {
  const { issue } = useIssue();
  const input = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<Attachment | null>(null);
  const canUpload = issue.permissions.comment;
  useIssueAction("attach", () => input.current?.click());
  const picker = (
    <input
      ref={input}
      type="file"
      multiple
      hidden
      onChange={(e) => {
        upload(Array.from(e.target.files ?? []));
        e.target.value = "";
      }}
    />
  );
  if (issue.attachments.length === 0 && uploading.length === 0) return canUpload ? picker : null;
  return (
    <Section
      title="Attachments"
      count={issue.attachments.length}
      actions={
        canUpload && (
          <IconButton label="Attach files" size="sm" onClick={() => input.current?.click()}>
            <Plus className="size-4" />
          </IconButton>
        )
      }
    >
      {picker}
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-2.5">
        {issue.attachments.map((a) => (
          <AttachmentCard key={a.id} attachment={a} onPreview={() => setPreview(a)} />
        ))}
        {uploading.map((name, i) => (
          <li key={`${name}-${i}`} className="flex flex-col overflow-hidden rounded-md border border-dashed border-line-strong">
            <div className="flex h-24 items-center justify-center bg-surface-2">
              <Spinner />
            </div>
            <div className="px-2.5 py-2">
              <p className="truncate text-[13px] font-medium" title={name}>
                {name}
              </p>
              <p className="text-xs text-muted">Uploading…</p>
            </div>
          </li>
        ))}
      </ul>
      <Modal open={!!preview} onClose={() => setPreview(null)} title={preview?.filename ?? ""} size="xl">
        {preview && (
          <div className="flex flex-col items-center gap-3">
            <img src={preview.url} alt={preview.filename} className="max-h-[70vh] max-w-full rounded border border-line object-contain" />
            <a href={preview.url} download={preview.filename} className="inline-flex items-center gap-1.5 text-sm text-accent hover:underline">
              <Download className="size-4" /> Download ({formatBytes(preview.size)})
            </a>
          </div>
        )}
      </Modal>
    </Section>
  );
}

const isImage = (a: Attachment) => /^image\/(png|jpe?g|gif|webp|avif|bmp|svg\+xml)$/.test(a.contentType);

function fileIcon(a: Attachment) {
  const ext = a.filename.split(".").pop()?.toLowerCase() ?? "";
  if (a.contentType.startsWith("image/")) return FileImage;
  if (/zip|tar|gzip|compressed|7z|rar/.test(a.contentType) || ["zip", "gz", "tgz", "7z", "rar"].includes(ext)) return FileArchive;
  if (a.contentType.startsWith("text/") || ["md", "txt", "log", "csv", "pdf"].includes(ext)) return FileText;
  if (["json", "yaml", "yml", "js", "ts", "go", "py", "sql", "xml", "html", "css"].includes(ext)) return FileCode;
  return File;
}

function AttachmentCard({ attachment: a, onPreview }: { attachment: Attachment; onPreview: () => void }) {
  const { issue } = useIssue();
  const { data: me } = useMe();
  const { data: project } = useProject(issue.projectKey);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const canDelete = (!!me && a.uploader?.id === me.id) || hasPermission(project, "comment.moderate");
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/attachments/{attachmentId}", { params: { path: { attachmentId: a.id } } })),
    onMutate: () => {
      const prev = qc.getQueryData<IssueDetail>(["issue", issue.key]);
      if (prev) qc.setQueryData<IssueDetail>(["issue", issue.key], { ...prev, attachments: prev.attachments.filter((x) => x.id !== a.id) });
      return { prev };
    },
    onError: (err, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(["issue", issue.key], ctx.prev);
      toast.error(err);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
    },
  });
  const Icon = fileIcon(a);
  const ext = a.filename.includes(".") ? a.filename.split(".").pop()!.toUpperCase() : "";
  return (
    <li className="group relative flex flex-col overflow-hidden rounded-md border border-line bg-surface transition-shadow hover:shadow-sm">
      {isImage(a) ? (
        <button type="button" onClick={onPreview} className="block h-24 overflow-hidden bg-surface-2" aria-label={`Preview ${a.filename}`}>
          <img src={a.url} alt="" loading="lazy" className="size-full object-cover transition-transform group-hover:scale-[1.02]" />
        </button>
      ) : (
        <a href={a.url} download={a.filename} className="flex h-24 flex-col items-center justify-center gap-1 bg-surface-2 text-faint" aria-label={`Download ${a.filename}`}>
          <Icon className="size-8" strokeWidth={1.5} />
          {ext && ext.length <= 5 && <span className="text-[10px] font-bold tracking-wider">{ext}</span>}
        </a>
      )}
      <div className="flex min-w-0 items-start gap-2 px-2.5 py-2">
        <div className="min-w-0 flex-1">
          <a href={a.url} download={a.filename} className="block truncate text-[13px] font-medium hover:text-accent" title={a.filename}>
            {a.filename}
          </a>
          <p className="truncate text-xs text-muted" title={`${a.uploader?.displayName ?? "Unknown"} · ${formatDateTime(a.createdAt)}`}>
            {formatBytes(a.size)} · {relativeTime(a.createdAt)}
          </p>
        </div>
        {a.uploader && <UserAvatar user={a.uploader} size={18} className="mt-0.5" />}
      </div>
      <div className="absolute top-1.5 right-1.5 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
        <a
          href={a.url}
          download={a.filename}
          aria-label={`Download ${a.filename}`}
          title="Download"
          className="inline-flex size-7 items-center justify-center rounded-md bg-surface/90 text-muted shadow-sm hover:text-ink"
        >
          <Download className="size-3.5" />
        </a>
        {canDelete && (
          <button
            type="button"
            aria-label={`Delete ${a.filename}`}
            title="Delete"
            onClick={async () => {
              if (await confirm({ title: "Delete attachment?", body: `${a.filename} will be removed from ${issue.key}.`, confirmLabel: "Delete", danger: true })) remove.mutate();
            }}
            className="inline-flex size-7 items-center justify-center rounded-md bg-surface/90 text-muted shadow-sm hover:text-danger"
          >
            <Trash2 className="size-3.5" />
          </button>
        )}
      </div>
    </li>
  );
}

/** DropOverlay covers the issue while files are dragged over it. */
export function DropOverlay({ issueKey }: { issueKey: string }) {
  return (
    <div className="pointer-events-none absolute inset-2 z-30 flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-accent bg-accent-soft/85 text-accent backdrop-blur-[1px]">
      <Upload className="size-8" />
      <p className="text-sm font-semibold">Drop files to attach them to {issueKey}</p>
    </div>
  );
}

/** useFileDrop tracks drag-and-drop of files over an element and hands dropped files to `onFiles`. */
export function useFileDrop(enabled: boolean, onFiles: (files: File[]) => void) {
  const [over, setOver] = useState(false);
  const depth = useRef(0);
  const hasFiles = (e: React.DragEvent) => Array.from(e.dataTransfer.types).includes("Files");
  const handlers = enabled
    ? {
        onDragEnter: (e: React.DragEvent) => {
          if (!hasFiles(e)) return;
          e.preventDefault();
          depth.current++;
          setOver(true);
        },
        onDragOver: (e: React.DragEvent) => {
          if (!hasFiles(e)) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = "copy";
        },
        onDragLeave: (e: React.DragEvent) => {
          if (!hasFiles(e)) return;
          depth.current = Math.max(0, depth.current - 1);
          if (depth.current === 0) setOver(false);
        },
        onDrop: (e: React.DragEvent) => {
          if (!hasFiles(e)) return;
          e.preventDefault();
          depth.current = 0;
          setOver(false);
          onFiles(Array.from(e.dataTransfer.files));
        },
      }
    : {};
  return { over, handlers };
}
