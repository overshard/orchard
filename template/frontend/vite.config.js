import { resolve } from "path";
import { defineConfig } from "vite";

// Vite is a build step only. Go serves build/dist and reads the manifest to
// find the hashed names, so nothing here ever runs a dev server.
export default defineConfig({
  base: "/static/",
  build: {
    outDir: resolve(import.meta.dirname, "../build/dist"),
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      input: {
        index: resolve(import.meta.dirname, "src/index.js"),
      },
      output: {
        entryFileNames: "assets/[name]-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
});
