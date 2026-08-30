import { resolve } from "path";
import { defineConfig } from "vite";

// Three entry points, so a visitor who never logs in never downloads Chart.js.
// base is the shell every page loads, pages is the public half, and dashboard
// is the charts and log-line styling only the authenticated pages need.

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
