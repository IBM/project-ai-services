import { Button, Tag, Tooltip } from "@carbon/react";
import { ArrowRight, Badge, Deploy } from "@carbon/icons-react";
import styles from "./ArchitectureCard.module.scss";

export interface ArchitectureCardProps {
  id: string;
  title: string;
  description: string;
  tags: string[];
  isCertified?: boolean;
  tagsHeading?: string;
  onDeploy?: (id: string) => void;
  onLearnMore?: (id: string) => void;
}

const ArchitectureCard = ({
  id,
  title,
  description,
  tags,
  isCertified,
  tagsHeading = "Services",
  onDeploy,
  onLearnMore,
}: ArchitectureCardProps) => {
  const maxVisibleTags = 4;
  const visibleTags = tags.slice(0, maxVisibleTags);
  const remainingCount = Math.max(0, tags.length - maxVisibleTags);

  return (
    <div className={styles.card}>
      <div className={styles.cardHeader}>
        <h3 className={styles.cardTitle}>{title}</h3>
        <div className={styles.headerRight}>
          {isCertified && (
            <Tooltip align="top" label="IBM certified">
              <button
                className={styles.certifiedBadge}
                type="button"
                aria-label="IBM certified"
              >
                <Badge size={16} />
              </button>
            </Tooltip>
          )}
        </div>
      </div>

      <p className={styles.cardDescription}>{description}</p>

      {tags.length > 0 && (
        <div className={styles.tagsSection}>
          <h4 className={styles.tagsHeading}>{tagsHeading}</h4>
          <div className={styles.tags}>
            {visibleTags.map((tag) => (
              <Tag key={tag} type="blue" size="md">
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
      )}

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

export default ArchitectureCard;
