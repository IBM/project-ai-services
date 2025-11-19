import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';


export default defineConfig(() => {
  return {
    build: {
      outDir: 'build',
    },
    server: {
      port: 3000,
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
        "/reference": {
          target: "http://localhost:3001",
          changeOrigin: true,
        },
      },
    },
  };
});
