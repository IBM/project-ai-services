import { useReducer, useCallback } from "react";
import { ServiceConfigCard } from "../components/ServiceConfigCard";
import type { ServiceConfig, ServiceConfigField } from "../types";
import type {
  DeployOptionsComponent as Component,
  LLMOption,
  JSONSchema,
  ProviderSchema,
} from "@/types/api.types";

export interface SharedStepTwoServiceItem {
  serviceId: string;
  serviceName: string;
  config: ServiceConfig;
  description: string;
  fields: ServiceConfigField[];
  inferenceComponent: Component | null;
  serviceSchema: JSONSchema | null;
  llmModelsWithProviders: LLMOption[];
}

export interface SharedStepTwoProps {
  services: SharedStepTwoServiceItem[];
  providerParamsByType: Record<string, Record<string, ProviderSchema>>;
  onChange: (serviceId: string, updated: ServiceConfig) => void;
  onEditingChange?: (isEditing: boolean) => void;
}

interface State {
  editingService: string | null;
  tempConfig: ServiceConfig | null;
}

type Action =
  | { type: "START_EDIT"; serviceId: string; config: ServiceConfig }
  | { type: "UPDATE_TEMP"; payload: Partial<ServiceConfig> }
  | { type: "RESET" };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "START_EDIT":
      return {
        editingService: action.serviceId,
        tempConfig: { ...action.config },
      };
    case "UPDATE_TEMP": {
      if (!state.tempConfig) return { ...state, tempConfig: null };
      const prev = state.tempConfig;
      const next = action.payload;
      return {
        ...state,
        tempConfig: {
          ...prev,
          ...next,
          // Shallow-merge at the components-map level so a single-key update
          // (e.g. change one provider) does not wipe sibling components.
          components:
            next.components !== undefined
              ? { ...prev.components, ...next.components }
              : prev.components,
          // params is REPLACED, not merged. Every caller that touches params
          // builds the complete intended params object before dispatching
          // (see the credential and service-schema onChange handlers in
          // ServiceConfigCard — both construct a full mergedParams).
          // Merging here would re-introduce provider keys that a backend
          // switch (inferenceBackend dropdown) intentionally cleared.
          params: next.params !== undefined ? next.params : prev.params,
        },
      };
    }
    case "RESET":
      return { editingService: null, tempConfig: null };
  }
}

export const SharedStepTwo: React.FC<SharedStepTwoProps> = ({
  services,
  providerParamsByType,
  onChange,
  onEditingChange,
}) => {
  const [state, dispatch] = useReducer(reducer, {
    editingService: null,
    tempConfig: null,
  });

  const handleEdit = useCallback(
    (serviceId: string, config: ServiceConfig) => {
      dispatch({ type: "START_EDIT", serviceId, config });
      onEditingChange?.(true);
    },
    [onEditingChange],
  );

  const handleApply = useCallback(
    (serviceId: string) => {
      if (!state.tempConfig) return;
      onChange(serviceId, state.tempConfig);
      dispatch({ type: "RESET" });
      onEditingChange?.(false);
    },
    [state.tempConfig, onChange, onEditingChange],
  );

  const handleCancel = useCallback(() => {
    dispatch({ type: "RESET" });
    onEditingChange?.(false);
  }, [onEditingChange]);

  const handleUpdateConfig = useCallback((updates: Partial<ServiceConfig>) => {
    dispatch({ type: "UPDATE_TEMP", payload: updates });
  }, []);

  return (
    <>
      {services.map((item) => {
        const isEditing = state.editingService === item.serviceId;
        const currentConfig = isEditing ? state.tempConfig : item.config;

        return (
          <ServiceConfigCard
            key={item.serviceId}
            serviceId={item.serviceId}
            serviceName={item.serviceName}
            config={item.config}
            description={item.description}
            fields={item.fields}
            isEditing={isEditing}
            currentConfig={currentConfig}
            providerParamsByType={providerParamsByType}
            inferenceComponent={item.inferenceComponent}
            serviceSchema={item.serviceSchema}
            llmModelsWithProviders={item.llmModelsWithProviders}
            onEdit={() => handleEdit(item.serviceId, item.config)}
            onApply={() => handleApply(item.serviceId)}
            onCancel={handleCancel}
            onUpdateConfig={handleUpdateConfig}
          />
        );
      })}
    </>
  );
};
