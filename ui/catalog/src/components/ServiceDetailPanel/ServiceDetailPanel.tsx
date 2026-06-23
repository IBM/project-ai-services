import { useState, useEffect } from "react";
import { SidePanel } from "@carbon/ibm-products";
import { Button, SkeletonText, SkeletonPlaceholder } from "@carbon/react";
import { Badge } from "@carbon/icons-react";
import {
  fetchServiceDetails,
  type AboutSection,
} from "@/services/serviceDetails.api";
import { transformServiceDetails } from "@/utils/serviceDetailsTransform";
import { useServiceDetailsStore } from "@/store/serviceDetails.store";
import styles from "./ServiceDetailPanel.module.scss";

export interface ServiceDetailData {
  id: string;
  title: string;
  description: string;
  certifiedBy?: string;
  tags?: string[];
  standalone?: boolean;
  about: AboutSection[];
}

export interface ServiceDetailPanelProps {
  open: boolean;
  onClose: () => void;
  serviceId: string | null;
}

/**
 * Renders a single field with title and value
 */
const renderField = (title: string, value: string) => (
  <div className={styles.demoItem}>
    <div className={styles.fieldLabel}>{title}</div>
    <div className={styles.fieldValue}>{value}</div>
  </div>
);

/**
 * Renders a list of values as bullet points
 */
const renderValueList = (title: string, values: string[]) => (
  <div className={styles.column}>
    <div className={styles.columnLabel}>{title}</div>
    <ul className={styles.bulletList}>
      {values.map((value, index) => (
        <li key={index}>{value}</li>
      ))}
    </ul>
  </div>
);

/**
 * Renders nested subsections with title/value pairs
 */
const renderNestedFields = (
  values: Array<{ title: string; value: string }>,
) => (
  <div className={styles.twoColumns}>
    {values.map((item, index) => (
      <div key={index} className={styles.column}>
        <ul className={styles.dashList}>
          <li>{`${item.title}: ${item.value}`}</li>
        </ul>
      </div>
    ))}
  </div>
);

/**
 * Dynamically renders a section based on its structure
 */
const renderSection = (section: AboutSection, sectionIndex: number) => {
  // Handle single value field
  if (section.value) {
    return renderField(section.title, section.value);
  }

  // Handle array of values
  if (section.values && Array.isArray(section.values)) {
    // Check if values are objects with title/value structure
    const firstValue = section.values[0];
    if (
      typeof firstValue === "object" &&
      firstValue !== null &&
      "title" in firstValue &&
      "value" in firstValue
    ) {
      return (
        <div key={sectionIndex}>
          <div className={styles.columnLabel}>{section.title}</div>
          {renderNestedFields(
            section.values as Array<{ title: string; value: string }>,
          )}
        </div>
      );
    }

    // Regular string array
    return renderValueList(section.title, section.values as string[]);
  }

  // Handle URL/CTA button
  if (section.url && section.ctaLabel) {
    return (
      <div key={sectionIndex} className={styles.assetField}>
        <div className={styles.fieldLabel}>{section.title}</div>
        <Button
          kind="tertiary"
          size="md"
          className={styles.sourceButton}
          onClick={() => window.open(section.url, "_blank")}
        >
          {section.ctaLabel}
        </Button>
      </div>
    );
  }

  // Handle URL without CTA (render as link)
  if (section.url) {
    return (
      <div key={sectionIndex} className={styles.assetField}>
        <div className={styles.fieldLabel}>{section.title}</div>
        <a
          href={section.url}
          className={styles.infoLink}
          target="_blank"
          rel="noopener noreferrer"
        >
          {section.url}
        </a>
      </div>
    );
  }

  return null;
};

/**
 * Renders a top-level about section with its subsections
 */
