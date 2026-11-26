import { useEffect, useState } from "react";
import { fetchModelDeployOptions } from "@/api/models.api";
import { fetchServiceSchemaParams } from "@/api/applications.api";
import { useDeployStore } from "@/store/deploy.store";
import type { ModelDeployOptions, ProviderSchema } from "@/types/api.types";
import { dedupe } from "@/utils/requestManager";

interface FetchState<T> {
  data: T | null;
  isLoading: boolean;
  error: string | null;
}

interface FetchError {
  key: string;
  message: string;
}

// Cache key for remote providers, which are fetched without a runtime
const REMOTE_RUNTIME_KEY = "remote";

// Fetches deploy options into the store unless a fresh entry is cached
export const loadModelDeployOptions = async (
  modelId: string,
  runtime?: string,
): Promise<void> => {
  const runtimeKey = runtime ?? REMOTE_RUNTIME_KEY;
  const store = useDeployStore.getState();
  if (
    store.modelDeployOptions[`${modelId}:${runtimeKey}`] &&
    !store.isModelDeployOptionsStale(modelId, runtimeKey)
  ) {
    return;
  }
  const data = await dedupe(
    `fetchModelDeployOptions:${modelId}:${runtimeKey}`,
    () => fetchModelDeployOptions(modelId, runtime),
  );
  useDeployStore.getState().setModelDeployOptions(modelId, runtimeKey, data);
};

// Returns cached deploy options for a model, fetching when missing or stale
export const useModelDeployOptions = (
  modelId: string | null,
  runtime?: string,
): FetchState<ModelDeployOptions> => {
  const runtimeKey = runtime ?? REMOTE_RUNTIME_KEY;
  const key = modelId ? `${modelId}:${runtimeKey}` : null;
  const cached = useDeployStore((state) =>
    key ? state.modelDeployOptions[key] : undefined,
  );
  const [fetchError, setFetchError] = useState<FetchError | null>(null);

  useEffect(() => {
    if (!key || !modelId) return;

    let cancelled = false;
    loadModelDeployOptions(modelId, runtime)
      .then(() => {
        if (!cancelled) setFetchError(null);
      })
      .catch((err) => {
        if (!cancelled) {
          setFetchError({
            key,
            message:
              err instanceof Error
                ? err.message
                : "Failed to load deploy options",
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [key, modelId, runtime]);

  const error = fetchError?.key === key ? fetchError.message : null;
  return {
    data: cached?.data ?? null,
    isLoading: !!key && !cached && !error,
    error: cached ? null : error,
  };
};

// Returns the cached provider schema, fetching when missing or stale
export const useProviderParamsSchema = (
  schemaUrl: string | undefined,
): FetchState<ProviderSchema> => {
  const key = schemaUrl ?? null;
  const cached = useDeployStore((state) =>
    key ? state.modelProviderSchemas[key] : undefined,
  );
  const [fetchError, setFetchError] = useState<FetchError | null>(null);

  useEffect(() => {
    if (!key) return;
    const store = useDeployStore.getState();
    if (
      store.modelProviderSchemas[key] &&
      !store.isModelProviderSchemaStale(key)
    ) {
      return;
    }

    let cancelled = false;
    dedupe(`fetchProviderParamsSchema:${key}`, () =>
      fetchServiceSchemaParams(key),
    )
      .then((data) => {
        useDeployStore.getState().setModelProviderSchema(key, data);
        if (!cancelled) setFetchError(null);
      })
      .catch((err) => {
        if (!cancelled) {
          setFetchError({
            key,
            message:
              err instanceof Error
                ? err.message
                : "Failed to load provider schema",
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [key]);

  const error = fetchError?.key === key ? fetchError.message : null;
  return {
    data: cached?.data ?? null,
    isLoading: !!key && !cached && !error,
    error: cached ? null : error,
  };
};
