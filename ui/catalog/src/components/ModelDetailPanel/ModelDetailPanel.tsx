import { useState, useEffect } from "react";
import { Tag } from "@carbon/react";
import { Badge } from "@carbon/icons-react";
import { fetchCatalogModelDetails } from "@/api/models.api";
import { useModelsStore } from "@/store/models.store";
import { dedupe } from "@/utils/requestManager";
import { CatalogDetailPanel } from "@/components/CatalogDetailPanel";
import styles from "./ModelDetailPanel.module.scss";

export interface ModelDetailPanelProps {
  open: boolean;
  onClose: () => void;
  modelId: string | null;
}

const ModelDetailPanel = ({
  open,
  onClose,
  modelId,
}: ModelDetailPanelProps) => {
  const [failedId, setFailedId] = useState<string | null>(null);

  const setModelDetails = useModelsStore((s) => s.setModelDetails);

  const model = useModelsStore((s) =>
    modelId ? (s.modelDetailsCache[modelId] ?? null) : null,
  );

  const hasFailed = failedId !== null && failedId === modelId;
  const isLoading = open && !!modelId && !model && !hasFailed;

  const error = hasFailed
    ? "Failed to load model details. Please try again."
    : null;

  useEffect(() => {
    if (!open || !modelId || model) return;

    let cancelled = false;

    dedupe(`modelDetails:${modelId}`, () => fetchCatalogModelDetails(modelId))
      .then((details) => {
        if (cancelled) return;

        setModelDetails(modelId, details);
      })
      .catch(() => {
        if (cancelled) return;

        setFailedId(modelId);
      });

    return () => {
      cancelled = true;
      // Clear a prior failure so reopening or switching model retries cleanly
      setFailedId(null);
    };
  }, [open, modelId, model, setModelDetails]);

  return (
    <CatalogDetailPanel
      open={open}
      onClose={onClose}
      title={model?.name || "Model Details"}
      description={model?.description}
      certifiedBy={model?.certified_by}
      license={model?.license}
      badges={
        model?.component_type && (
          <div className={styles.componentTypeBadge}>
            <Badge size={16} className={styles.checkIcon} />
            {model.component_type.name || model.component_type.short_name}
          </div>
        )
      }
      about={model?.about}
      isLoading={isLoading}
      error={error}
    >
      {model?.supported_providers && model.supported_providers.length > 0 && (
        <div>
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Supported providers</h2>
            <div className={styles.providerList}>
              {model.supported_providers.map((provider) => (
                <div key={provider.id} className={styles.providerCard}>
                  <div className={styles.providerInfo}>
                    <div className={styles.providerName}>{provider.name}</div>
                    <div className={styles.providerMeta}>
                      <Tag type="gray" size="sm">
                        {provider.deployment_type === "local"
                          ? "Local"
                          : "Remote"}
                      </Tag>
                      {provider.accelerator_type && (
                        <Tag type="blue" size="sm">
                          {provider.accelerator_type}
                        </Tag>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </section>
          <div className={styles.divider} />
        </div>
      )}
    </CatalogDetailPanel>
  );
};

export default ModelDetailPanel;
