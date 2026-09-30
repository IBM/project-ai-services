import {
  Grid,
  Column,
  InlineNotification,
  ClickableTile,
  SkeletonText,
  SkeletonPlaceholder,
} from "@carbon/react";
import { Badge, CheckmarkFilled } from "@carbon/icons-react";
import { useServices } from "@/hooks/useServices";
import styles from "../ServicesDeployFlow.module.scss";

interface StepZeroProps {
  title: string;
  selectedServiceId: string | null;
  onServiceSelect: (serviceId: string) => void;
  isOpen?: boolean;
}

export const StepZero: React.FC<StepZeroProps> = ({
  title,
  selectedServiceId,
  onServiceSelect,
  isOpen = true,
}) => {
  // Use cached services from Zustand store
  // Only auto-fetch when StepZero is actually visible (isOpen = true)
  const { services, isLoading, error } = useServices(isOpen);

  // Filter services to show only standalone services and sort alphabetically by name
  const standaloneServices = services
    .filter((service) => service.standalone === true)
    .sort((a, b) => a.name.localeCompare(b.name));

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      <div className={styles.formSection}>
        {isLoading ? (
          <Grid narrow fullWidth className={styles.serviceSelectionGrid}>
            {[0, 1, 2, 3].map((i) => (
              <Column
                key={i}
                sm={4}
                md={4}
                lg={8}
                className={styles.tileColumn}
              >
                <div className={styles.serviceTile}>
                  <SkeletonText width="60%" />
                  <SkeletonPlaceholder className={styles.serviceTileSkeleton} />
                </div>
              </Column>
            ))}
          </Grid>
        ) : error ? (
          <InlineNotification
            kind="error"
            title="Error loading services"
            subtitle={error}
            lowContrast
            hideCloseButton
          />
        ) : (
          <Grid narrow fullWidth className={styles.serviceSelectionGrid}>
            {standaloneServices.map((service) => (
              <Column
                key={service.id}
                sm={4}
                md={4}
                lg={8}
                className={styles.tileColumn}
              >
                <ClickableTile
                  id={`service-tile-${service.id}`}
                  className={`${styles.serviceTile} ${
                    selectedServiceId === service.id ? styles.selected : ""
                  }`}
                  onClick={() => onServiceSelect(service.id)}
                >
                  {selectedServiceId === service.id && (
                    <CheckmarkFilled
                      size={16}
                      className={styles.selectedIndicator}
                    />
                  )}
                  <div className={styles.serviceTileContent}>
                    <h3 className={styles.serviceTileName}>{service.name}</h3>
                    <div className={styles.certifiedBadge}>
                      <Badge size={16} />
                      <span>IBM certified</span>
                    </div>
                    <p className={styles.serviceTileDescription}>
                      {service.description}
                    </p>
                  </div>
                </ClickableTile>
              </Column>
            ))}
          </Grid>
        )}
      </div>
    </>
  );
};
