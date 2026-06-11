export const AUTH_ENDPOINTS = {
  LOGIN: "/auth/login",
  LOGOUT: "/auth/logout",
  REFRESH: "/auth/refresh",
  ME: "/auth/me",
};

export const DIGITAL_ASSISTANTS_ENDPOINTS = {
  LIST_ARCHITECTURES: "/architectures",
  DEPLOY_OPTIONS: (architectureId: string) =>
    `/architectures/${architectureId}/deploy-options`,
  PROVIDER_PARAMS: (componentType: string, providerId: string) =>
    `/components/${componentType}/providers/${providerId}/params`,
  APPLICATIONS: "/applications",
  APPLICATION_BY_ID: (id: string) => `/applications/${id}`,
  RESOURCES: "/resources",
};

export const SERVICE_ENDPOINTS = {
  GET_SERVICES: "/services",
};

export const APPLICATION_ENDPOINTS = {
  GET_APPLICATIONS: "/applications",
  GET_DEPLOYED_SERVICES: "/applications?deployment_type=services",
  DELETE_APPLICATION: (id: string) => `/applications/${id}`,
};
