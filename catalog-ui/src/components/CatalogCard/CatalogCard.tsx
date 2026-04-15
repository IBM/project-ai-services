import { Button, Tag, Tooltip } from "@carbon/react";
import { ArrowRight, Badge, Deploy } from "@carbon/icons-react";
import styles from "./CatalogCard.module.scss";

export interface CatalogCardProps {
  id: string;
  title: string;
  description: string;
  tags: string[];
  category?: string;
  isCertified?: boolean;
  tagsHeading?: string;
  onDeploy?: (id: string) => void;
  onLearnMore?: (id: string) => void;
  onExplore?: (id: string) => void;
}

const CatalogCard = ({
  id,
  title,
  description,
  tags,
  category,
  isCertified,
  tagsHeading = "Tags",
  onDeploy,
  onLearnMore,
  onExplore,
}: CatalogCardProps) => {
  const maxVisibleTags = 4;
  const visibleTags = tags.slice(0, maxVisibleTags);
  const remainingCount = tags.length - maxVisibleTags;

  return (
    <div className={styles.card}>
      <div className={styles.cardHeader}>
        <h3 className={styles.cardTitle}>{title}</h3>
        <div className={styles.headerRight}>
          {category && <span className={styles.category}>{category}</span>}
          {isCertified && (
            <Tooltip align="top" label="IBM certified">
              <button className={styles.certifiedBadge} type="button">
                <Badge size={16} className={styles.badgeIcon} />
              </button>
            </Tooltip>
          )}
        </div>
      </div>

      <p className={styles.cardDescription}>{description}</p>

      <div className={styles.tagsSection}>
        <h4 className={styles.tagsHeading}>{tagsHeading}</h4>
        <div className={styles.tags}>
          {visibleTags.map((tag, index) => (
            <Tag key={index} type="blue" size="md">
              {tag}
            </Tag>
          ))}
          {remainingCount > 0 && (
            <Tag type="blue" size="md">
              +{remainingCount}
            </Tag>
          )}
        </div>
      </div>

      <div className={styles.cardActions}>
        {onDeploy && (
          <Button
            kind="tertiary"
            size="sm"
            renderIcon={Deploy}
            onClick={() => onDeploy(id)}
          >
            Deploy
          </Button>
        )}
        {onExplore && (
          <Button
            kind="tertiary"
            size="sm"
            renderIcon={Deploy}
            onClick={() => onExplore(id)}
          >
            Explore
          </Button>
        )}
        {onLearnMore && (
          <Button
            kind="tertiary"
            size="sm"
            renderIcon={ArrowRight}
            onClick={() => onLearnMore(id)}
          >
            Learn more
          </Button>
        )}
      </div>
    </div>
  );
};

export default CatalogCard;
