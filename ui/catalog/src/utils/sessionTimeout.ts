import { SESSION_CONFIG } from "@/constants/session.constants";

export const parseTokenExpiry = (headerValue: string | null): Date | null => {
  if (!headerValue) {
    return null;
  }

  try {
    const date = new Date(headerValue);
    if (isNaN(date.getTime())) {
      console.error("Invalid token expiry date:", headerValue);
      return null;
    }
    return date;
  } catch (error) {
    console.error("Error parsing token expiry:", error);
    return null;
  }
};

export const getTimeUntilExpiry = (expiry: Date | null): number => {
  if (!expiry) {
    return 0;
  }

  const now = new Date().getTime();
  const expiryTime = expiry.getTime();
  const timeRemaining = expiryTime - now;

  return Math.max(0, timeRemaining);
};

export const shouldRefreshToken = (
  expiry: Date | null,
  buffer: number = SESSION_CONFIG.TOKEN_REFRESH_BUFFER,
): boolean => {
  if (!expiry) {
    return false;
  }

  const timeUntilExpiry = getTimeUntilExpiry(expiry);
  return timeUntilExpiry > 0 && timeUntilExpiry <= buffer;
};

export const isTokenExpired = (expiry: Date | null): boolean => {
  if (!expiry) {
    return true;
  }

  return getTimeUntilExpiry(expiry) === 0;
};

export const formatTimeRemaining = (milliseconds: number): string => {
  const totalSeconds = Math.ceil(milliseconds / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;

  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
};

export const getTimeUntilWarning = (
  lastActivity: Date,
  inactivityTimeout: number = SESSION_CONFIG.INACTIVITY_TIMEOUT,
  warningTime: number = SESSION_CONFIG.WARNING_TIME,
): number => {
  const now = new Date().getTime();
  const lastActivityTime = lastActivity.getTime();
  const timeSinceActivity = now - lastActivityTime;
  const timeUntilWarning = inactivityTimeout - warningTime - timeSinceActivity;

  return Math.max(0, timeUntilWarning);
};

export const getTimeUntilLogout = (
  lastActivity: Date,
  inactivityTimeout: number = SESSION_CONFIG.INACTIVITY_TIMEOUT,
): number => {
  const now = new Date().getTime();
  const lastActivityTime = lastActivity.getTime();
  const timeSinceActivity = now - lastActivityTime;
  const timeUntilLogout = inactivityTimeout - timeSinceActivity;

  return Math.max(0, timeUntilLogout);
};
