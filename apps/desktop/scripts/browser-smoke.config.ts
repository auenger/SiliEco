import { resolve } from "node:path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin({ exclude: ["@silieco/core"] })],
    build: {
      outDir: "out/browser-smoke",
      rollupOptions: { input: resolve("scripts/page-feedback-smoke.ts"), external: ["electron"], output: { entryFileNames: "index.cjs", format: "cjs" } },
    },
  },
  renderer: {
    root: resolve("scripts/browser-smoke"),
    plugins: [react(), tailwindcss()],
    resolve: { dedupe: ["react", "react-dom", "@tanstack/react-query"] },
    build: {
      outDir: resolve("out/browser-smoke/ui"),
      rollupOptions: { input: resolve("scripts/browser-smoke/index.html") },
    },
  },
});
