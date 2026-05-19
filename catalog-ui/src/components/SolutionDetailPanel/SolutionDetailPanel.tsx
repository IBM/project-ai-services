import { SidePanel } from "@carbon/ibm-products";
import { Tag, Button } from "@carbon/react";
import { Badge, ArrowRight } from "@carbon/icons-react";
import { useUseCases } from "@/hooks/useUseCases";
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
      size="md"
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
            <Button
              kind="tertiary"
              size="sm"
              renderIcon={ArrowRight}
              onClick={() =>
                window.open(solutionData.demo, "_blank", "noopener,noreferrer")
              }
              className={styles.demoButton}
            >
              Watch demo
            </Button>
          </div>
        )}

        {allStories.length > 0 && (
          <div className={styles.storiesSection}>
            <h4 className={styles.storiesSectionTitle}>
              Client stories and testimonials
            </h4>
            <div className={styles.storiesContainer}>
              {allStories.map((story, index) => (
                <div key={index} className={styles.story}>
                  <p className={styles.storyCompany}>{story.company}</p>
                  {story.description && (
                    <p className={styles.storyDescription}>
                      {story.description}
                    </p>
                  )}
                  {story.url && (
                    <Button
                      kind="tertiary"
                      size="sm"
                      renderIcon={ArrowRight}
                      onClick={() =>
                        window.open(story.url, "_blank", "noopener,noreferrer")
                      }
                      className={styles.demoButton}
                    >
                      Read {story.company} client story
                    </Button>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </SidePanel>
  );
};

export default SolutionDetailPanel;
