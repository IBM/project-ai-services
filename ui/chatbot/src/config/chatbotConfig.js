/**
 * Chatbot Configuration Utility
 *
 * Centralizes environment variable extraction for the proxy server
 */

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
