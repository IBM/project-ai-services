import { useEffect } from "react";
import { Button, Grid, Column, Layer, Link, SkeletonText } from "@carbon/react";
import { Deploy, Code, PlayOutline } from "@carbon/icons-react";
import styles from "../DigitalAssistants.module.scss";
import { useDeployStore } from "@/store/deploy.store";
import { fetchArchitectureDetails } from "@/api/digitalAssistants";
import type {
  AboutSection,
  AboutSectionValue,
} from "@/types/digitalAssistants";

interface AboutTabProps {
  onDeployClick: () => void;
}

export const AboutTab: React.FC<AboutTabProps> = ({ onDeployClick }) => {
  const architectureDetails = useDeployStore(
    (state) => state.architectureDetails,
  );
  const architectureDetailsLoading = useDeployStore(
    (state) => state.architectureDetailsLoading,
  );
  const architectureDetailsError = useDeployStore(
    (state) => state.architectureDetailsError,
  );
  const selectedArchitectureId = useDeployStore(
    (state) => state.selectedArchitectureId,
  );
  const setArchitectureDetails = useDeployStore(
    (state) => state.setArchitectureDetails,
  );
  const setArchitectureDetailsLoading = useDeployStore(
    (state) => state.setArchitectureDetailsLoading,
  );
  const setArchitectureDetailsError = useDeployStore(
    (state) => state.setArchitectureDetailsError,
  );

  useEffect(() => {
    const loadArchitectureDetails = async () => {
      if (!selectedArchitectureId) return;

      // If we already have data for this architecture, don't fetch again
      if (
        architectureDetails &&
        architectureDetails.id === selectedArchitectureId
      ) {
        return;
      }

      setArchitectureDetailsLoading(true);
      try {
        const data = await fetchArchitectureDetails(selectedArchitectureId);
        setArchitectureDetails(data);
      } catch (error) {
        const errorMessage =
          error instanceof Error
            ? error.message
            : "Failed to load architecture details";
        setArchitectureDetailsError(errorMessage);
      }
    };

    loadArchitectureDetails();
  }, [
    selectedArchitectureId,
    architectureDetails,
    setArchitectureDetails,
    setArchitectureDetailsLoading,
    setArchitectureDetailsError,
  ]);

  // Helper function to check if a value is AboutSectionValue type
  const isAboutSectionValue = (value: unknown): value is AboutSectionValue => {
    return typeof value === "object" && value !== null && "title" in value;
  };

  // Render Services Section
  const renderServicesSection = (section: AboutSection) => {
    if (!section.values || !Array.isArray(section.values)) return null;

    return (
      <Layer withBackground key="services">
        <section className={styles.aboutSection}>
          <div className={styles.sectionHeader}>
            <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
            <Button
              kind="primary"
              size="md"
              renderIcon={Deploy}
              onClick={onDeployClick}
            >
              Deploy
            </Button>
          </div>
          <ul className={styles.servicesList}>
            {section.values.map((value, idx) => (
              <li key={idx}>
                {typeof value === "string" ? value : value.title || ""}
              </li>
            ))}
          </ul>
        </section>
      </Layer>
    );
  };

  // Render Use Case Domains Section
  const renderUseCaseDomainsSection = (section: AboutSection) => {
    if (!section.sections || !Array.isArray(section.sections)) return null;

    return (
      <Layer withBackground key="use-case-domains">
        <section className={styles.aboutSection}>
          <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
          <Grid narrow className={styles.gridWithTopMargin}>
            {section.sections.map((domain, idx) => (
              <Column sm={4} md={4} lg={4} key={idx}>
                <h5 className={styles.useCaseDomain}>{domain.title}</h5>
                {domain.values && (
                  <ul className={styles.useCaseList}>
                    {domain.values.map((value, valueIdx) => (
                      <li key={valueIdx}>{value}</li>
                    ))}
                  </ul>
                )}
              </Column>
            ))}
          </Grid>
        </section>
      </Layer>
    );
  };

  // Render Resource Allocation Section
  const renderResourceAllocationSection = (section: AboutSection) => {
    if (!section.values || !Array.isArray(section.values)) return null;

    return (
      <Layer withBackground key="resource-allocation">
        <section className={styles.aboutSection}>
          <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
          <Grid narrow className={styles.gridWithTopMargin}>
            {section.values.map((item, idx) => {
              if (isAboutSectionValue(item)) {
                return (
                  <Column sm={4} md={4} lg={5} key={idx}>
                    <div className={styles.resourceItem}>
                      <span className={styles.resourceLabel}>{item.title}</span>
                      <span className={styles.resourceValue}>{item.value}</span>
                    </div>
                  </Column>
                );
              }
              return null;
            })}
          </Grid>
        </section>
      </Layer>
    );
  };

  // Render Code and Architecture Section
  const renderCodeArchitectureSection = (section: AboutSection) => {
    if (!section.sections || !Array.isArray(section.sections)) return null;

    const codeSection = section.sections.find((s) => s.url && s.ctaLabel);
    const imageSection = section.sections.find((s) => s.image);

    return (
      <Layer
        withBackground
        className={styles.sideBySideColumn}
        key="code-architecture"
      >
        <section className={styles.sideBySideSection}>
          <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
          {codeSection && (
            <Button
              kind="tertiary"
              size="sm"
              className={styles.codeButton}
              renderIcon={Code}
              onClick={() => window.open(codeSection.url, "_blank")}
            >
              {codeSection.ctaLabel || "View code"}
            </Button>
          )}
          {imageSection && imageSection.image && (
            <div className={styles.architectureDiagram}>
              <img
                src={imageSection.image.source}
                alt="Architecture Diagram"
                className={styles.diagramImage}
              />
            </div>
          )}
        </section>
      </Layer>
    );
  };

  // Render Demos and Prototypes Section
  const renderDemosSection = (section: AboutSection) => {
    if (!section.sections || !Array.isArray(section.sections)) return null;

    return (
      <Layer withBackground className={styles.sideBySideColumn} key="demos">
        <section className={styles.demosSection}>
          <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
          {section.sections.map((demo, idx) => (
            <div className={styles.demoCard} key={idx}>
              <img
                src={demo.image?.source || "images/ragDemoThumbnail.webp"}
                alt={demo.title || "Demo"}
                className={styles.demoImage}
              />
              <div className={styles.demoContent}>
                {demo.title && (
                  <h5 className={styles.demoTitle}>{demo.title}</h5>
                )}
                {demo.description && (
                  <p className={styles.demoDescription}>{demo.description}</p>
                )}
                {demo.url && demo.ctaLabel && (
                  <div className={styles.demoActions}>
                    <Link
                      href={demo.url}
                      target="_blank"
                      renderIcon={PlayOutline}
                    >
                      {demo.ctaLabel}
                    </Link>
                  </div>
                )}
              </div>
            </div>
          ))}
        </section>
      </Layer>
    );
  };

  // Dynamic section renderer
  const renderSection = (section: AboutSection) => {
    const title = section.title.toLowerCase();

    if (title.includes("service")) {
      return renderServicesSection(section);
    } else if (title.includes("use case") || title.includes("domain")) {
      return renderUseCaseDomainsSection(section);
    } else if (title.includes("resource") || title.includes("allocation")) {
      return renderResourceAllocationSection(section);
    } else if (title.includes("code") || title.includes("architecture")) {
      return renderCodeArchitectureSection(section);
    } else if (title.includes("demo") || title.includes("prototype")) {
      return renderDemosSection(section);
    }

    return null;
  };

  // Loading state
  if (architectureDetailsLoading) {
    return (
      <div className={styles.aboutContent}>
        <Layer withBackground>
          <section className={styles.aboutSection}>
            <SkeletonText heading />
            <SkeletonText paragraph lineCount={3} />
          </section>
        </Layer>
        <Layer withBackground>
          <section className={styles.aboutSection}>
            <SkeletonText heading />
            <SkeletonText paragraph lineCount={5} />
          </section>
        </Layer>
      </div>
    );
  }

  // Error state
  if (architectureDetailsError) {
    return (
      <div className={styles.aboutContent}>
        <Layer withBackground>
          <section className={styles.aboutSection}>
            <h4 className={styles.aboutSectionTitle}>
              Error loading architecture details
            </h4>
            <p>{architectureDetailsError}</p>
          </section>
        </Layer>
      </div>
    );
  }

  // No data state
  if (!architectureDetails || !architectureDetails.about) {
    return (
      <div className={styles.aboutContent}>
        <Layer withBackground>
          <section className={styles.aboutSection}>
            <h4 className={styles.aboutSectionTitle}>
              No architecture details available
            </h4>
            <p>Please select an architecture to view details.</p>
          </section>
        </Layer>
      </div>
    );
  }

  // Separate sections for side-by-side layout
  const codeArchSection = architectureDetails.about.find(
    (s) =>
      s.title.toLowerCase().includes("code") ||
      s.title.toLowerCase().includes("architecture"),
  );
  const demosSection = architectureDetails.about.find(
    (s) =>
      s.title.toLowerCase().includes("demo") ||
      s.title.toLowerCase().includes("prototype"),
  );
  const otherSections = architectureDetails.about.filter(
    (s) => s !== codeArchSection && s !== demosSection,
  );

  return (
    <div className={styles.aboutContent}>
      {/* Render other sections */}
      {otherSections.map((section, index) => (
        <div key={index}>{renderSection(section)}</div>
      ))}

      {/* Code and Architecture + Demos Section (Side by Side) */}
      {(codeArchSection || demosSection) && (
        <div className={styles.sideBySideGrid}>
          {codeArchSection && renderCodeArchitectureSection(codeArchSection)}
          {demosSection && renderDemosSection(demosSection)}
        </div>
      )}
    </div>
  );
};
