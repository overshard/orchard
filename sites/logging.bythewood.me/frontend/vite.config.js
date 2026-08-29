import { resolve } from "path";
import { defineConfig } from "vite";

// Vite output goes to ../build/dist and Go serves it at /static/. The manifest
// is read at runtime so templates resolve the content-hashed names, which is
// what lets everything under /static/ be cached for a year at the edge.
//
// Three entry points, matching what each page actually needs: base is the shell
// every page loads, pages is the public marketing half, and dashboard is the
// charts and the log-line styling that only the three authenticated pages use.
// A visitor who never logs in never downloads Chart.js.

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
        dashboard: resolve(__dirname, "static_src/dashboard/index.js"),
      },
      output: {
        entryFileNames: "assets/[name]-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: (assetInfo) => {
          if (/\.(png|jpg|gif|svg|webp)$/.test(assetInfo.name || "")) {
            return "images/[name]-[hash][extname]";
          }
          return "assets/[name]-[hash][extname]";
        },
      },
    },
  },
  css: {
    preprocessorOptions: {
      scss: {
        quietDeps: true,
      },
    },
  },
});
