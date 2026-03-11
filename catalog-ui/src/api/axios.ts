import axios from "axios";
import { API_BASE_URL } from "@/constants/env.constants";
import { useAuthStore } from "@/store/auth.store";
import { refreshAccessToken } from "@/services/auth";

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
    const originalRequest = error.config;

    if (error.response?.status === 401 && !originalRequest._retry) {
      originalRequest._retry = true;

      try {
        const newToken = await refreshAccessToken();

        originalRequest.headers.Authorization = `Bearer ${newToken}`;

        return api(originalRequest);
      } catch {
        useAuthStore.getState().clearTokens();
        window.location.href = "/login";
      }
    }

    return Promise.reject(error);
  },
);