const renderAboutSection = (aboutSection: AboutSection, index: number) => {
  if (!aboutSection.sections || aboutSection.sections.length === 0) {
    return null;
  }

  return (
    <div key={index}>
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{aboutSection.title}</h2>

        {/* Check if this is a grid-based section (Service details, etc.) */}
        {aboutSection.sections.some((s) => s.value) && (
          <div className={styles.demoGrid}>
            {aboutSection.sections
              .filter((s) => s.value)
              .map((section, idx) => (
                <div key={idx}>{renderSection(section, idx)}</div>
              ))}
          </div>
        )}

        {/* Render sections with values arrays */}
        {aboutSection.sections.some((s) => s.values) && (
          <div
            className={
              aboutSection.sections.length === 2
                ? styles.twoColumns
                : styles.threeColumns
            }
          >
            {aboutSection.sections
              .filter((s) => s.values || s.url)
              .map((section, idx) => (
                <div key={idx}>{renderSection(section, idx)}</div>
              ))}
          </div>
        )}
      </section>

      <div className={styles.divider} />
    </div>
  );
};

const ServiceDetailPanel = ({
  open,
  onClose,
  serviceId,
}: ServiceDetailPanelProps) => {
  const [service, setService] = useState<ServiceDetailData | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { getServiceDetails, setServiceDetails } = useServiceDetailsStore();

  useEffect(() => {
    if (!open || !serviceId) {
      return;
    }

    const loadServiceDetails = async () => {
      // Check cache first
      const cachedService = getServiceDetails(serviceId);
      if (cachedService) {
        setService(cachedService);
        return;
      }

      // If not in cache, fetch from API
      setIsLoading(true);
      setError(null);

      try {
        const serviceData = await fetchServiceDetails(serviceId);
        const transformedData = transformServiceDetails(serviceData);

        // Store in cache
        setServiceDetails(serviceId, transformedData);
        setService(transformedData);
      } catch (err) {
        console.error("Failed to fetch service details:", err);
        setError("Failed to load service details. Please try again.");
      } finally {
        setIsLoading(false);
      }
    };

    loadServiceDetails();
  }, [open, serviceId, getServiceDetails, setServiceDetails]);

  if (!open) return null;

  return (
    <SidePanel
      open={open}
      onRequestClose={onClose}
      title={service?.title || "Service Details"}
      includeOverlay
      placement="right"
      size="lg"
      className={styles.sidePanel}
    >
      <div className={styles.content}>
        {isLoading && (
          <div className={styles.skeletonContainer}>
            {/* Description skeleton */}
            <SkeletonText
              paragraph
              lineCount={3}
              className={styles.skeletonDescription}
            />

            {/* Tags skeleton */}
            <div className={styles.skeletonTags}>
              <SkeletonPlaceholder className={styles.skeletonTag} />
              <SkeletonPlaceholder className={styles.skeletonTag} />
            </div>

            <div className={styles.divider} />

            {/* Section skeleton */}
            <div className={styles.skeletonSection}>
              <SkeletonText heading className={styles.skeletonSectionTitle} />
              <div className={styles.skeletonGrid}>
                <div>
                  <SkeletonText lineCount={1} width="60%" />
                  <SkeletonText lineCount={1} width="80%" />
                </div>
                <div>
                  <SkeletonText lineCount={1} width="60%" />
                  <SkeletonText lineCount={1} width="80%" />
                </div>
              </div>
            </div>

            <div className={styles.divider} />

            {/* Another section skeleton */}
            <div className={styles.skeletonSection}>
              <SkeletonText heading className={styles.skeletonSectionTitle} />
              <SkeletonText paragraph lineCount={4} />
            </div>

            <div className={styles.divider} />

            {/* Another section skeleton */}
            <div className={styles.skeletonSection}>
              <SkeletonText heading className={styles.skeletonSectionTitle} />
              <SkeletonText paragraph lineCount={3} />
            </div>
          </div>
        )}

        {error && <div className={styles.errorMessage}>{error}</div>}

        {!isLoading && !error && service && (
          <>
            <p className={styles.description}>{service.description}</p>

            <div className={styles.tagContainer}>
              {service.certifiedBy && (
                <div className={styles.certifiedTag}>
                  <Badge size={16} className={styles.checkIcon} />
                  {service.certifiedBy} certified
                </div>
              )}
            </div>

            <div className={styles.divider} />

            {/* Dynamically render all about sections */}
            {service.about.map((aboutSection, index) =>
              renderAboutSection(aboutSection, index),
            )}
          </>
        )}
      </div>
    </SidePanel>
  );
};

export default ServiceDetailPanel;

// Made with Bob
