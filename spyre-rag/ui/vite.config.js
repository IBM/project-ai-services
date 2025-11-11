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
  };
});
