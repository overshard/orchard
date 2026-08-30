import { resolve } from "path";
import { defineConfig } from "vite";

export default defineConfig({
  root: "static_src",
  base: "/static/",
  build: {
    outDir: resolve(__dirname, "../build/dist"),
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      input: resolve(__dirname, "static_src/index.js"),
      output: {
        entryFileNames: "base-[hash].js",
        assetFileNames: (assetInfo) => {
          if (/\.(woff2?|eot|ttf|otf)$/.test(assetInfo.name)) {
            // Hashed, because web/static.go stamps a year of immutable on
            // every asset the manifest lists, and an unhashed name means a
            // font swap ships different bytes at the same URL with no way to
            // invalidate a returning visitor's copy.
            return "fonts/[name]-[hash][extname]";
          }
          if (/\.css$/.test(assetInfo.name)) {
            return "base-[hash].css";
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
