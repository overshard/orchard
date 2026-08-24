import { resolve } from "path";
import { defineConfig } from "vite";

// Same shape the Rust projects use, so the Go server can read the manifest the
// same way theirs do. Vite builds a bundle; it never serves anything. Go owns
// every request, in dev and in prod alike.
export default defineConfig({
  root: "static_src",
  base: "/static/",
  publicDir: resolve(__dirname, "public"),
  build: {
    outDir: resolve(__dirname, "../dist"),
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
