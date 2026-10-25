import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// The API runs on WFM_API (default :8080); the dev server proxies API, auth and hook routes to it.
const api = process.env.WFM_API ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  // Separate caches let several dev servers run side by side.
  cacheDir: process.env.VITE_CACHE_DIR ?? "node_modules/.vite",
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: Number(process.env.PORT ?? 5173),
    proxy: {
      "/api": { target: api, changeOrigin: false },
      "/auth": { target: api, changeOrigin: false },
      "/healthz": { target: api },
      // Shown in the UI as the SCIM base URL and SCM webhook URLs.
      "/scim": { target: api },
      "/hooks": { target: api },
      // MCP for AI assistants (streamable HTTP) and their OAuth sign-in; /oauth/consent is a UI page.
      "/mcp": { target: api, changeOrigin: false },
      "/.well-known": { target: api, changeOrigin: false },
      "/oauth/authorize": { target: api, changeOrigin: false },
      "/oauth/token": { target: api, changeOrigin: false },
      "/oauth/register": { target: api, changeOrigin: false },
      "/oauth/revoke": { target: api, changeOrigin: false },
    },
  },
  build: { sourcemap: true, chunkSizeWarningLimit: 1500 },
  test: { environment: "node", include: ["src/**/*.test.ts"] },
});
