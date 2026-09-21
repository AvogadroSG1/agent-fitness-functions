import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: { exclude: ["node_modules/**", "dist/**", "tests/browser/**"] },
  build: { outDir: "../server/web", emptyOutDir: true, rollupOptions: { output: { entryFileNames: "assets/browser.js", chunkFileNames: "assets/[name].js", assetFileNames: "assets/[name][extname]" } } },
});
