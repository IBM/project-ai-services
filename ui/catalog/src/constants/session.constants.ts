export const SESSION_CONFIG = {
  INACTIVITY_TIMEOUT: 15 * 60 * 1000,
  WARNING_TIME: 2 * 60 * 1000,
  TOKEN_REFRESH_BUFFER: 5 * 60 * 1000,
  ACTIVITY_EVENTS: ["mousedown", "keydown", "scroll", "touchstart"] as const,
} as const;
