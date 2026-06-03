import { SidePanel } from "@carbon/ibm-products";
import { Button, Tag, Link, Layer } from "@carbon/react";
import { Badge, PlayOutline, DataViewAlt } from "@carbon/icons-react";
import { useUseCases } from "@/hooks/useUseCases";
import { renderParagraphs } from "@/utils/string.tsx";
import styles from "./SolutionDetailPanel.module.scss";

interface SolutionDetailPanelProps {
  open: boolean;
  onClose: () => void;
  solutionId: string | null;
}

const SolutionDetailPanel = ({
  open,
  onClose,
  solutionId,
}: SolutionDetailPanelProps) => {
  const { useCases, isLoading } = useUseCases();

  const solutionData = useCases.find((uc) => uc.id === solutionId);

  if (isLoading) {
    return (
      <SidePanel
        open={open}
        onRequestClose={onClose}
        title="Loading..."
        size="md"
        includeOverlay
      >
        <div className={styles.panelContent}>Loading use case details...</div>
      </SidePanel>
    );
  }

  if (!solutionData) {
    return (
      <SidePanel
        open={open}
        onRequestClose={onClose}
        title="Not Found"
        size="md"
        includeOverlay
      >
        <div className={styles.panelContent}>Use case not found.</div>
      </SidePanel>
    );
  }

  const isCertified = solutionData.creator === "IBM";
  const allStories = [
    ...(solutionData.clientStories || []),
    ...(solutionData.partnerStories || []),
  ];

  return (
    <SidePanel
      open={open}
      onRequestClose={onClose}
      title={solutionData.title}
      size="lg"
      includeOverlay
    >
      <div className={styles.panelContent}>
        <div className={styles.header}>
          <p className={styles.description}>{solutionData.description}</p>
          <div className={styles.tags}>
            <Tag type="outline" size="sm">
              by IBM Power
            </Tag>
            {solutionData.architectures.map((arch, index) => (
              <Tag type="gray" size="sm" key={index}>
                {arch}
              </Tag>
            ))}
            {isCertified && (
              <div className={styles.certifiedTag}>
                <Badge size={16} className={styles.badgeIcon} />
                <span>IBM certified</span>
              </div>
            )}
          </div>
        </div>

        <div className={styles.domainSection}>
          <h4 className={styles.domainSectionTitle}>Domain</h4>
          <ul className={styles.domainList}>
            <li>{solutionData.domain}</li>
          </ul>
        </div>

        {solutionData.demo && (
          <div className={styles.demoSection}>
            <h4 className={styles.demoSectionTitle}>Demos and prototypes</h4>
            <Layer withBackground className={styles.demoCard}>
              <h5 className={styles.demoCardTitle}>
                {solutionData.demoTitle || solutionData.title}
              </h5>
              <p className={styles.demoCardDescription}>
                {solutionData.demoDescription || solutionData.description}
              </p>
              <div className={styles.demoCardFooter}>
                <span className={styles.demoDuration}>
                  {solutionData.demoDuration &&
                    `(${solutionData.demoDuration})`}
                </span>
                <Link
                  href={solutionData.demo}
                  target="_blank"
                  rel="noopener noreferrer"
                  renderIcon={PlayOutline}
                  className={styles.demoLink}
                >
                  Watch
                </Link>
              </div>
            </Layer>
          </div>
        )}

        {(solutionData.featuredArticle ||
          allStories.length > 0 ||
          solutionData.testimonial) && (
          <div className={styles.storiesSection}>
            <h4 className={styles.storiesSectionTitle}>
              Client stories and testimonials
            </h4>

            {solutionData.featuredArticle && (
              <div className={styles.storiesContainer}>
                {solutionData.featuredArticle.company && (
                  <p className={styles.storyCompany}>
                    {solutionData.featuredArticle.company}
                  </p>
                )}
                <Layer withBackground className={styles.storyCard}>
                  <div className={styles.storyCardContent}>
                    <div className={styles.storyCardImage}>
                      <img
                        src={solutionData.featuredArticle.imagePath}
                        alt={solutionData.featuredArticle.title}
                        className={styles.storyImage}
                      />
                    </div>
                    <div className={styles.storyCardText}>
                      <h5 className={styles.storyCardTitle}>
                        {solutionData.featuredArticle.title}
                      </h5>
                      <Link
                        href={solutionData.featuredArticle.articleUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        renderIcon={DataViewAlt}
                        className={styles.storyLink}
                      >
                        Read
                      </Link>
                    </div>
                  </div>
                </Layer>
                {solutionData.featuredArticle.description && (
                  <p className={styles.storyDescription}>
                    {renderParagraphs(solutionData.featuredArticle.description)}
                  </p>
                )}
              </div>
            )}

            {!solutionData.featuredArticle && allStories.length > 0 && (
              <div className={styles.storiesContainer}>
                {allStories.map((story, index) => (
                  <div key={index} className={styles.story}>
                    <p className={styles.storyCompany}>{story.company}</p>
                    {story.url && (
                      <Button
                        kind="tertiary"
                        size="sm"
                        onClick={() =>
                          window.open(
                            story.url,
                            "_blank",
                            "noopener,noreferrer",
                          )
                        }
                        className={styles.demoButton}
                      >
                        Read online
                      </Button>
                    )}
                    {story.description && (
                      <p className={styles.storyDescription}>
                        {renderParagraphs(story.description)}
                      </p>
                    )}
                  </div>
                ))}
              </div>
            )}

            {solutionData.featuredArticle &&
              solutionData.partnerStories &&
              solutionData.partnerStories.length > 0 && (
                <div className={styles.testimonialSection}>
                  <h4 className={styles.testimonialCompany}>
                    Partner testimonial
                  </h4>
                  {solutionData.partnerStories.map((story, index) => (
                    <div key={index} className={styles.testimonialContent}>
                      <p className={styles.testimonialQuote}>
                        &ldquo;{story.description}&rdquo;
                      </p>
                      {story.testimonial && (
                        <p className={styles.testimonialAttribution}>
                          – {story.testimonial}
                        </p>
                      )}
                    </div>
                  ))}
                </div>
              )}

            {solutionData.testimonial &&
              solutionData.testimonial.company &&
              solutionData.testimonial.quote && (
                <div className={styles.testimonialSection}>
                  <h4 className={styles.testimonialCompany}>
                    Client testimonial
                  </h4>
                  <p className={styles.testimonialQuote}>
                    &ldquo;{solutionData.testimonial.quote}&rdquo;
                  </p>
                  <p className={styles.testimonialAttribution}>
                    – {solutionData.testimonial.company}
                  </p>
                </div>
              )}
          </div>
        )}
      </div>
    </SidePanel>
  );
};

export default SolutionDetailPanel;
