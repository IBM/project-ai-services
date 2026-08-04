import { SkeletonText, SkeletonPlaceholder, Tile } from "@carbon/react";
import styles from "./ServiceCard.module.scss";

const ServiceCardSkeleton = () => {
  return (
    <div className={styles.cardWrapper}>
      <Tile className={`${styles.productiveCard} ${styles.skeletonCard}`}>
        <div className={styles.cardHeader}>
          <div className={styles.headerTitleBlock}>
            <SkeletonText heading width="60%" />
          </div>
        </div>
        <div className={styles.description}>
          <SkeletonText paragraph lineCount={2} width="100%" />
        </div>
        <div className={styles.skeletonActions}>
          <SkeletonPlaceholder style={{ width: "100%", height: "48px" }} />
        </div>
      </Tile>
    </div>
  );
};

export default ServiceCardSkeleton;

// Made with Bob
