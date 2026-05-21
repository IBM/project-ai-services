import { Grid, Column } from "@carbon/react";
import { ArchitectureCard, SolutionCard } from "@/components";
import styles from "./CardsDemo.module.scss";

const CardsDemo = () => {
  const handleDeploy = (id: string) => {
    console.log("Deploy clicked:", id);
  };

  const handleLearnMore = (id: string) => {
    console.log("Learn more clicked:", id);
  };

  const handleViewDetails = (id: string) => {
    console.log("View details clicked:", id);
  };

  return (
    <div className={styles.container}>
      <h1 className={styles.pageTitle}>Card Components Demo</h1>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>ArchitectureCard Examples</h2>
        <Grid>
          <Column sm={4} md={4} lg={5}>
            <ArchitectureCard
              id="catalog-1"
              title="Digital assistant"
              description="Enable digital assistants using Retrieval-Augmented Generation (RAG) so AI services can query a managed knowledge base to answer questions from custom documents and data."
              tags={[
                "Q&A",
                "Find similar items",
                "Translation",
                "Digitize documents",
                "Summarization",
                "Reranking",
              ]}
              isCertified={true}
              tagsHeading="Services"
              onDeploy={handleDeploy}
              onLearnMore={handleLearnMore}
            />
          </Column>
          <Column sm={4} md={4} lg={5}>
            <ArchitectureCard
              id="catalog-2"
              title="Document processing"
              description="Process and analyze documents using advanced AI capabilities for extraction, classification, and insights generation."
              tags={["OCR", "Classification", "Extraction", "Analysis"]}
              isCertified={false}
              tagsHeading="Services"
              onDeploy={handleDeploy}
              onLearnMore={handleLearnMore}
            />
          </Column>
          <Column sm={4} md={4} lg={6}>
            <ArchitectureCard
              id="catalog-3"
              title="Data analytics platform"
              description="Comprehensive analytics solution for processing large datasets and generating actionable insights with machine learning."
              tags={[]}
              isCertified={true}
              tagsHeading="Services"
              onDeploy={handleDeploy}
              onLearnMore={handleLearnMore}
            />
          </Column>
        </Grid>
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>SolutionCard Examples</h2>
        <Grid>
          <Column sm={4} md={4} lg={5} className={styles.solutionCardColumn}>
            <SolutionCard
              id="solution-1"
              title="Agriculture assistant"
              description="Provides farmers with accurate, validated crop‑growing guidance tailored to their region and preferred language."
              tags={["Digital assistant", "Crop guidance"]}
              category="Agriculture"
              onViewDetails={handleViewDetails}
            />
          </Column>
          <Column sm={4} md={4} lg={5} className={styles.solutionCardColumn}>
            <SolutionCard
              id="solution-2"
              title="Banking advisor"
              description="AI-powered financial advisory system providing personalized banking recommendations and insights."
              tags={["Financial services", "Advisory"]}
              category="Banking and Finance"
              onViewDetails={handleViewDetails}
            />
          </Column>
          <Column sm={4} md={4} lg={6} className={styles.solutionCardColumn}>
            <SolutionCard
              id="solution-3"
              title="Healthcare diagnostics"
              description="Advanced diagnostic support system for healthcare professionals using AI-powered analysis."
              tags={["Diagnostics", "Healthcare AI"]}
              category="Healthcare"
              onViewDetails={handleViewDetails}
            />
          </Column>
        </Grid>
      </section>
    </div>
  );
};

export default CardsDemo;
