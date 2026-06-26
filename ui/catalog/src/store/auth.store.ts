import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { UserInfo } from "@/types/auth";
import {
  getTimeUntilExpiry,
  shouldRefreshToken,
  isTokenExpired,
} from "@/utils/sessionTimeout";

type AuthState = {
  accessToken: string | null;
  refreshToken: string | null;
  tokenExpiry: string | null; // Store as ISO string for persistence
  userInfo: UserInfo | null;
  setTokens: (access: string, refresh: string) => void;
  setAccessToken: (token: string) => void;
  setTokenExpiry: (expiry: Date) => void;
  setUserInfo: (user: UserInfo) => void;
  clearTokens: () => void;
  clearUserInfo: () => void;
  isAuthenticated: () => boolean;
  getTokenExpiry: () => Date | null;
  getTimeUntilExpiry: () => number;
  shouldRefreshToken: () => boolean;
  isTokenExpired: () => boolean;
};

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      accessToken: null,
      refreshToken: null,
      tokenExpiry: null,
      userInfo: null,

      setTokens: (access, refresh) =>
        set({
          accessToken: access,
          refreshToken: refresh,
        }),

      setAccessToken: (token) =>
        set({
          accessToken: token,
        }),

      setTokenExpiry: (expiry) =>
        set({
          tokenExpiry: expiry.toISOString(),
        }),

      setUserInfo: (user) =>
        set({
          userInfo: user,
        }),

      clearTokens: () =>
        set({
          accessToken: null,
          refreshToken: null,
          tokenExpiry: null,
        }),

      clearUserInfo: () =>
        set({
          userInfo: null,
        }),

      isAuthenticated: () => {
        const { accessToken, refreshToken } = get();
        return !!(accessToken && refreshToken);
      },

      getTokenExpiry: () => {
        const { tokenExpiry } = get();
        return tokenExpiry ? new Date(tokenExpiry) : null;
      },

      getTimeUntilExpiry: () => {
        const expiry = get().getTokenExpiry();
        return getTimeUntilExpiry(expiry);
      },

      shouldRefreshToken: () => {
        const expiry = get().getTokenExpiry();
        return shouldRefreshToken(expiry);
      },

      isTokenExpired: () => {
        const expiry = get().getTokenExpiry();
        return isTokenExpired(expiry);
      },
    }),
    {
      name: "auth-storage",
      storage: createJSONStorage(() => sessionStorage),
    },
  ),
);
