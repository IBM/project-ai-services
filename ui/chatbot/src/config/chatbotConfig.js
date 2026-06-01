/**
 * Chatbot Configuration Utility
 *
 * Centralizes environment variable extraction for chatbot settings
 * to avoid duplication across server.js and customSendMessage.jsx
 */

/**
 * Get chatbot configuration from environment variables with defaults
 * @returns {Object} Configuration object with searchMode, topK, and rerank
 */
export function getChatbotConfig() {
  const config = {
    searchMode: process.env.CHATBOT_SEARCH_MODE || 'hybrid',
    topK: parseInt(process.env.CHATBOT_NUM_CHUNKS_POST_RERANKER || '3', 10),
    rerank:
      process.env.CHATBOT_RERANK === 'true' ||
      process.env.CHATBOT_RERANK === true,
  };

  return config;
}

/**
 * Get the target URL from environment variables
 * @returns {string} Target URL for backend services
 */
export function getTargetURL() {
  return process.env.TARGET_URL;
}

/**
 * Get the server port from environment variables
 * @returns {number} Port number for the server
 */
export function getServerPort() {
  return process.env.PORT || 3001;
}

/**
 * Default chatbot configuration values
 */
export const DEFAULT_CONFIG = {
  searchMode: 'hybrid',
  topK: 3,
  rerank: true,
};
