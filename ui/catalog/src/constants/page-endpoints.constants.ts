export const ROUTES = {
  LOGIN: "/login",
  LOGOUT: "/logout",
  DIGITAL_ASSISTANTS: "/digital-assistants",
  SERVICES: "/services",
  DATA_CONNECTORS: "/data-connectors",
  DEPLOYMENT_LOCATIONS: "/deployment-locations",
  MODELS: "/models",
  USE_CASE_REFERENCES: "/use-case-references",
} as const;

export const PAGE_TITLES = {
  LOGIN: "Login",
  DIGITAL_ASSISTANTS: "Digital assistants",
  SERVICES: "Services",
  DATA_CONNECTORS: "Data connectors",
  DEPLOYMENT_LOCATIONS: "Deployment locations",
  USE_CASE_REFERENCES: "Use case references",
  MODELS: "Models",
} as const satisfies Partial<Record<keyof typeof ROUTES, string>>;

export const PAGE_SUBTITLES = {
  DIGITAL_ASSISTANTS:
    "Production-ready tools that help users complete tasks and access information through conversation or commands. Assistants integrate multiple services for complex use cases and support retrieval-augmented generation (RAG).",
  SERVICES:
    "Single-purpose AI capabilities designed to perform specific tasks independently or as part of larger solutions.",
  DATA_CONNECTORS:
    "Pre-built integrations with data workflows to connect and synchronize with data sources, exchange data between services, and push outputs to data sinks. Connectors reduce integration effort and accelerate end-to-end AI and data workflows.",
  DEPLOYMENT_LOCATIONS:
    "Deployment locations that provide the infrastructure and runtime targets where AI services are deployed, executed, and managed. They enable consistent deployment, scaling, monitoring, and governance across development, test, and production workloads.",
  USE_CASE_REFERENCES:
    "Ready-to-explore use cases based on real-world AI solutions to help you envision how AI can solve common business problems—and accelerate your AI journey.",
  MODELS:
    "Manage foundation AI models and deployed inference instances across IBM Power worker nodes.",
} as const;

export type PageKey = keyof typeof PAGE_TITLES;
