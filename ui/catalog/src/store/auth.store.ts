import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { UserInfo } from "@/types/auth";

type AuthState = {
  accessToken: string | null;
  refreshToken: string | null;
  userInfo: UserInfo | null;
  setTokens: (access: string, refresh: string) => void;
  setAccessToken: (token: string) => void;
  setUserInfo: (user: UserInfo) => void;
  clearTokens: () => void;
  clearUserInfo: () => void;
  isAuthenticated: () => boolean;
};

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      accessToken: null,
      refreshToken: null,
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

      setUserInfo: (user) =>
        set({
          userInfo: user,
        }),

      clearTokens: () =>
        set({
          accessToken: null,
          refreshToken: null,
        }),

      clearUserInfo: () =>
        set({
          userInfo: null,
        }),

      isAuthenticated: () => {
        const { accessToken, refreshToken } = get();
        return !!(accessToken && refreshToken);
      },
    }),
    {
      name: "auth-storage",
      storage: createJSONStorage(() => sessionStorage),
    },
  ),
);
