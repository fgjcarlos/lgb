import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { writeFileSync } from "node:fs";

// preserveGitkeep recreates `dist/.gitkeep` after Vite empties the output dir.
// The committed placeholder lets `//go:embed all:frontend/dist` compile in a
// fresh clone (or backend-only iteration) without requiring the SPA bundle.
function preserveGitkeep() {
  return {
    name: "preserve-gitkeep",
    closeBundle() {
      writeFileSync(path.resolve(__dirname, "dist/.gitkeep"), "");
    },
  };
}

export default defineConfig({
  plugins: [react(), preserveGitkeep()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        ws: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
