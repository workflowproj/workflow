import { CircleAlert } from "lucide-react";
import { ApiError, errorMessage } from "@/api/client";

/** ErrorBox shows an API error (and its field errors) inline. */
export function ErrorBox({ error, className }: { error: unknown; className?: string }) {
  if (!error) return null;
  const fields = error instanceof ApiError ? (error.problem.errors ?? []) : [];
  return (
    <div role="alert" className={`flex gap-2 rounded-md border border-danger/30 bg-danger-soft px-3 py-2 text-sm text-danger ${className ?? ""}`}>
      <CircleAlert className="mt-0.5 size-4 shrink-0" />
      <div>
        <p>{errorMessage(error)}</p>
        {fields.length > 1 && (
          <ul className="mt-1 list-disc pl-4">
            {fields.map((f, i) => (
              <li key={i}>
                <span className="font-mono text-xs">{f.field}</span>: {f.message}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
