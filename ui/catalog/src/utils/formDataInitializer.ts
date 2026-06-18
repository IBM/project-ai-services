/**
 * Form Data Initializer
 * Creates initial form data structure dynamically from deploy options API response
 */

import type { DeployOptionsResponse } from "@/types/digitalAssistants";
import type {
  DeployFormData,
  ComponentConfig,
  ServiceConfig,
} from "@/components/DeployFlow/types";
import { getDefaultInferenceBackendProviderId } from "./inferenceComponentHelper";

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

    // Determine inference backend provider using generic logic
    // This checks for components with multiple providers that expect model input
    const inferenceBackendProviderId = getDefaultInferenceBackendProviderId(
      service.components,
    );

    services[service.id] = {
      enabled: true,
      version: service.version || deployOptions.version,
      components,
      params: {}, // Service-level params from schema
      // Set inferenceBackend to LLM or reranker's default provider if service has either component
      ...(inferenceBackendProviderId && {
        inferenceBackend: inferenceBackendProviderId,
      }),
    };
  });

  return {
    name: defaultName,
    version: deployOptions.version,
    globalComponents,
    services,
  };
}

// Updates a specific component configuration within a service
export function updateServiceComponent(
  formData: DeployFormData,
  serviceId: string,
  componentType: string,
  updates: Partial<ComponentConfig>,
): DeployFormData {
  return {
    ...formData,
    services: {
      ...formData.services,
      [serviceId]: {
        ...formData.services[serviceId],
        components: {
          ...formData.services[serviceId].components,
          [componentType]: {
            ...formData.services[serviceId].components[componentType],
            ...updates,
          },
        },
      },
    },
  };
}

// Updates service-level parameters for a specific service
export function updateServiceParams(
  formData: DeployFormData,
  serviceId: string,
  params: Record<string, unknown>,
): DeployFormData {
  return {
    ...formData,
    services: {
      ...formData.services,
      [serviceId]: {
        ...formData.services[serviceId],
        params: {
          ...formData.services[serviceId].params,
          ...params,
        },
      },
    },
  };
}

// Updates a global component configuration shared across services
export function updateGlobalComponent(
  formData: DeployFormData,
  componentType: string,
  updates: Partial<ComponentConfig>,
): DeployFormData {
  return {
    ...formData,
    globalComponents: {
      ...formData.globalComponents,
      [componentType]: {
        ...formData.globalComponents[componentType],
        ...updates,
      },
    },
  };
}

// Toggles the enabled/disabled state of a service
export function toggleService(
  formData: DeployFormData,
  serviceId: string,
  enabled: boolean,
): DeployFormData {
  return {
    ...formData,
    services: {
      ...formData.services,
      [serviceId]: {
        ...formData.services[serviceId],
        enabled,
      },
    },
  };
}
