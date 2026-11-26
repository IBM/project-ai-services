import type {
  ModelDeployProfile,
  ModelDeployProvider,
  ProviderSchema,
} from "@/types/api.types";
import { parseSchema, validateField } from "@/utils/schemaParser";
import type { ParsedField } from "@/utils/schemaParser";
import { bytesToGB } from "../../Shared/utils/resources";
import type { CalculatedResources } from "../../Shared/components/ResourceRequirementsPanel";
import type { ModelConfig } from "../types";

// Mirrors the backend name rule; not exposed by any API
const MODEL_NAME_PATTERN = /^[A-Za-z0-9_-]{3,100}$/;

export const MODEL_NAME_ERROR =
  "Use 3-100 letters, numbers, hyphens or underscores";

export const MODEL_RUNTIME_OPTIONS: Record<
  string,
  { label: string; description: string }
> = {
  podman: {
    label: "Red Hat Enterprise Linux (RHEL)",
    description:
      "Deploys the model as a standalone inference container directly on a RHEL host using Podman. Ideal for lightweight, single-host GPU deployments.",
  },
  openshift: {
    label: "Red Hat OpenShift (OCP)",
    description:
      "Deploys the model as a managed inference workload on a Red Hat OpenShift cluster using OpenShift AI and KServe. Ideal for orchestrated, scalable, and air-gapped model serving.",
  },
  remote: {
    label: "Remote",
    description:
      "Connects to an existing model inference endpoint on external infrastructure such as watsonx.ai or a self-hosted vLLM server. Ideal for models already running outside your cluster.",
  },
};

// Default profile of a provider, else its first
const getDefaultProfile = (
  provider: ModelDeployProvider | undefined,
): ModelDeployProfile | undefined =>
  provider?.profiles?.find((profile) => profile.default) ??
  provider?.profiles?.[0];

// Selection for one backend: default profile, no params
export const buildProviderConfig = (
  provider: ModelDeployProvider,
): ModelConfig => ({
  inferenceBackendId: provider.id,
  resourceProfileId: getDefaultProfile(provider)?.id ?? "",
  params: {},
});

// Initial selection: default backend and profile
export const buildDefaultConfig = (
  providers: ModelDeployProvider[],
): ModelConfig =>
  buildProviderConfig(providers.find((p) => p.default) ?? providers[0]);

// Checks the selection still matches an offered provider and profile
export const isConfigResolved = (
  config: ModelConfig,
  providers: ModelDeployProvider[],
): boolean => {
  const provider = providers.find((p) => p.id === config.inferenceBackendId);
  if (!provider) return false;
  if (!provider.profiles?.length) return true;
  return provider.profiles.some((p) => p.id === config.resourceProfileId);
};

// Checks the model name against the naming rule
export const isValidModelName = (name: string): boolean =>
  MODEL_NAME_PATTERN.test(name.trim());

// Dropdown label, e.g. "Default (cards: 1, memory: 24 GB)"
export const profileLabel = (profile: ModelDeployProfile): string => {
  const cards = Object.values(profile.resources.accelerators ?? {}).reduce(
    (sum, count) => sum + count,
    0,
  );
  const memory = bytesToGB(profile.resources.memory ?? 0);
  return `${profile.name} (cards: ${cards}, memory: ${memory} GB)`;
};

// Converts profile resources (bytes) to GB for the requirements tiles
export const profileToCalculatedResources = (
  profile: ModelDeployProfile | undefined,
): CalculatedResources => ({
  cpu: profile?.resources.cpu ?? 0,
  memory: bytesToGB(profile?.resources.memory ?? 0),
  accelerators: profile?.resources.accelerators ?? {},
  storage: bytesToGB(profile?.resources.storage ?? 0),
});

// Schema fields the user fills in; the model is chosen in the first step
export const getParamFields = (schema: ProviderSchema): ParsedField[] =>
  parseSchema(schema).filter((field) => field.key !== "model" && !field.uiOnly);

// Per-field error map for the entered params
export const getParamErrors = (
  schema: ProviderSchema | null,
  params: Record<string, unknown>,
): Record<string, string> => {
  if (!schema) return {};
  const errors: Record<string, string> = {};
  getParamFields(schema).forEach((field) => {
    const error = validateField(params[field.key], field);
    if (error) errors[field.key] = error;
  });
  return errors;
};
