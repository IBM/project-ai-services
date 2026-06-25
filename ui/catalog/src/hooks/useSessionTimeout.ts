import { useEffect, useState, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useAuthStore } from "@/store/auth.store";
import { logout, refreshAccessToken } from "@/services/auth";
import { SESSION_CONFIG } from "@/constants/session.constants";
import {
  getTimeUntilWarning,
  getTimeUntilLogout,
  formatTimeRemaining,
} from "@/utils/sessionTimeout";
import { ROUTES } from "@/constants";
import {
  LogoutReason,
  SESSION_STORAGE_KEYS,
  type LoginLocationState,
} from "@/types/navigation.types";

interface UseSessionTimeoutReturn {
  showWarning: boolean;
  timeRemaining: string;
  extendSession: () => void;
  handleLogout: () => Promise<void>;
}

export const useSessionTimeout = (): UseSessionTimeoutReturn => {
  const navigate = useNavigate();
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated());
  const shouldRefreshToken = useAuthStore((state) =>
    state.shouldRefreshToken(),
  );

  const [lastActivity, setLastActivity] = useState<Date>(new Date());
  const [showWarning, setShowWarning] = useState(false);
  const [timeRemaining, setTimeRemaining] = useState("0:00");

  const warningTimerRef = useRef<number | null>(null);
  const logoutTimerRef = useRef<number | null>(null);
  const countdownTimerRef = useRef<number | null>(null);
  const tokenRefreshTimerRef = useRef<number | null>(null);
  const showWarningRef = useRef<boolean>(false);
  const isMountedRef = useRef<boolean>(true);
  const tokenRefreshAttemptsRef = useRef<number>(0);
  const MAX_REFRESH_ATTEMPTS = 3;

  const clearAllTimers = useCallback(() => {
    if (warningTimerRef.current) {
      clearTimeout(warningTimerRef.current);
      warningTimerRef.current = null;
    }
    if (logoutTimerRef.current) {
      clearTimeout(logoutTimerRef.current);
      logoutTimerRef.current = null;
    }
    if (countdownTimerRef.current) {
      clearInterval(countdownTimerRef.current);
      countdownTimerRef.current = null;
    }
    if (tokenRefreshTimerRef.current) {
      clearTimeout(tokenRefreshTimerRef.current);
      tokenRefreshTimerRef.current = null;
    }
  }, []);

  const performLogout = useCallback(
    async (logoutType: "auto" | "manual") => {
      clearAllTimers();

      if (isMountedRef.current) {
        setShowWarning(false);
        showWarningRef.current = false;
      }

      try {
        await logout();
      } finally {
        if (isMountedRef.current) {
          if (logoutType === "auto") {
            sessionStorage.setItem(
              SESSION_STORAGE_KEYS.LOGOUT_REASON,
              LogoutReason.INACTIVITY,
            );
            navigate(ROUTES.LOGIN, {
              replace: true,
              state: {
                logoutReason: LogoutReason.INACTIVITY,
              } as LoginLocationState,
            });
          } else {
            navigate(ROUTES.LOGOUT, { replace: true });
          }
        }
      }
    },
    [navigate, clearAllTimers],
  );

  const handleAutoLogout = useCallback(
    async () => performLogout("auto"),
    [performLogout],
  );

  const handleManualLogout = useCallback(
    async () => performLogout("manual"),
    [performLogout],
  );

  const updateCountdown = useCallback(() => {
    const timeUntilLogout = getTimeUntilLogout(lastActivity);
    if (timeUntilLogout <= 0) {
      handleAutoLogout();
    } else {
      setTimeRemaining(formatTimeRemaining(timeUntilLogout));
    }
  }, [lastActivity, handleAutoLogout]);

  useEffect(() => {
    if (showWarning) {
      updateCountdown();
      countdownTimerRef.current = setInterval(updateCountdown, 1000);

      return () => {
        if (countdownTimerRef.current) {
          clearInterval(countdownTimerRef.current);
          countdownTimerRef.current = null;
        }
      };
    }
  }, [showWarning, updateCountdown]);

  const resetActivity = useCallback(() => {
    const now = new Date();
    setLastActivity(now);

    if (showWarningRef.current) {
      showWarningRef.current = false;
      setShowWarning(false);
    }

    if (warningTimerRef.current) {
      clearTimeout(warningTimerRef.current);
    }
    if (logoutTimerRef.current) {
      clearTimeout(logoutTimerRef.current);
    }

    const timeUntilWarning = getTimeUntilWarning(now);
    warningTimerRef.current = setTimeout(() => {
      if (isMountedRef.current) {
        showWarningRef.current = true;
        setShowWarning(true);
      }
    }, timeUntilWarning);

    const timeUntilLogout = SESSION_CONFIG.INACTIVITY_TIMEOUT;
    logoutTimerRef.current = setTimeout(() => {
      handleAutoLogout();
    }, timeUntilLogout);
  }, [handleAutoLogout]);

  const handleActivity = useCallback(() => {
    if (!showWarningRef.current) {
      resetActivity();
    }
  }, [resetActivity]);

  useEffect(() => {
    if (!isAuthenticated) {
      clearAllTimers();
      return;
    }

    resetActivity();

    SESSION_CONFIG.ACTIVITY_EVENTS.forEach((event) => {
      window.addEventListener(event, handleActivity);
    });

    return () => {
      SESSION_CONFIG.ACTIVITY_EVENTS.forEach((event) => {
        window.removeEventListener(event, handleActivity);
      });
      clearAllTimers();
    };
  }, [isAuthenticated, handleActivity, resetActivity, clearAllTimers]);

  useEffect(() => {
    if (!isAuthenticated) {
      return;
    }

    const checkTokenExpiry = async () => {
      if (shouldRefreshToken) {
        try {
          await refreshAccessToken();
          // Reset attempts on successful refresh
          tokenRefreshAttemptsRef.current = 0;
        } catch (error) {
          console.error("Failed to refresh token:", error);
          tokenRefreshAttemptsRef.current += 1;

          const errorResponse = error as { response?: { status?: number } };
          const isPermanentFailure =
            errorResponse?.response?.status === 401 ||
            errorResponse?.response?.status === 403;

          if (
            tokenRefreshAttemptsRef.current >= MAX_REFRESH_ATTEMPTS ||
            isPermanentFailure
          ) {
            await handleAutoLogout();
          }
        }
      }
    };

    checkTokenExpiry();
    tokenRefreshTimerRef.current = setInterval(checkTokenExpiry, 30000);

    return () => {
      if (tokenRefreshTimerRef.current) {
        clearInterval(tokenRefreshTimerRef.current);
        tokenRefreshTimerRef.current = null;
      }
    };
  }, [isAuthenticated, shouldRefreshToken, handleAutoLogout]);

  useEffect(() => {
    isMountedRef.current = true;

    return () => {
      isMountedRef.current = false;
      clearAllTimers();
    };
  }, [clearAllTimers]);

  return {
    showWarning,
    timeRemaining,
    extendSession: resetActivity,
    handleLogout: handleManualLogout,
  };
};
