import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig(() => {
  return {
    build: {
      outDir: 'build',
      cssMinify: 'esbuild',
    },
    css: {
      preprocessorOptions: {
        scss: {
          silenceDeprecations: ['if-function'],
        },
      },
    },
    preview: {
      port: 3010,
    },
    plugins: [react()],
    server: {
      port: 3000,
      proxy: {
        '/v1/chat/completions': {
          target: 'http://localhost:3001',
          changeOrigin: true,
        },
        '/v1/similarity-search': {
          target: 'http://localhost:3001',
          changeOrigin: true,
        },
        '/v1/models': {
          target: 'http://localhost:3001',
          changeOrigin: true,
        },
        '/db-status': {
          target: 'http://localhost:3001',
          changeOrigin: true,
        },
        '/config': {
          target: 'http://localhost:3001',
          changeOrigin: true,
        },
      },
    },
  };
});
