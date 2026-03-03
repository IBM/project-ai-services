import type { LoginRequest, LoginResponse } from "../types/auth";

import { API_BASE_URL } from "@/constants/env.constants";
import { AUTH_ENDPOINTS } from "@/constants/api-endpoints.constants";

export const login = async (payload: LoginRequest): Promise<LoginResponse> => {
  const response = await fetch(`${API_BASE_URL}${AUTH_ENDPOINTS.LOGIN}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    throw new Error("Invalid credentials");
  }

  return response.json() as Promise<LoginResponse>;
};

export const logout = async (token: string): Promise<void> => {
  await fetch(`${API_BASE_URL}${AUTH_ENDPOINTS.LOGOUT}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
    },
  });
};
