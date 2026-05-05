import { Button, Tag, Tooltip } from "@carbon/react";
import { ArrowRight, Badge, Deploy } from "@carbon/icons-react";
import styles from "./ServiceCard.module.scss";

export interface ServiceCardProps {
  id: string;
  title: string;
  description: string;
  tags: string[];
  category?: string;
  isCertified?: boolean;
  onDeploy?: (id: string) => void;
  onLearnMore?: (id: string) => void;
  onExplore?: (id: string) => void;
}

const ServiceCard = ({
  id,
  title,
  description,
  tags,
  category,
  isCertified,
  onDeploy,
  onLearnMore,
}: ServiceCardProps) => {
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
        <p className={styles.tagsHeading}>Architectures</p>
        <div className={styles.tags}>
          {tags.map((tag, index) => (
            <Tag key={index} type="gray" size="sm">
              {tag}
            </Tag>
          ))}
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

export default ServiceCard;
