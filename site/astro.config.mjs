import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";

// Canonical Markdown keeps repository-relative links for GitHub readers.
function operatorDocLinks() {
  return (tree, { path }) => {
    const adrIndex = path?.endsWith("/docs/adr/README.md");
    if (!adrIndex && !path?.endsWith("/docs/deployment.md") && !path?.endsWith("/docs/backup-restore.md")) return;
    const visit = (node) => {
      if (node.type === "link" || node.type === "definition") {
        const [file, fragment] = node.url.split("#", 2);
        const guide = { "deployment.md": "deployment", "backup-restore.md": "backup-restore" }[file];
        if (guide) node.url = `/lgb/${guide}${fragment ? `#${fragment}` : ""}`;
        else if (/^(?:adr\/)?\d{4}-[\w-]+\.md$|^api\/openapi\.yaml$/.test(file)) {
          node.url = `https://github.com/fgjcarlos/lgb/blob/main/docs/${adrIndex ? "adr/" : ""}${node.url}`;
        }
      }
      node.children?.forEach(visit);
    };
    visit(tree);
  };
}

export default defineConfig({
  site: "https://fgjcarlos.github.io",
  base: "/lgb",
  trailingSlash: "ignore",
  markdown: { remarkPlugins: [operatorDocLinks] },
  vite: {
    plugins: [tailwindcss()],
  },
});
