import axios from "axios";
import { API_BASE_URL } from "@/constants/env.constants";
import { useAuthStore } from "@/store/auth.store";
import { refreshAccessToken } from "@/services/auth";
import { AUTH_ENDPOINTS } from "@/constants/api-endpoints.constants";

const AUTH_ROUTES = [
  AUTH_ENDPOINTS.LOGIN,
  AUTH_ENDPOINTS.LOGOUT,
  AUTH_ENDPOINTS.REFRESH,
];
const isAuthRoute = (url?: string) => {
  return AUTH_ROUTES.some((route) => url?.includes(route));
};
let refreshPromise: Promise<string> | null = null;

export const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    "Content-Type": "application/json",
  },
});

api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (!error.config) {
      return Promise.reject(error);
    }
    const originalRequest = error.config;

    if (
      error.response?.status === 401 &&
      !originalRequest._retry &&
      !isAuthRoute(originalRequest.url)
    ) {
      originalRequest._retry = true;

      try {
        if (!refreshPromise) {
          refreshPromise = refreshAccessToken();
        }
        const newToken = await refreshPromise;
        originalRequest.headers.Authorization = `Bearer ${newToken}`;
        return api(originalRequest);
      } catch (refreshError) {
        useAuthStore.getState().clearTokens();
        window.location.href = "/login";
        return Promise.reject(refreshError);
      } finally {
        refreshPromise = null;
      }
    }
    return Promise.reject(error);
  },
);
