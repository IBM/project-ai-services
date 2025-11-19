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
        "/generate": {
          target: "http://localhost:3001",
          changeOrigin: true,
        },
        "/stream": {
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
