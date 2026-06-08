export const AUTH_ENDPOINTS = {
  LOGIN: "/auth/login",
  LOGOUT: "/auth/logout",
  REFRESH: "/auth/refresh",
};

export const SERVICE_ENDPOINTS = {
  GET_SERVICES: "/services",
};

export const APPLICATION_ENDPOINTS = {
  GET_APPLICATIONS: "/applications",
  DELETE_APPLICATION: (id: string) => `/applications/${id}`,
};
