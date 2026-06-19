import { useEffect } from "react";
import { Button, Grid, Column, Layer, Link, SkeletonText } from "@carbon/react";
import { Deploy, Code, PlayOutline } from "@carbon/icons-react";
import styles from "../DigitalAssistants.module.scss";
import { useDeployStore } from "@/store/deploy.store";
import { fetchArchitectureDetails } from "@/api/digitalAssistants";
import { dedupe } from "@/utils/requestManager";
import type { AboutSection } from "@/types/digitalAssistants";

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
  const isArchitectureDetailsStale = useDeployStore(
    (state) => state.isArchitectureDetailsStale,
  );

  useEffect(() => {
    const loadArchitectureDetails = async () => {
      if (!selectedArchitectureId) return;

      // Check if we have data for this architecture
      const hasCorrectData =
        architectureDetails &&
        architectureDetails.id === selectedArchitectureId;

      // Check if cache is stale
      const isStale = isArchitectureDetailsStale();

      // Fetch if we don't have data for this architecture or cache is stale
      // dedupe() handles preventing duplicate in-flight requests
      if (!hasCorrectData || isStale) {
        setArchitectureDetailsLoading(true);
        try {
          const requestKey = `architectureDetails:${selectedArchitectureId}`;
          const data = await dedupe(requestKey, () =>
            fetchArchitectureDetails(selectedArchitectureId),
          );
          setArchitectureDetails(data);
        } catch (error) {
          const errorMessage =
            error instanceof Error
              ? error.message
              : "Failed to load architecture details";
          setArchitectureDetailsError(errorMessage);
        }
      }
    };

    loadArchitectureDetails();
  }, [
    selectedArchitectureId,
    architectureDetails,
    setArchitectureDetails,
    setArchitectureDetailsLoading,
    setArchitectureDetailsError,
    isArchitectureDetailsStale,
  ]);

  // Generic section renderer - renders sections based on their structure
  const renderSection = (section: AboutSection, index: number) => {
    // Services section - has values array
    if (section.values && Array.isArray(section.values)) {
      return (
        <Layer withBackground key={index}>
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
    }

    // Sections with subsections
    if (section.sections && Array.isArray(section.sections)) {
      // Check if it's a demo/prototype structure FIRST (has description + url + ctaLabel)
      // This must be checked before code structure because demos also have url + ctaLabel
      const hasDemoStructure = section.sections.some(
        (s) => s.description && s.url && s.ctaLabel,
      );

      if (hasDemoStructure) {
        return (
          <Layer withBackground className={styles.sideBySideColumn} key={index}>
            <section className={styles.demosSection}>
              <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
              {section.sections.map((demo, idx) => (
                <div className={styles.demoCard} key={idx}>
                  <div className={styles.demoContent}>
                    {demo.title && (
                      <h5 className={styles.demoTitle}>{demo.title}</h5>
                    )}
                    {demo.description && (
                      <p className={styles.demoDescription}>
                        {demo.description}
                      </p>
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
      }

      // Check if it has code/architecture structure (url + ctaLabel OR image)
      const hasCodeStructure = section.sections.some(
        (s) => s.url && s.ctaLabel,
      );
      const hasImageStructure = section.sections.some((s) => s.image);

      if (hasCodeStructure || hasImageStructure) {
        const codeSection = section.sections.find((s) => s.url && s.ctaLabel);
        const imageSection = section.sections.find((s) => s.image);

        return (
          <Layer withBackground className={styles.sideBySideColumn} key={index}>
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
                  {/* TODO: Update image here */}
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
      }

      // Check if subsections have title/value pairs (like resource allocation)
      const hasResourceStructure = section.sections.some(
        (s) => s.title && s.value && !s.values,
      );

      if (hasResourceStructure) {
        return (
          <Layer withBackground key={index}>
            <section className={styles.aboutSection}>
              <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
              <Grid narrow className={styles.gridWithTopMargin}>
                {section.sections.map((item, idx) => (
                  <Column sm={4} md={4} lg={5} key={idx}>
                    <div className={styles.resourceItem}>
                      <span className={styles.resourceLabel}>{item.title}</span>
                      <span className={styles.resourceValue}>{item.value}</span>
                    </div>
                  </Column>
                ))}
              </Grid>
            </section>
          </Layer>
        );
      }

      // Default subsections rendering (use case domains with values arrays)
      return (
        <Layer withBackground key={index}>
          <section className={styles.aboutSection}>
            <h4 className={styles.aboutSectionTitle}>{section.title}</h4>
            <Grid narrow className={styles.gridWithTopMargin}>
              {section.sections.map((subsection, idx) => (
                <Column sm={4} md={4} lg={4} key={idx}>
                  <h5 className={styles.useCaseDomain}>{subsection.title}</h5>
                  {subsection.values && (
                    <ul className={styles.useCaseList}>
                      {subsection.values.map((value, valueIdx) => (
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

  // Separate code/architecture and demos sections for side-by-side layout
  // Demos section has description + url + ctaLabel
  const demosSection = architectureDetails.about.find((s) =>
    s.sections?.some((item) => item.description && item.url && item.ctaLabel),
  );

  // Code/arch section has url with ctaLabel OR image (but not description)
  const codeArchSection = architectureDetails.about.find(
    (s) =>
      s !== demosSection &&
      s.sections?.some((item) => (item.url && item.ctaLabel) || item.image),
  );

  // Get other sections (excluding code/arch and demos)
  const otherSections = architectureDetails.about.filter(
    (s) => s !== codeArchSection && s !== demosSection,
  );

  return (
    <div className={styles.aboutContent}>
      {/* Render other sections first */}
      {otherSections.map((section, index) => (
        <div key={index}>{renderSection(section, index)}</div>
      ))}

      {/* Code and Architecture + Demos Section (Side by Side) */}
      {(codeArchSection || demosSection) && (
        <div className={styles.sideBySideGrid}>
          {codeArchSection &&
            renderSection(
              codeArchSection,
              architectureDetails.about.indexOf(codeArchSection),
            )}
          {demosSection &&
            renderSection(
              demosSection,
              architectureDetails.about.indexOf(demosSection),
            )}
        </div>
      )}
    </div>
  );
};
