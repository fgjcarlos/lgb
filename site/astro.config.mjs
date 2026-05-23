import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  site: "https://fgjcarlos.github.io",
  base: "/lgb",
  trailingSlash: "ignore",
  vite: {
    plugins: [tailwindcss()],
  },
});
