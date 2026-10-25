import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/cn";

/**
 * The ways to connect, as copyable snippets. Without a token: AI assistants add Workflow by its
 * MCP address and sign in through the browser (OAuth); the REST API and CLI use a token. With a
 * token (just created): every client sends it as a header.
 */
export function connectSnippets(server: string, token?: string) {
  const mcp = `${server}/mcp`;
  const pat = token ?? "wfm_pat_…";
  const rest = {
    id: "rest",
    label: "REST API",
    hint: token ? "Every screen uses this API. The OpenAPI description generates clients for any language." : "Send a personal access token. The OpenAPI description generates clients for any language.",
    code: `curl -H "Authorization: Bearer ${pat}" \\\n  "${server}/api/v1/search?jql=assignee%20%3D%20currentUser()"\n\n# API description\ncurl -O ${server}/api/v1/openapi.yaml`,
  };
  const cli = {
    id: "cli",
    label: "CLI",
    hint: "Apply workflows kept in your repository (needs a token with the write scope).",
    code: `export WFM_SERVER=${server}\nexport WFM_TOKEN=${pat}\nwfm workflow apply -f workflow.yaml --project KEY`,
  };
  if (!token) {
    return [
      {
        id: "claude",
        label: "Claude",
        hint: "claude.ai, Claude Desktop and mobile: Settings → Connectors → Add custom connector. Paste this address, choose Connect and approve access in your browser.",
        code: mcp,
      },
      {
        id: "claude-code",
        label: "Claude Code",
        hint: "Run this, then /mcp in Claude Code and choose Authenticate; your browser asks you to approve access.",
        code: `claude mcp add --transport http workflow ${mcp}`,
      },
      {
        id: "http",
        label: "Other MCP clients",
        hint: "Cursor, VS Code, ChatGPT connectors and other clients that support OAuth: add the address and sign in through the browser when they connect.",
        code: JSON.stringify({ mcpServers: { workflow: { type: "http", url: mcp } } }, null, 2),
      },
      rest,
      cli,
    ];
  }
  return [
    {
      id: "claude-code",
      label: "Claude Code",
      hint: "Run in your terminal; the tools appear in Claude Code's /mcp list.",
      code: `claude mcp add --transport http workflow ${mcp} \\\n  --header "Authorization: Bearer ${token}"`,
    },
    {
      id: "http",
      label: "Other MCP clients",
      hint: "For clients that connect over HTTP (Cursor, VS Code, Windsurf and others): add this to their MCP settings.",
      code: JSON.stringify({ mcpServers: { workflow: { type: "http", url: mcp, headers: { Authorization: `Bearer ${token}` } } } }, null, 2),
    },
    {
      id: "desktop",
      label: "Claude Desktop",
      hint: "Claude Desktop can also start a local relay: install the wfm binary, then add this to claude_desktop_config.json.",
      code: JSON.stringify({ mcpServers: { workflow: { command: "wfm", args: ["mcp"], env: { WFM_SERVER: server, WFM_TOKEN: token } } } }, null, 2),
    },
    rest,
    cli,
  ];
}

/** ConnectSnippets shows one tab per way of connecting, each with a copy button. */
export function ConnectSnippets({ server, token, className }: { server: string; token?: string; className?: string }) {
  const snippets = connectSnippets(server, token);
  const [tab, setTab] = useState(snippets[0].id);
  const current = snippets.find((s) => s.id === tab) ?? snippets[0];
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div role="tablist" aria-label="How to connect" className="flex flex-wrap gap-1">
        {snippets.map((s) => (
          <button
            key={s.id}
            type="button"
            role="tab"
            aria-selected={s.id === current.id}
            aria-controls={`connect-${s.id}`}
            onClick={() => setTab(s.id)}
            className={cn(
              "rounded-md px-2.5 py-1 text-[13px] font-medium",
              s.id === current.id ? "bg-accent-soft text-accent" : "text-muted hover:bg-surface-2 hover:text-ink",
            )}
          >
            {s.label}
          </button>
        ))}
      </div>
      <div id={`connect-${current.id}`} role="tabpanel" className="flex flex-col gap-1.5">
        <p className="text-[13px] text-muted">{current.hint}</p>
        <CopyableCode code={current.code} />
      </div>
    </div>
  );
}

export function CopyableCode({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard unavailable (insecure context): the text stays selectable.
    }
  };
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md border border-line bg-surface-2 py-2.5 pr-20 pl-3 font-mono text-[12px] leading-relaxed">{code}</pre>
      <button
        type="button"
        onClick={copy}
        className="absolute top-1.5 right-1.5 inline-flex items-center gap-1 rounded border border-line bg-surface px-2 py-1 text-[12px] text-muted hover:text-ink"
      >
        {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}
