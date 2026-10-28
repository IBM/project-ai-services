export const ROUTES = {
  LOGIN: "/login",
  LOGOUT: "/logout",
  DIGITAL_ASSISTANTS: "/digital-assistants",
  SERVICES: "/services",
  CONNECTORS: "/connectors",
  WORKER_RESOURCES: "/worker-resources",
  USE_CASE_REFERENCES: "/use-case-references",
} as const;

export const PAGE_TITLES = {
  LOGIN: "Login",
  DIGITAL_ASSISTANTS: "Digital assistants",
  SERVICES: "Services",
  CONNECTORS: "Connectors",
  WORKER_RESOURCES: "Worker resources",
  USE_CASE_REFERENCES: "Use case references",
} as const satisfies Partial<Record<keyof typeof ROUTES, string>>;

export type PageKey = keyof typeof PAGE_TITLES;
