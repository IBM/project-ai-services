// Vite config used by the root Tiltfile for local development (`tilt up -- --rag_enable=true`).
// It reuses vite.config.js but sends chat requests to the chatbot service,
// like the production nginx does.
import { defineConfig } from 'vite';
import baseConfig from './vite.config.js';

const chatbotUrl = process.env.RAG_CHATBOT_URL || 'http://localhost:5000';

const proxyTo = (target) => ({ target, changeOrigin: true });

export default defineConfig((env) => {
  const base = baseConfig(env);
  return {
    ...base,
    server: {
      ...base.server,
      proxy: {
        '/v1/chat/completions': proxyTo(chatbotUrl),
        '/v1/models': proxyTo(chatbotUrl),
        '/db-status': proxyTo(chatbotUrl),
      },
    },
  };
});
