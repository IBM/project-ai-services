import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';


export default defineConfig(() => {
  return {
    build: {
      outDir: 'build',
    },
    preview: {
      port: 3010,
    },
    plugins: [react()],
    server: {
      port: 3000,
      proxy: {
        "/v1/chat/completions": {
          target: "http://localhost:3001",
          changeOrigin: true,
        },
        "/v1/similarity-search": {
          target: "http://localhost:3001",
          changeOrigin: true,
        },
        "/v1/models": {
          target: "http://localhost:3001",
          changeOrigin: true,
        },
        "/db-status": {
          target: "http://localhost:3001",
          changeOrigin: true,
        }
      },
    },
  };
});
