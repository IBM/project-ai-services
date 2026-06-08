import { ProductiveCard } from "@carbon/ibm-products";
import { Badge, Deploy, ArrowRight } from "@carbon/icons-react";
import styles from "./ServiceCard.module.scss";

export interface ServiceCardProps {
  id: string;
  title: string;
  description: string;
  isCertified?: boolean;
  onDeploy?: (id: string) => void;
  onLearnMore?: (id: string) => void;
}
const ServiceCard = ({
  id,
  title,
  description,
  isCertified,
  onDeploy,
  onLearnMore,
}: ServiceCardProps) => {
  const isPartOfDigitalAssistants =
    title === "Question and Answer" || title === "Find Similar Items";

  return (
    <div className={styles.cardWrapper}>
      <ProductiveCard
        primaryButtonIcon={ArrowRight}
        primaryButtonText={" "}
        secondaryButtonIcon={isPartOfDigitalAssistants ? undefined : Deploy}
        secondaryButtonText={
          isPartOfDigitalAssistants ? "Part of digital assistants" : "Deploy"
        }
        onPrimaryButtonClick={() => onLearnMore?.(id)}
        onClick={() => onLearnMore?.(id)}
        onSecondaryButtonClick={() => onDeploy?.(id)}
        clickZone="two"
        className={`${styles.productiveCard} ${
          isPartOfDigitalAssistants ? styles.digitalAssistantCard : ""
        }`}
      >
        {" "}
        <div className={styles.cardHeader}>
          <div className={styles.headerTitleBlock}>
            <h1 className={styles.cardTitle}>{title}</h1>
            {isCertified && (
              <span className={styles.certifiedBadge}>
                <Badge size={16} className={styles.badgeIcon} />
                <span className={styles.badgeName}>IBM certified</span>
              </span>
            )}
          </div>
        </div>
        <p className={styles.description}>{description}</p>
      </ProductiveCard>
    </div>
  );
};

export default ServiceCard;
