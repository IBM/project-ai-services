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
    const defaultProvider =
      component.providers.find((provider) => provider.default === true) ||
      component.providers[0];
    const defaultModelForProvider = models.find(
      (model) => model.providerId === defaultProvider?.id,
    );

    // Only include params if there is a model for the selected default provider
    // Components like vector_store don't have models and shouldn't have params
    const componentConfig: ComponentConfig = {
      providerId: defaultProvider?.id || "",
      params: defaultModelForProvider
        ? { model: defaultModelForProvider.id }
        : {},
    };
    serviceConfig.components[component.type] = componentConfig;
  });

  formData.services[selectedServiceId] = serviceConfig;

  return formData;
};
