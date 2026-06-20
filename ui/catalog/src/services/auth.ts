import { api } from "@/api/axios";
import { AUTH_ENDPOINTS } from "@/constants/api-endpoints.constants";
import { useAuthStore } from "@/store/auth.store";
import { useDeployStore } from "@/store/deploy.store";
import { fetchArchitectures } from "@/api/digitalAssistants";
import type { LoginRequest, LoginResponse, UserInfo } from "@/types/auth";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";

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

export const logout = async () => {
  const refreshToken = useAuthStore.getState().refreshToken;

  try {
    await api.post(AUTH_ENDPOINTS.LOGOUT, null, {
      headers: {
        "X-Refresh-Token": refreshToken,
      },
    });
  } finally {
    // Clear auth store state
    useAuthStore.getState().clearTokens();
    useAuthStore.getState().clearUserInfo();

    // Clear all deploy store data
    useDeployStore.getState().clearAll();

    // Clear all service deploy store data
    useServiceDeployStore.getState().clearAllCache();
  }
};

export const getUserInfo = async (): Promise<UserInfo> => {
  const response = await api.get(AUTH_ENDPOINTS.ME);
  const userInfo: UserInfo = {
    id: response.data.id,
    username: response.data.username,
    name: response.data.name,
  };
  useAuthStore.getState().setUserInfo(userInfo);
  return userInfo;
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
