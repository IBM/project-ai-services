import type {
  DeployFormData,
  ServiceConfig,
  ComponentConfig,
} from "../../Shared/types";
import type { ServiceDeployOptions } from "@/types/api.types";
import { DEFAULT_FORM_DATA } from "../../Shared/utils/formData";

export const initializeFormData = (
  deployOptions: ServiceDeployOptions,
  selectedServiceId: string,
): DeployFormData => {
  const formData: DeployFormData = {
    name: "Service deployment",
    version: deployOptions.version,
    globalComponents: {}, // Empty for service deployments
    services: {},
    ...DEFAULT_FORM_DATA,
    dataSources: [],
    uploadFromSourceEnabled: false,
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
    const defaultProvider =
      component.providers.find((provider) => provider.default === true) ||
      component.providers[0];

    // Default model seeding is handled reactively in ServicesStepOne via useEffect
    const componentConfig: ComponentConfig = {
      providerId: defaultProvider?.id || "",
      params: {},
    };
    serviceConfig.components[component.type] = componentConfig;
  });

  formData.services[selectedServiceId] = serviceConfig;

  return formData;
};
