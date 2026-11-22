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
        // The dev server has no catalog to sign in to by default: point VITE_CATALOG_UI_URL at one,
        // and set CATALOG_API_TARGET for the code exchange.
        '/auth/exchange': {
          target: process.env.CATALOG_API_TARGET || 'http://localhost:8080',
          changeOrigin: true,
          rewrite: () => '/api/v1/auth/code/exchange',
        },
        '/v1/chat/completions': {
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
      },
    },
  };
});
