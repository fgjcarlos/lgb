import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// vite.config.ts — Vite configuration for LGB frontend.
//
// Phase 0 scaffold only. No custom plugins beyond the standard React plugin.
// The build target is a static SPA under dist/. Requirements: MVP-FND-9.5.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
