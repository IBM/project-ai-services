import type { CatalogModel, CreateModelRequest } from "@/types/api.types";
import type { DeployFormData } from "../../Shared/types";
import type { ModelConfig } from "../types";

interface BuildPayloadOptions {
  formData: DeployFormData;
  model: CatalogModel;
  config: ModelConfig;
}

// Drops empty optional values
const compactParams = (
  params: Record<string, unknown>,
): Record<string, unknown> =>
  Object.fromEntries(
    Object.entries(params).filter(
      ([, value]) => value !== undefined && value !== null && value !== "",
    ),
  );

// Maps the flow state to the create-model payload
export const buildCreateModelPayload = ({
  formData,
  model,
  config,
}: BuildPayloadOptions): CreateModelRequest => {
  const isRemote = formData.deploymentType === "remote";
  const params = compactParams(config.params);

  return {
    deployment_type: isRemote ? "remote" : "local",
    component_type: model.component_type.id,
    name: formData.name.trim(),
    provider_id: config.inferenceBackendId,
    model: model.id,
    profile:
      !isRemote && config.resourceProfileId
        ? config.resourceProfileId
        : undefined,
    worker_name: isRemote ? undefined : formData.workerName,
    params: Object.keys(params).length > 0 ? params : undefined,
  };
};
