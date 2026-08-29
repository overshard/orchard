import { resolve } from "path";
import { defineConfig } from "vite";

// Project structure mirrors analytics/blog/darkfurrow:
// - All entry points live under static_src/{base,pages,properties}
// - Vite outputs to ../dist/ with content-hashed filenames
// - The Rust binary reads dist/.vite/manifest.json to resolve hashed names
export default defineConfig({
  base: "/static/",
  build: {
    outDir: resolve(__dirname, "../build/dist"),
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      input: {
        base: resolve(__dirname, "static_src/base/index.js"),
        pages: resolve(__dirname, "static_src/pages/index.js"),
        properties: resolve(__dirname, "static_src/properties/index.js"),
      },
    },
  },
  css: {
    preprocessorOptions: {
      scss: { quietDeps: true },
    },
  },
});
