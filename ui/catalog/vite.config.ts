import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  build: {
    cssMinify: 'esbuild',
  },
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "https://catalog-api.10.20.179.49.nip.io",
        changeOrigin: true,
        secure: false,
      },
    },
  },
});
