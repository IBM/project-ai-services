import type { DeployFormData, ServiceConfig, ComponentConfig } from "../types";
import type {
  ServiceDeployOptions,
  LLMOption,
} from "@/services/deployment.api";

export const initializeFormData = (
  deployOptions: ServiceDeployOptions,
  selectedServiceId: string,
  componentModels?: Record<string, LLMOption[]>,
): DeployFormData => {
  const formData: DeployFormData = {
    name: "Service deployment",
    version: deployOptions.version,
    globalComponents: {}, // Empty for service deployments
    services: {},
  };

  // Initialize the selected service with ALL components from API
  const serviceConfig: ServiceConfig = {
    enabled: true,
    version: deployOptions.version,
    components: {},
    params: {},
  };

  // Add ALL components to the service config (no filtering)
  // The API returns only the components needed for this specific service
  deployOptions.components?.forEach((component) => {
    const componentKey = `${selectedServiceId}:${component.type}`;
    const models = componentModels?.[componentKey] || [];

    // Only include params if there are models available
    // Components like vector_store don't have models and shouldn't have params
    const componentConfig: ComponentConfig = {
      providerId: component.providers[0]?.id || "",
      params: models.length > 0 ? { model: models[0].id } : {},
    };

    serviceConfig.components[component.type] = componentConfig;
  });

  formData.services[selectedServiceId] = serviceConfig;

  return formData;
};
