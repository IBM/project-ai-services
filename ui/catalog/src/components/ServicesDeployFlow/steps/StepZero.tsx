import {
  Grid,
  Column,
  InlineLoading,
  InlineNotification,
  ClickableTile,
} from "@carbon/react";
import { Badge, Checkmark } from "@carbon/icons-react";
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
          <div className={styles.loadingContainer}>
            <InlineLoading description="Loading services..." />
          </div>
        ) : error ? (
          <InlineNotification
            kind="error"
            title="Error loading services"
            subtitle={error}
            lowContrast
            hideCloseButton
          />
        ) : (
          <Grid narrow className={styles.serviceSelectionGrid}>
            {standaloneServices.map((service) => (
              <Column key={service.id} sm={4} md={4} lg={7}>
                <ClickableTile
                  id={`service-tile-${service.id}`}
                  className={`${styles.serviceTile} ${
                    selectedServiceId === service.id ? styles.selected : ""
                  }`}
                  onClick={() => onServiceSelect(service.id)}
                >
                  {selectedServiceId === service.id && (
                    <div className={styles.selectedIndicator}>
                      <Checkmark size={20} />
                    </div>
                  )}
                  <div className={styles.serviceTileContent}>
                    <h3 className={styles.serviceTileName}>{service.name}</h3>
                    <div className={styles.certifiedBadge}>
                      {/* <Checkmark size={16} /> */}
                      <span className={styles.certifiedBadge}>
                        <Badge size={16} className={styles.badgeIcon} />
                        <span className={styles.badgeName}>IBM certified</span>
                      </span>
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

// Made with Bob
