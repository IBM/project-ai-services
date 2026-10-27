import { useMemo, useEffect } from "react";
import { Dropdown } from "@carbon/react";
import styles from "../../Shared/DeployFlow.shared.module.scss";
import type { StepProps } from "../types";
import type { ComponentConfig, DeployFormData } from "../../Shared/types";
import type { LLMOption } from "@/types/api.types";
import { useDeployStore } from "@/store/deploy.store";
import { COMPONENT_TYPES, DEFAULT_RUNTIME } from "@/constants";
import { getKnowledgeBaseComponents } from "../utils/knowledgeBase";

const EMPTY: LLMOption[] = [];

type DAKnowledgeBaseProps = Pick<
  StepProps,
  "deployOptions" | "providerParamsByType" | "runtime"
> & {
  formData: Pick<DeployFormData, "globalComponents">;
  onChange: (updates: Partial<DeployFormData>) => void;
};

interface ComponentRow {
  type: string;
  name: string;
  hasModels: boolean;
  modelOptions: Array<{ id: string; text: string }>;
  selectedModel: string;
  providerOptions: Array<{ id: string; text: string }>;
  /** Providers filtered to those that offer the currently-selected model. */
  filteredProviderOptions: Array<{ id: string; text: string }>;
  selectedProviderId: string;
}

