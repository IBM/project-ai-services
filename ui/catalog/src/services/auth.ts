import { api } from "@/api/axios";
import { AUTH_ENDPOINTS } from "@/constants/api-endpoints.constants";
import { useAuthStore } from "@/store/auth.store";
import { useDeployStore } from "@/store/deploy.store";
import { fetchArchitectures } from "@/api/applications.api";
import type { LoginRequest, LoginResponse, UserInfo } from "@/types/auth";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { dedupe } from "@/utils/requestManager";

export const login = async (payload: LoginRequest): Promise<LoginResponse> => {
  const response = await api.post(AUTH_ENDPOINTS.LOGIN, payload);
  const accessToken = response.data.access_token;
  const refreshToken = response.data.refresh_token;
  useAuthStore.getState().setTokens(accessToken, refreshToken);

  // Fetch architectures if not in store
  const deployStore = useDeployStore.getState();

  if (deployStore.architectures.length === 0) {
    try {
      deployStore.setArchitecturesLoading(true);
      const architectures = await fetchArchitectures();
      deployStore.setArchitectures(architectures);
    } catch (error) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : "Failed to fetch architectures";
      deployStore.setArchitecturesError(errorMessage);
    }
  }

  return response.data;
};

export const logout = (): Promise<void> =>
  dedupe("logout", async () => {
    const refreshToken = useAuthStore.getState().refreshToken;
    try {
      await api.post(AUTH_ENDPOINTS.LOGOUT, null, {
        headers: {
          "X-Refresh-Token": refreshToken,
        },
      });
    } finally {
      useAuthStore.getState().clearTokens();
      useAuthStore.getState().clearUserInfo();
      useDeployStore.getState().clearAll();
      useServiceDeployStore.getState().clearAllCache();
    }
  });

export const getUserInfo = (): Promise<UserInfo> =>
  dedupe("getUserInfo", async () => {
    const response = await api.get(AUTH_ENDPOINTS.ME);
    const userInfo: UserInfo = {
      id: response.data.id,
      username: response.data.username,
      name: response.data.name,
    };
    useAuthStore.getState().setUserInfo(userInfo);
    return userInfo;
  });

export const refreshAccessToken = async () => {
  const refreshToken = useAuthStore.getState().refreshToken;
  const response = await api.post(AUTH_ENDPOINTS.REFRESH, {
    refresh_token: refreshToken,
  });

  const newAccessToken = response.data.access_token;
  const newRefreshToken = response.data.refresh_token;

  useAuthStore.getState().setTokens(newAccessToken, newRefreshToken);

  return newAccessToken;
};
