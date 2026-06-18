export const AUTH_ENDPOINTS = {
  LOGIN: "/auth/login",
  LOGOUT: "/auth/logout",
  REFRESH: "/auth/refresh",
  ME: "/auth/me",
};

export const DIGITAL_ASSISTANTS_ENDPOINTS = {
  LIST_ARCHITECTURES: "/architectures",
  LIST_SERVICES: "/services",
  ARCHITECTURE_DETAILS: (architectureId: string) =>
    `/architectures/${architectureId}`,
  DEPLOY_OPTIONS: (architectureId: string) =>
    `/architectures/${architectureId}/deploy-options`,
  PROVIDER_PARAMS: (componentType: string, providerId: string) =>
    `/components/${componentType}/providers/${providerId}/params`,
  SERVICE_PARAMS: (serviceId: string) => `/services/${serviceId}/params`,
  RESOURCES: "/resources",
};

export const SERVICE_ENDPOINTS = {
  GET_SERVICES: "/services",
  GET_SERVICE_DETAILS: (id: string) => `/services/${id}`,
  GET_SERVICE_DEPLOY_OPTIONS: (id: string) => `/services/${id}/deploy-options`,
  GET_SERVICE_PARAMS: (id: string) => `/services/${id}/params`,
  GET_COMPONENT_PROVIDER_PARAMS: (componentType: string, providerId: string) =>
    `/components/${componentType}/providers/${providerId}/params`,
};

export const APPLICATION_ENDPOINTS = {
  GET_APPLICATIONS: "/applications/",
  GET_DEPLOYED_SERVICES: "/applications?deployment_type=services",
  DELETE_APPLICATION: (id: string) => `/applications/${id}`,
  GET_APPLICATION_DETAILS: (id: string) => `/applications/${id}`,
  GET_APPLICATION_RESOURCES: (id: string) => `/applications/${id}/resources`,
  UPDATE_APPLICATION: (id: string) => `/applications/${id}`,
};
