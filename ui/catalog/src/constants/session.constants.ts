export const SESSION_CONFIG = {
  INACTIVITY_TIMEOUT: 30 * 1000,
  WARNING_TIME: 20 * 1000,
  TOKEN_REFRESH_BUFFER: 2 * 60 * 1000,
  ACTIVITY_EVENTS: ["mousedown", "keydown", "scroll", "touchstart"] as const,
} as const;
