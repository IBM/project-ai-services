import type {
  DeployFormData,
  ServiceConfig,
  ComponentConfig,
} from "../../Shared/types";
import type { ServiceDeployOptions, JSONSchema } from "@/types/api.types";
import { DEFAULT_FORM_DATA } from "../../Shared/utils/formData";
import { parseSchema, getFieldDefault } from "@/utils/schemaParser";

export const initializeFormData = (
  deployOptions: ServiceDeployOptions,
  selectedServiceId: string,
  _componentModels?: Record<string, unknown>,
  serviceSchema?: JSONSchema | null,
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

  const hasComponents = (deployOptions.components?.length ?? 0) > 0;

  // Only iterate components when they exist (Scenarios A / C)
  if (hasComponents) {
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
  }

  // Seed service-level params from schema defaults (Scenarios B / C)
  if (serviceSchema) {
    const fields = parseSchema(serviceSchema);
    const defaultParams: Record<string, unknown> = {};
    fields.forEach((field) => {
      if (!field.uiOnly) {
        defaultParams[field.key] = getFieldDefault(field);
      }
    });
    serviceConfig.params = defaultParams;
  }

  formData.services[selectedServiceId] = serviceConfig;

  return formData;
};
