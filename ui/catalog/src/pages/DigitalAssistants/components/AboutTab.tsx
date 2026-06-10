import { Button, Grid, Column, Layer, Link } from "@carbon/react";
import { Deploy, Code, PlayOutline } from "@carbon/icons-react";
import styles from "../DigitalAssistants.module.scss";

interface AboutTabProps {
  onDeployClick: () => void;
}

export const AboutTab: React.FC<AboutTabProps> = ({ onDeployClick }) => {
  return (
    <div className={styles.aboutContent}>
      {/* Services Section */}
      <Layer withBackground>
        <section className={styles.aboutSection}>
          <div className={styles.sectionHeader}>
            <h4 className={styles.aboutSectionTitle}>Services</h4>
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
            <li>Digitize documents</li>
            <li>Find similar items</li>
            <li>Question and answer</li>
            <li>Summarize</li>
          </ul>
        </section>
      </Layer>

      {/* Use Case Domains Section */}
      <Layer withBackground>
        <section className={styles.aboutSection}>
          <h4 className={styles.aboutSectionTitle}>Use case domains</h4>
          <Grid narrow className={styles.gridWithTopMargin}>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>Agriculture</h5>
              <ul className={styles.useCaseList}>
                <li>Agriculture assistant</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>Banking</h5>
              <ul className={styles.useCaseList}>
                <li>Analyst assistant</li>
                <li>Financial documents assistant</li>
                <li>Open account agent</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>
                Enterprise resource planning
              </h5>
              <ul className={styles.useCaseList}>
                <li>BI assistant</li>
                <li>Invoice matching assistant</li>
                <li>Order processing assistant</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>Insurance</h5>
              <ul className={styles.useCaseList}>
                <li>Claims & policy management agent</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>IT operations</h5>
              <ul className={styles.useCaseList}>
                <li>Invoice matching assistant</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>Public sector</h5>
              <ul className={styles.useCaseList}>
                <li>Private documents assistant</li>
                <li>Product sales assistant</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>Professional services</h5>
              <ul className={styles.useCaseList}>
                <li>Conference slide search</li>
              </ul>
            </Column>
            <Column sm={4} md={4} lg={4}>
              <h5 className={styles.useCaseDomain}>Real estate</h5>
              <ul className={styles.useCaseList}>
                <li>Real estate assistant</li>
              </ul>
            </Column>
          </Grid>
        </section>
      </Layer>

      {/* Minimum Resource Allocation Section */}
      <Layer withBackground>
        <section className={styles.aboutSection}>
          <h4 className={styles.aboutSectionTitle}>
            Minimum resource allocation
          </h4>
          <Grid narrow className={styles.gridWithTopMargin}>
            <Column sm={4} md={4} lg={5}>
              <div className={styles.resourceItem}>
                <span className={styles.resourceLabel}>Required cores</span>
                <span className={styles.resourceValue}>0.5 - 2.0</span>
              </div>
            </Column>
            <Column sm={4} md={4} lg={5}>
              <div className={styles.resourceItem}>
                <span className={styles.resourceLabel}>Required memory</span>
                <span className={styles.resourceValue}>15GB - 25GB</span>
              </div>
            </Column>
            <Column sm={4} md={4} lg={6}>
              <div className={styles.resourceItem}>
                <span className={styles.resourceLabel}>
                  Required Spyre cards
                </span>
                <span className={styles.resourceValue}>4 cards</span>
              </div>
            </Column>
          </Grid>
        </section>
      </Layer>

      {/* Code and Architecture + Demos Section (Side by Side) */}
      <div className={styles.sideBySideGrid}>
        {/* Code and Architecture Section */}
        <Layer withBackground className={styles.sideBySideColumn}>
          <section className={styles.sideBySideSection}>
            <h4 className={styles.aboutSectionTitle}>Code and architecture</h4>
            <Button
              kind="tertiary"
              size="sm"
              className={styles.codeButton}
              renderIcon={Code}
              onClick={() =>
                window.open(
                  "https://github.com/IBM/project-ai-services/tree/main/services/chatbot",
                  "_blank",
                )
              }
            >
              View code
            </Button>
            <div className={styles.architectureDiagram}>
              <img
                src="images/ragArchDiagram.webp"
                alt="RAG Architecture Diagram"
                className={styles.diagramImage}
              />
            </div>
          </section>
        </Layer>

        {/* Demos and Prototypes Section */}
        <Layer withBackground className={styles.sideBySideColumn}>
          <section className={styles.demosSection}>
            <h4 className={styles.aboutSectionTitle}>Demos and prototypes</h4>
            <div className={styles.demoCard}>
              <img
                src="images/ragDemoThumbnail.webp"
                alt="RAG Demo"
                className={styles.demoImage}
              />
              <div className={styles.demoContent}>
                <h5 className={styles.demoTitle}>
                  Retrieval-Augmented Generation (RAG)
                </h5>
                <p className={styles.demoDescription}>
                  Discover the architecture behind this pre-built digital
                  assistant
                </p>
                <div className={styles.demoActions}>
                  <Link
                    href="https://github.com/user-attachments/assets/958980a7-f653-4474-84a7-28d657b5f7d1"
                    target="_blank"
                    renderIcon={PlayOutline}
                  >
                    Watch
                  </Link>
                </div>
              </div>
            </div>
          </section>
        </Layer>
      </div>
    </div>
  );
};
