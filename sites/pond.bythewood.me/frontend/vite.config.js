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
    // The font policy is self only, so a small font inlined as a data URI is
    // one the browser refuses.
    assetsInlineLimit: 0,
    rollupOptions: {
      // key and k are the pages that show or carry a pond key, and each is its
      // own entry so neither loads the game.
      input: {
        style: resolve(import.meta.dirname, "src/style.js"),
        index: resolve(import.meta.dirname, "src/index.js"),
        key: resolve(import.meta.dirname, "src/key.js"),
        k: resolve(import.meta.dirname, "src/k.js"),
      },
      output: {
        entryFileNames: "assets/[name]-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
});
