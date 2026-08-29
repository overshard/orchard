import { resolve } from "path";
import { defineConfig } from "vite";

// Vite builds a bundle and never serves anything. Go owns every request, in dev
// and in prod alike, and reads the manifest to resolve hashed filenames.
export default defineConfig({
  root: "static_src",
  base: "/static/",
  publicDir: resolve(__dirname, "public"),
  build: {
    outDir: resolve(__dirname, "../build/dist"),
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      input: resolve(__dirname, "static_src/index.js"),
      output: {
        entryFileNames: "base-[hash].js",
        assetFileNames: (assetInfo) => {
          if (/\.css$/.test(assetInfo.name)) {
            return "base-[hash].css";
          }
          return "assets/[name]-[hash][extname]";
        },
      },
    },
  },
});
