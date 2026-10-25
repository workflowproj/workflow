import { describe, expect, it } from "vitest";
import { connectSnippets } from "./ConnectSnippets";

describe("connectSnippets", () => {
  const server = "https://wfm.example.com";

  it("connects assistants by URL with OAuth when there is no token", () => {
    const s = connectSnippets(server);
    expect(s.map((x) => x.id)).toEqual(["claude", "claude-code", "http", "rest", "cli"]);
    expect(s.find((x) => x.id === "claude")!.code).toBe("https://wfm.example.com/mcp");
    expect(s.find((x) => x.id === "claude-code")!.code).toBe("claude mcp add --transport http workflow https://wfm.example.com/mcp");
    const http = JSON.parse(s.find((x) => x.id === "http")!.code);
    expect(http.mcpServers.workflow).toEqual({ type: "http", url: "https://wfm.example.com/mcp" });
    expect(s.find((x) => x.id === "rest")!.code).toContain("Bearer wfm_pat_…");
  });

  it("sends a just-created token as a header", () => {
    const s = connectSnippets(server, "wfm_pat_secret");
    expect(s.map((x) => x.id)).toEqual(["claude-code", "http", "desktop", "rest", "cli"]);
    expect(s.find((x) => x.id === "claude-code")!.code).toContain('--header "Authorization: Bearer wfm_pat_secret"');
    const http = JSON.parse(s.find((x) => x.id === "http")!.code);
    expect(http.mcpServers.workflow.headers).toEqual({ Authorization: "Bearer wfm_pat_secret" });
    expect(s.find((x) => x.id === "cli")!.code).toContain("WFM_TOKEN=wfm_pat_secret");
  });
});
