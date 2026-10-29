import { useEffect, useRef, useState } from "react";
import { PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";

/** ApiDocsPage renders this server's OpenAPI description with Swagger UI. Swagger UI is bundled
 * (no CDN: installs may have no internet) and loaded only here. "Try it out" calls this server as
 * the signed-in person: the session cookie goes along, plus the X-Requested-With header that
 * requests changing data need. */
export function ApiDocsPage() {
  const ref = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<"loading" | "ready" | Error>("loading");
  useEffect(() => {
    let cancelled = false;
    Promise.all([import("swagger-ui-dist/swagger-ui-es-bundle.js"), import("swagger-ui-dist/swagger-ui.css")])
      .then(([mod]) => {
        if (cancelled || !ref.current) return;
        mod.default({
          domNode: ref.current,
          url: "/api/v1/openapi.json",
          deepLinking: true,
          docExpansion: "none",
          filter: true,
          defaultModelsExpandDepth: 0,
          requestInterceptor: (req: { headers: Record<string, string> }) => {
            req.headers["X-Requested-With"] = "fetch";
            return req;
          },
          onComplete: () => !cancelled && setState("ready"),
        });
      })
      .catch((e: unknown) => !cancelled && setState(e instanceof Error ? e : new Error(String(e))));
    return () => {
      cancelled = true;
    };
  }, []);
  return (
    <div className="mx-auto max-w-6xl p-4 md:p-6">
      <h1 className="text-2xl font-semibold tracking-tight">API docs</h1>
      <p className="mt-1 text-sm text-muted">
        Every endpoint of this server. <strong>Try it out</strong> runs requests as you; for scripts and CI, create an access token on your profile.
      </p>
      {state === "loading" && <PageSpinner label="Loading the API docs" />}
      {state instanceof Error && <ErrorBox error={state} />}
      {/* Swagger UI's stylesheet is light-only, so it keeps a light surface in dark mode. */}
      <div ref={ref} className="mt-4 rounded-lg bg-white text-black" />
    </div>
  );
}
