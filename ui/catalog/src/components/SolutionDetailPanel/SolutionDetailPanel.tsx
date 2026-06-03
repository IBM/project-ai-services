import { SidePanel } from "@carbon/ibm-products";
import { Tag, Button } from "@carbon/react";
import { Badge, ArrowRight } from "@carbon/icons-react";
import styles from "./SolutionDetailPanel.module.scss";

interface SolutionDetailPanelProps {
  open: boolean;
  onClose: () => void;
}

const SolutionDetailPanel = ({ open, onClose }: SolutionDetailPanelProps) => {
  // Dummy data based on the reference
  const solutionData = {
    title: "IT service desk assistant",
    description:
      "Enables teams to quickly resolve everyday IT issues, automate common support tasks, and get instant help—without waiting for a technician.",
    tags: ["by IBM Power", "IBM certified"],
    domains: ["IT operations"],
    referenceArchitectures: ["Digital assistant"],
    demos: {
      title: "Watch Spyre for Power + Turnkey AI demo",
      url: "#",
    },
    clientStories: [
      {
        company: "System House, DACH",
        description:
          'A large system house and IBM Power client uses IBM Spyre™ for Power "Digital assistant" as an assistant for their IT service desk team. The IT operations team routinely receives tickets where IT users are looking for competent answers to IT-related questions. The IT service desk must find a suitable sequence of commands for diagnosing and then issuing the issue at hand. It is key to provide these answers correctly and quickly, as to fix issues both correctly and in time.Newer members of the IT service desk team are often challenged by these goals, especially when being alone during on-call duty. Their new IT service desk assistant helps bringing these team members up to speed, gives them confidence by reaffirming their envisioned solutions are solid, and even boosts the productivity of senior members of the team.',
      },
    ],
  };

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
              {solutionData.tags[0]}
            </Tag>
            {solutionData.referenceArchitectures.map((arch, index) => (
              <Tag type="gray" size="sm" key={index}>
                {arch}
              </Tag>
            ))}
            <div className={styles.certifiedTag}>
              <Badge size={16} className={styles.badgeIcon} />
              <span>{solutionData.tags[1]}</span>
            </div>
          </div>
        </div>

        <div className={styles.domainSection}>
          <h4 className={styles.domainSectionTitle}>Domain</h4>
          <ul className={styles.domainList}>
            {solutionData.domains.map((domain, index) => (
              <li key={index}>{domain}</li>
            ))}
          </ul>
        </div>

        <div className={styles.demoSection}>
          <h4 className={styles.demoSectionTitle}>Demos and prototypes</h4>
          <Button
            kind="tertiary"
            size="sm"
            renderIcon={ArrowRight}
            onClick={() =>
              window.open(
                solutionData.demos.url,
                "_blank",
                "noopener,noreferrer",
              )
            }
            className={styles.demoButton}
          >
            {solutionData.demos.title}
          </Button>
        </div>

        <div className={styles.storiesSection}>
          <h4 className={styles.storiesSectionTitle}>
            Client stories and testimonials
          </h4>
          <div className={styles.storiesContainer}>
            {solutionData.clientStories.map((story, index) => (
              <div key={index} className={styles.story}>
                <p className={styles.storyCompany}>{story.company}</p>
                <p className={styles.storyDescription}>{story.description}</p>
              </div>
            ))}
          </div>
        </div>
      </div>
    </SidePanel>
  );
};

export default SolutionDetailPanel;
