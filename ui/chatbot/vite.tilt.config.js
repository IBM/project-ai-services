// Vite config used by the root Tiltfile for local development (`tilt up -- --rag_enable=true`).
// It reuses vite.config.js but sends chat requests to the chatbot service and
// similarity-search requests to the similarity service, like the production nginx does.
import { defineConfig } from 'vite';
import baseConfig from './vite.config.js';

const chatbotUrl = process.env.RAG_CHATBOT_URL || 'http://localhost:5000';
const similarityUrl = process.env.RAG_SIMILARITY_URL || 'http://localhost:7000';

const proxyTo = (target) => ({ target, changeOrigin: true });

// Serves /config (nginx does this in the container image).
const chatbotConfigPlugin = {
  name: 'tilt-chatbot-config',
  configureServer(server) {
    server.middlewares.use('/config', (req, res) => {
      res.setHeader('Content-Type', 'application/json');
      res.end(
        JSON.stringify({
          searchMode: process.env.CHATBOT_SEARCH_MODE || 'hybrid',
          numChunksPostReranker:
            process.env.CHATBOT_NUM_CHUNKS_POST_RERANKER || '3',
          rerank: process.env.CHATBOT_RERANK || 'true',
        }),
      );
    });
  },
};

export default defineConfig((env) => {
  const base = baseConfig(env);
  return {
    ...base,
    plugins: [...base.plugins, chatbotConfigPlugin],
    server: {
      ...base.server,
      proxy: {
        '/v1/chat/completions': proxyTo(chatbotUrl),
        '/v1/models': proxyTo(chatbotUrl),
        '/db-status': proxyTo(chatbotUrl),
        '/v1/similarity-search': proxyTo(similarityUrl),
      },
    },
  };
});
