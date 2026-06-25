export const LogoutReason = {
  INACTIVITY: "INACTIVITY",
  MANUAL: "MANUAL",
  TOKEN_EXPIRED: "TOKEN_EXPIRED",
} as const;

export type LogoutReasonType = (typeof LogoutReason)[keyof typeof LogoutReason];

export interface LoginLocationState {
  logoutReason?: LogoutReasonType;
  message?: string;
}

export const SESSION_STORAGE_KEYS = {
  LOGOUT_REASON: "logout_reason",
  LOGOUT_MESSAGE: "logout_message",
} as const;
