import { resolve } from "path";
import { defineConfig } from "vite";

// One entry. This site is a grid, a detail page and a 404, so splitting the
// bundle would cost a second request to save a few kilobytes.

export default defineConfig({
  base: "/static/",
  publicDir: resolve(__dirname, "static_src/public"),
  build: {
    outDir: resolve(__dirname, "../build/dist"),
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      input: resolve(__dirname, "static_src/app/index.js"),
      output: {
        entryFileNames: "app-[hash].js",
        assetFileNames: (assetInfo) => {
          const name = assetInfo.name || "";
          if (/\.(woff2?|eot|ttf|otf)$/.test(name)) {
            // Hashed, because web/static.go stamps a year of immutable on every
            // asset the manifest lists and a font swapped at an unhashed name
            // could never reach a returning visitor.
            return "fonts/[name]-[hash][extname]";
          }
          if (/\.css$/.test(name)) {
            return "app-[hash].css";
          }
          return "assets/[name]-[hash][extname]";
        },
      },
    },
  },
  css: {
    preprocessorOptions: {
      scss: { quietDeps: true },
    },
  },
});
