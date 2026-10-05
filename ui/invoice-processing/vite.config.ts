import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');

  return {
    plugins: [react()],
    css: {
      preprocessorOptions: {
        scss: {
          silenceDeprecations: ['if-function'],
        },
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(import.meta.dirname, './src'),
        '@components': path.resolve(import.meta.dirname, './src/components'),
        '@contexts': path.resolve(import.meta.dirname, './src/contexts'),
        '@pages': path.resolve(import.meta.dirname, './src/pages'),
        '@services': path.resolve(import.meta.dirname, './src/services'),
        '@utils': path.resolve(import.meta.dirname, './src/utils'),
        '@constants': path.resolve(import.meta.dirname, './src/constants'),
        '~@ibm/plex': path.resolve(import.meta.dirname, './node_modules/@ibm/plex'),
      },
    },
    server: {
      port: parseInt(env.VITE_PORT, 10) || 4101,
      proxy: {
        '/v1': {
          target: env.VITE_API_TARGET || 'http://localhost:4100',
          changeOrigin: true,
          secure: false,
        },
      },
    },
  };
});
