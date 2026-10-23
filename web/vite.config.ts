import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// The API runs on WFM_API (default :8080); the dev server proxies API, auth and hook routes to it.
const api = process.env.WFM_API ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: Number(process.env.PORT ?? 5173),
    proxy: {
      "/api": { target: api, changeOrigin: false },
      "/auth": { target: api, changeOrigin: false },
      "/healthz": { target: api },
    },
  },
  build: { sourcemap: true, chunkSizeWarningLimit: 1500 },
  test: { environment: "node", include: ["src/**/*.test.ts"] },
});
