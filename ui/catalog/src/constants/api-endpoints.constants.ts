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
  GET_DEPLOYED_SERVICES: "/applications?deployment_type=services",
  DELETE_APPLICATION: (id: string) => `/applications/${id}`,
};
