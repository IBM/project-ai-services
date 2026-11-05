import { SkeletonText, SkeletonPlaceholder, Tile } from "@carbon/react";
import styles from "./ModelCard.module.scss";

const ModelCardSkeleton = () => {
  return (
    <div className={styles.cardWrapper}>
      <Tile className={styles.skeletonCard}>
        <div className={styles.cardHeader}>
          <SkeletonText width="30%" />
          <SkeletonText heading width="65%" />
          <SkeletonText width="25%" />
        </div>
        <div className={styles.description}>
          <SkeletonText paragraph lineCount={3} width="100%" />
        </div>
        <div className={styles.providerNames}>
          <SkeletonText width="60%" />
        </div>
        <SkeletonPlaceholder className={styles.skeletonFooter} />
      </Tile>
    </div>
  );
};

export default ModelCardSkeleton;
