import type { ReactNode } from "react";
import { SidePanel } from "@carbon/ibm-products";
import { SkeletonText, SkeletonPlaceholder, Button } from "@carbon/react";
import { Badge } from "@carbon/icons-react";
import { formatVersion } from "@/utils/string";
import type { CatalogAboutSection } from "@/types/api.types";
import styles from "./CatalogDetailPanel.module.scss";

export interface CatalogDetailPanelProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  certifiedBy?: string;
  license?: string;
  badges?: ReactNode;
  about?: CatalogAboutSection[];
  isLoading?: boolean;
  error?: string | null;
  children?: ReactNode;
}

// Renders a single field with title and value
const renderField = (title: string, value: string) => (
  <div className={styles.demoItem}>
    <div className={styles.fieldLabel}>{title}</div>
    <div className={styles.fieldValue}>{value}</div>
  </div>
);

// Renders a list of values as bullet points
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

// Renders nested subsections with title/value pairs
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

// Dynamically renders a section based on its structure
const renderSection = (section: CatalogAboutSection, sectionIndex: number) => {
  if (section.value) {
    const displayValue =
      section.title.toLowerCase() === "version"
        ? formatVersion(section.value)
        : section.value;
    return renderField(section.title, displayValue);
  }

  if (section.values && Array.isArray(section.values)) {
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

    return renderValueList(section.title, section.values as string[]);
  }

  if (section.url && section.ctaLabel) {
    return (
      <div key={sectionIndex} className={styles.assetField}>
        <div className={styles.fieldLabel}>{section.title}</div>
        <Button
          kind="tertiary"
          size="md"
          className={styles.sourceButton}
          href={section.url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {section.ctaLabel}
        </Button>
      </div>
    );
  }

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

// Renders a top-level about section with its subsections
const renderAboutSection = (
  aboutSection: CatalogAboutSection,
  index: number,
) => {
  if (!aboutSection.sections || aboutSection.sections.length === 0) {
    return null;
  }

  return (
    <div key={index}>
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{aboutSection.title}</h2>

        {aboutSection.sections.some((s) => s.value) && (
          <div className={styles.demoGrid}>
            {aboutSection.sections
              .filter((s) => s.value)
              .map((section, idx) => (
                <div key={idx}>{renderSection(section, idx)}</div>
              ))}
          </div>
        )}

        {aboutSection.sections.some((s) => s.values || s.url) && (
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

// Unified slide-in detail panel for catalog items (Services, Models, Digital Assistants)
const CatalogDetailPanel = ({
  open,
  onClose,
  title,
  description,
  certifiedBy,
  license,
  badges,
  about,
  isLoading = false,
  error = null,
  children,
}: CatalogDetailPanelProps) => {
  if (!open) return null;

  const hasBadgesOrTags = Boolean(badges || certifiedBy || license);

  return (
    <SidePanel
      open={open}
      onRequestClose={onClose}
      title={title}
      includeOverlay
      placement="right"
      size="lg"
      className={styles.sidePanel}
    >
      <div className={styles.content}>
        {isLoading && (
          <div className={styles.skeletonContainer}>
            <SkeletonText
              paragraph
              lineCount={3}
              className={styles.skeletonDescription}
            />

            <div className={styles.skeletonTags}>
              <SkeletonPlaceholder className={styles.skeletonTag} />
              <SkeletonPlaceholder className={styles.skeletonTag} />
            </div>

            <div className={styles.divider} />

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

            <div className={styles.skeletonSection}>
              <SkeletonText heading className={styles.skeletonSectionTitle} />
              <SkeletonText paragraph lineCount={4} />
            </div>
          </div>
        )}

        {error && <div className={styles.errorMessage}>{error}</div>}

        {!isLoading && !error && (
          <>
            {description && <p className={styles.description}>{description}</p>}

            {hasBadgesOrTags && (
              <div className={styles.tagContainer}>
                {badges}
                {certifiedBy && (
                  <div className={styles.certifiedTag}>
                    <Badge size={16} className={styles.checkIcon} />
                    {certifiedBy} certified
                  </div>
                )}
                {license && (
                  <div className={styles.licenseTag}>License: {license}</div>
                )}
              </div>
            )}

            {(hasBadgesOrTags || description) && (
              <div className={styles.divider} />
            )}

            {/* Dynamically rendered about sections */}
            {about &&
              about.length > 0 &&
              about.map((aboutSection, index) =>
                renderAboutSection(aboutSection, index),
              )}

            {/* Domain-specific extra content (e.g. Supported Providers) */}
            {children}
          </>
        )}
      </div>
    </SidePanel>
  );
};

export default CatalogDetailPanel;