export const DAKnowledgeBase: React.FC<DAKnowledgeBaseProps> = ({
  formData,
  onChange,
  deployOptions,
  providerParamsByType,
  runtime = DEFAULT_RUNTIME,
}) => {
  const globalComponentModels = useDeployStore(
    (state) => state.globalComponentModels,
  );
  const kbComponents = useMemo(
    () => getKnowledgeBaseComponents(deployOptions),
    [deployOptions],
  );
  // Set default model selection when model options arrive and nothing is selected yet.
  useEffect(() => {
    const updates: Record<string, ComponentConfig> = {};
    let hasUpdates = false;

    kbComponents.forEach((component) => {
      const config = formData.globalComponents[component.type];
      if (config?.params?.model) return;

      const models =
        globalComponentModels[`${runtime}:${component.type}`] ?? EMPTY;
      if (models.length === 0) return;

      // Prefer the selected or default provider's first model, fall back to first overall.
      const defaultProviderId =
        config?.providerId ||
        component.providers.find((p) => p.default)?.id ||
        component.providers[0]?.id;
      const defaultModel =
        models.find((m) => m.providerId === defaultProviderId) ?? models[0];

      updates[component.type] = {
        providerId: defaultModel.providerId,
        params: { model: defaultModel.id },
      };
      hasUpdates = true;
    });
    if (hasUpdates) {
      onChange({
        globalComponents: { ...formData.globalComponents, ...updates },
      });
    }
  }, [
    kbComponents,
    formData.globalComponents,
    globalComponentModels,
    onChange,
    runtime,
  ]);

  const rows = useMemo<ComponentRow[]>(() => {
    const built = kbComponents.map((component) => {
      const models =
        globalComponentModels[`${runtime}:${component.type}`] ?? EMPTY;
      const selectedModel =
        (formData.globalComponents[component.type]?.params?.model as string) ||
        "";

      // Deduplicate by id — multiple providers can expose the same model const.
      const seen = new Set<string>();
      const modelOptions: Array<{ id: string; text: string }> = [];
      models.forEach((m) => {
        if (!seen.has(m.id)) {
          seen.add(m.id);
          modelOptions.push({ id: m.id, text: m.text });
        }
      });

      // Provider display names come directly from provider metadata
      const allProviderOptions = component.providers.map((p) => ({
        id: p.id,
        text: p.name,
      }));

      // For model-first components, filter backends to those offering the
      // selected model. Fall back to showing all providers when nothing is
      // selected yet so the dropdown is never empty.
      const providerIdsForModel = selectedModel
        ? new Set(
            models
              .filter((m) => m.id === selectedModel)
              .map((m) => m.providerId),
          )
        : null;
      const filteredProviderOptions =
        providerIdsForModel && providerIdsForModel.size > 0
          ? allProviderOptions.filter((p) => providerIdsForModel.has(p.id))
          : allProviderOptions;

      return {
        type: component.type,
        name: component.name,
        hasModels: modelOptions.length > 0,
        modelOptions,
        selectedModel,
        providerOptions: allProviderOptions,
        filteredProviderOptions,
        selectedProviderId:
          formData.globalComponents[component.type]?.providerId || "",
      };
    });

    // Vector store first, then embedding model (matches the design).
    const rank = (type: string) => {
      if (type === COMPONENT_TYPES.VECTOR_STORE) return 0;
      if (type === COMPONENT_TYPES.EMBEDDING) return 1;
      return 2;
    };
    return built.sort((a, b) => rank(a.type) - rank(b.type));
  }, [kbComponents, formData.globalComponents, globalComponentModels, runtime]);

  // Resolve provider from the selected model and update formData.
  // Keeps the current backend if it already supports the new model (mirrors
  // handleLlmModelChange in ServiceConfigCard); only falls back to the first
  // compatible provider when the current one doesn't offer the chosen model.
  const handleModelChange = (componentType: string, modelId: string) => {
    const models =
      globalComponentModels[`${runtime}:${componentType}`] ?? EMPTY;

    const supportingProviderIds = models
      .filter((m) => m.id === modelId)
      .map((m) => m.providerId);
    if (supportingProviderIds.length === 0) return;

    const currentProviderId =
      formData.globalComponents[componentType]?.providerId ?? "";
    const newProviderId = supportingProviderIds.includes(currentProviderId)
      ? currentProviderId
      : supportingProviderIds[0];

    onChange({
      globalComponents: {
        ...formData.globalComponents,
        [componentType]: {
          providerId: newProviderId,
          params: { model: modelId },
        },
      },
    });
  };

  // Updates only the provider while keeping the current model param intact.
  const handleBackendChange = (componentType: string, providerId: string) => {
    const currentModel =
      (formData.globalComponents[componentType]?.params?.model as string) || "";
    onChange({
      globalComponents: {
        ...formData.globalComponents,
        [componentType]: {
          providerId,
          params: { model: currentModel },
        },
      },
    });
  };

  const handleProviderChange = (componentType: string, providerId: string) => {
    const paramsMap = providerParamsByType[componentType] || {};
    const cachedParams = paramsMap[providerId];
    const properties = cachedParams?.properties as Record<
      string,
      { default?: unknown }
    >;
    const modelParam: Record<string, unknown> = {};
    if (properties?.model?.default) {
      modelParam.model = properties.model.default;
    }

    onChange({
      globalComponents: {
        ...formData.globalComponents,
        [componentType]: { providerId, params: modelParam },
      },
    });
  };

  if (rows.length === 0) return null;

  return (
    <div className={styles.knowledgeBase}>
      <h3 className={styles.knowledgeBaseTitle}>Knowledge base</h3>
      <p className={styles.knowledgeBaseDescription}>
        Embedding model and vector store are used by Digitize documents, Find
        similar items and Question and answer services.
      </p>
      <div className={styles.knowledgeBaseFields}>
        {rows.map((component) => {
          if (component.hasModels) {
            // Model-first components (e.g. embedding): render two dropdowns —
            // one for the model, one for the backend filtered by that model.
            const backendTitle =
              component.type === COMPONENT_TYPES.EMBEDDING
                ? "Embedding backend"
                : `${component.name.replace(/\s*model\s*$/i, "").trim()} backend`;

            const backendLabel =
              component.type === COMPONENT_TYPES.EMBEDDING
                ? "Select embedding backend"
                : `Select ${component.name
                    .replace(/\s*model\s*$/i, "")
                    .trim()
                    .toLowerCase()} backend`;

            return (
              <div key={component.type} className={styles.knowledgeBaseRow}>
                <Dropdown
                  id={`${component.type}-model`}
                  titleText={component.name}
                  label={`Select ${component.name.toLowerCase()}`}
                  items={component.modelOptions}
                  selectedItem={
                    component.modelOptions.find(
                      (m) => m.id === component.selectedModel,
                    ) || null
                  }
                  itemToString={(item) => (item ? item.text : "")}
                  onChange={({
                    selectedItem,
                  }: {
                    selectedItem: { id: string; text: string } | null;
                  }) =>
                    handleModelChange(component.type, selectedItem?.id || "")
                  }
                />
                <Dropdown
                  id={`${component.type}-backend`}
                  titleText={backendTitle}
                  label={backendLabel}
                  items={component.filteredProviderOptions}
                  selectedItem={
                    component.filteredProviderOptions.find(
                      (p) => p.id === component.selectedProviderId,
                    ) || null
                  }
                  itemToString={(item) => (item ? item.text : "")}
                  onChange={({
                    selectedItem,
                  }: {
                    selectedItem: { id: string; text: string } | null;
                  }) =>
                    handleBackendChange(component.type, selectedItem?.id || "")
                  }
                />
              </div>
            );
          }

          // Provider-first components (e.g. vector store): single provider dropdown on its own line.
          return (
            <div key={component.type} className={styles.knowledgeBaseRow}>
              <Dropdown
                id={`${component.type}-provider`}
                titleText={component.name}
                label={`Select ${component.name.toLowerCase()}`}
                items={component.providerOptions}
                selectedItem={
                  component.providerOptions.find(
                    (p) => p.id === component.selectedProviderId,
                  ) || null
                }
                itemToString={(item) => (item ? item.text : "")}
                onChange={({
                  selectedItem,
                }: {
                  selectedItem: { id: string; text: string } | null;
                }) =>
                  handleProviderChange(component.type, selectedItem?.id || "")
                }
              />
            </div>
          );
        })}
      </div>
    </div>
  );
};
