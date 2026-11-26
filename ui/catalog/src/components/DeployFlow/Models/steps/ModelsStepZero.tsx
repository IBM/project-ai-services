import {
  Grid,
  Column,
  InlineNotification,
  ClickableTile,
  SkeletonText,
  SkeletonPlaceholder,
  Tag,
} from "@carbon/react";
import { Badge, CheckmarkFilled } from "@carbon/icons-react";
import type { CatalogModel } from "@/types/api.types";
import { PROVIDER_TAG_LABELS } from "@/constants/app.constants";
import sharedStyles from "../../Shared/DeployFlow.shared.module.scss";
import styles from "../ModelsDeployFlow.module.scss";

interface ModelsStepZeroProps {
  title: string;
  models: CatalogModel[];
  isLoading: boolean;
  error: string | null;
  selectedModelId: string | null;
  onModelSelect: (modelId: string) => void;
}

export const ModelsStepZero: React.FC<ModelsStepZeroProps> = ({
  title,
  models,
  isLoading,
  error,
  selectedModelId,
  onModelSelect,
}) => {
  return (
    <>
      <div className={sharedStyles.stepHeader}>
        <h2 className={sharedStyles.stepTitle}>{title}</h2>
      </div>

      <div className={sharedStyles.formSection}>
        {isLoading ? (
          <Grid narrow fullWidth className={sharedStyles.serviceSelectionGrid}>
            {[0, 1, 2, 3].map((i) => (
              <Column
                key={i}
                sm={4}
                md={4}
                lg={4}
                className={sharedStyles.tileColumn}
              >
                <div className={sharedStyles.serviceTile}>
                  <SkeletonText width="60%" />
                  <SkeletonPlaceholder
                    className={sharedStyles.serviceTileSkeleton}
                  />
                </div>
              </Column>
            ))}
          </Grid>
        ) : error ? (
          <InlineNotification
            kind="error"
            role="alert"
            title="Failed to load models."
            subtitle="Cancel and reopen to try again."
            lowContrast
            hideCloseButton
          />
        ) : models.length === 0 ? (
          <InlineNotification
            kind="info"
            title="No models available"
            subtitle="There are no models in the catalog yet."
            lowContrast
            hideCloseButton
          />
        ) : (
          <Grid narrow fullWidth className={sharedStyles.serviceSelectionGrid}>
            {models.map((model) => {
              const isSelected = selectedModelId === model.id;
              const providerTags = [
                ...new Set(
                  model.providers.map((p) => PROVIDER_TAG_LABELS[p.id] ?? p.id),
                ),
              ];

              return (
                <Column
                  key={model.id}
                  sm={4}
                  md={4}
                  lg={4}
                  className={sharedStyles.tileColumn}
                >
                  <ClickableTile
                    id={`model-tile-${model.id}`}
                    role="button"
                    aria-pressed={isSelected}
                    className={`${sharedStyles.serviceTile} ${styles.modelTile} ${
                      isSelected ? sharedStyles.selected : ""
                    }`}
                    onClick={() => onModelSelect(model.id)}
                  >
                    {isSelected && (
                      <CheckmarkFilled
                        size={16}
                        className={sharedStyles.selectedIndicator}
                      />
                    )}
                    <div className={sharedStyles.serviceTileContent}>
                      <span className={styles.componentTypeLabel}>
                        {model.component_type.short_name}
                      </span>
                      <h3 className={sharedStyles.serviceTileName}>
                        {model.name}
                      </h3>
                      {model.certified_by && (
                        <div className={sharedStyles.certifiedBadge}>
                          <Badge size={16} />
                          <span>{model.certified_by}</span>
                        </div>
                      )}
                      <p
                        className={`${sharedStyles.serviceTileDescription} ${styles.tileDescription}`}
                      >
                        {model.description}
                      </p>
                    </div>
                    {providerTags.length > 0 && (
                      <div className={styles.providerTags}>
                        {providerTags.map((tag) => (
                          <Tag key={tag} type="gray" size="sm">
                            {tag}
                          </Tag>
                        ))}
                      </div>
                    )}
                  </ClickableTile>
                </Column>
              );
            })}
          </Grid>
        )}
      </div>
    </>
  );
};
