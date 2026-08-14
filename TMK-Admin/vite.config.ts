import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { fileURLToPath } from "node:url";

const adminBase = process.env.ADMIN_BASE_PATH || "/admin/";
const projectRoot = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  base: adminBase,
  plugins: [react()],
  build: {
    outDir: path.resolve(projectRoot, "../TMK-Glance/internal/adminui/dist"),
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5174,
    proxy: {
      "/api": "http://127.0.0.1:18080",
    },
  },
});
