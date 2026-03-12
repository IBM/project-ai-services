import { api } from "@/api/axios";
import { AUTH_ENDPOINTS } from "@/constants/api-endpoints.constants";
import { useAuthStore } from "@/store/auth.store";
import type { LoginRequest, LoginResponse } from "@/types/auth";

export const login = async (payload: LoginRequest): Promise<LoginResponse> => {
  const response = await api.post(AUTH_ENDPOINTS.LOGIN, payload);
  const accessToken = response.data.access_token;
  const refreshToken = response.data.refresh_token;
  useAuthStore.getState().setTokens(accessToken, refreshToken);

  return response.data;
};

export const logout = async () => {
  await api.post(AUTH_ENDPOINTS.LOGOUT);
  useAuthStore.getState().clearTokens();
};

export const refreshAccessToken = async () => {
  const refreshToken = useAuthStore.getState().refreshToken;
  const response = await api.post(AUTH_ENDPOINTS.REFRESH, {
    refresh_token: refreshToken,
  });

  const newAccessToken = response.data.access_token;
  useAuthStore.getState().setAccessToken(newAccessToken);

  return newAccessToken;
};
