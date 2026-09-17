/**
 * Form Data Initializer
 * Creates initial form data structure dynamically from deploy options API response
 */

import type { DeployOptionsResponse, JSONSchema } from "@/types/api.types";
import type {
  DeployFormData,
  ComponentConfig,
  ServiceConfig,
} from "@/components/DeployFlow/Shared/types";
import { DEFAULT_FORM_DATA } from "@/components/DeployFlow/Shared/utils/formData";
import { parseSchema, getFieldDefault } from "@/utils/schemaParser";

// Initializes form data structure from deploy options with default values.
// An optional serviceSchemas map (keyed by serviceId) is accepted so that
// when schemas are already cached on re-open, their field defaults are seeded
// immediately rather than waiting for DAStepTwo's reactive effect.
// A service may have both components and a direct schema simultaneously — both
// branches are handled independently (Scenario C).
export function initializeFormData(
  deployOptions: DeployOptionsResponse,
  defaultName: string = "Digital assistant (copy)",
  serviceSchemas?: Record<string, JSONSchema | null>,
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

    // Initialize each component for the service (independent of schema branch)
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

    // Seed service-level params from schema defaults when already cached.
    // This is independent of the components branch — both may run (Scenario C).
    const schema = serviceSchemas?.[service.id];
    const params: Record<string, unknown> = {};
    if (schema) {
      const fields = parseSchema(schema);
      fields.forEach((field) => {
        if (!field.uiOnly) {
          params[field.key] = getFieldDefault(field);
        }
      });
    }

    services[service.id] = {
      enabled: true,
      version: service.version || deployOptions.version,
      components,
      params,
    };
  });

  return {
    name: defaultName,
    version: deployOptions.version,
    globalComponents,
    services,
    ...DEFAULT_FORM_DATA,
    dataSources: [],
    uploadFromSourceEnabled: false,
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
