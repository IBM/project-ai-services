import { Tag } from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import { Badge, Deploy, ArrowRight } from "@carbon/icons-react";
import type {
  CatalogModelProvider,
  ModelComponentType,
} from "@/types/api.types";
import { PROVIDER_TAG_LABELS } from "@/constants/app.constants";
import styles from "./ModelCard.module.scss";

export interface ModelCardProps {
  id: string;
  name: string;
  componentType: ModelComponentType;
  description: string;
  certifiedBy: string;
  providers: CatalogModelProvider[];
  onDeploy?: (id: string) => void;
}

const ModelCard = ({
  id,
  name,
  componentType,
  description,
  certifiedBy,
  providers,
  onDeploy,
}: ModelCardProps) => {
  const providerNames = [...new Set(providers.map((p) => p.name))].join(", ");
  const providerTags = [
    ...new Set(providers.map((p) => PROVIDER_TAG_LABELS[p.id] ?? p.id)),
  ];

  return (
    <div className={styles.cardWrapper}>
      <ProductiveCard
        primaryButtonIcon={ArrowRight}
        primaryButtonText={" "}
        secondaryButtonIcon={Deploy}
        secondaryButtonText="Deploy"
        onSecondaryButtonClick={() => onDeploy?.(id)}
        className={styles.productiveCard}
      >
        <div className={styles.cardHeader}>
          <span className={styles.componentTypeBadge}>
            <Badge size={16} className={styles.badgeIcon} />
            <span className={styles.componentTypeLabel}>
              {componentType.short_name}
            </span>
          </span>
          <h1 className={styles.cardTitle}>{name}</h1>
          <span className={styles.certifiedBy}>By {certifiedBy}</span>
        </div>
        <p className={styles.description}>{description}</p>
        {providerNames && (
          <p className={styles.providerNames}>{providerNames}</p>
        )}
      </ProductiveCard>
      {providerTags.length > 0 && (
        <div className={styles.providerTags}>
          {providerTags.map((tag) => (
            <Tag key={tag} type="gray" size="sm">
              {tag}
            </Tag>
          ))}
        </div>
      )}
    </div>
  );
};

export default ModelCard;
