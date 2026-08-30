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
            // Hashed, since web/static.go stamps a year of immutable on every
            // asset the manifest lists, and a font swap at an unhashed name
            // could never reach a returning visitor.
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
