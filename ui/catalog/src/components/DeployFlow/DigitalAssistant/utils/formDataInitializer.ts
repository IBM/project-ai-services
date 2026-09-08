/**
 * Form Data Initializer
 * Creates initial form data structure dynamically from deploy options API response
 */

import type { DeployOptionsResponse } from "@/types/api.types";
import type {
  DeployFormData,
  ComponentConfig,
  ServiceConfig,
} from "@/components/DeployFlow/Shared/types";
import { DEFAULT_FORM_DATA } from "@/components/DeployFlow/Shared/utils/formData";

// Initializes form data structure from deploy options with default values
export function initializeFormData(
  deployOptions: DeployOptionsResponse,
  defaultName: string = "Digital assistant (copy)",
): DeployFormData {
  // Initialize global components
  const globalComponents: Record<string, ComponentConfig> = {};

  deployOptions.global_components.forEach((component) => {
    // Find default provider or use first one
    const defaultProvider =
      component.providers.find((p) => p.default) || component.providers[0];

    if (defaultProvider) {
      globalComponents[component.type] = {
        providerId: defaultProvider.id,
        params: {}, // Will be populated when user selects/changes provider
      };
    }
  });

  // Initialize services
  const services: Record<string, ServiceConfig> = {};

  deployOptions.services.forEach((service) => {
    const components: Record<string, ComponentConfig> = {};

    // Initialize each component for the service
    service.components.forEach((component) => {
      const defaultProvider =
        component.providers.find((p) => p.default) || component.providers[0];

      if (defaultProvider) {
        components[component.type] = {
          providerId: defaultProvider.id,
          params: {}, // Will be populated from schema
        };
      }
    });

    services[service.id] = {
      enabled: true,
      version: service.version || deployOptions.version,
      components,
      params: {},
    };
  });

  return {
    name: defaultName,
    version: deployOptions.version,
    globalComponents,
    services,
    ...DEFAULT_FORM_DATA,
  };
}
