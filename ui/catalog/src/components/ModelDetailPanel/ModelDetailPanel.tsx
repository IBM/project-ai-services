import { useState, useEffect } from "react";
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
    ? "Failed to load model details. Close and reopen the panel to try again."
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
    />
  );
};

export default ModelDetailPanel;
