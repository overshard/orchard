import { resolve } from "path";
import { defineConfig } from "vite";

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
