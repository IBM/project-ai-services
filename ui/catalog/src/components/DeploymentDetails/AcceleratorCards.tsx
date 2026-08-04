import { Grid, Column } from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import styles from "./DeploymentDetails.module.scss";

interface AcceleratorCards {
  id: string;
  label: string;
}

interface AcceleratorCardsProps {
  cards: AcceleratorCards[];
}

/**
 * AcceleratorCards component displays Spyre accelerator card allocations
 * Shows a list of accelerator card IDs in a numbered format
 */
const AcceleratorCards = ({ cards }: AcceleratorCardsProps) => {
  // Split cards into two columns for display
  const midpoint = Math.ceil(cards.length / 2);
  const leftColumnCards = cards.slice(0, midpoint);
  const rightColumnCards = cards.slice(midpoint);

  return (
    <Grid className={styles.resourcesGrid}>
      <Column sm={4} md={8} lg={16}>
        <ProductiveCard
          title="Accelerator cards"
          className={styles.resourceCard}
        >
          <Grid className={styles.acceleratorCardsGrid}>
            <Column sm={2} md={4} lg={8}>
              <ol className={styles.acceleratorCardsList}>
                {leftColumnCards.map((card) => (
                  <li key={card.id} className={styles.acceleratorCardItem}>
                    {card.label}
                  </li>
                ))}
              </ol>
            </Column>
            <Column sm={2} md={4} lg={8}>
              <ol
                className={styles.acceleratorCardsList}
                start={leftColumnCards.length + 1}
              >
                {rightColumnCards.map((card) => (
                  <li key={card.id} className={styles.acceleratorCardItem}>
                    {card.label}
                  </li>
                ))}
              </ol>
            </Column>
          </Grid>
        </ProductiveCard>
      </Column>
    </Grid>
  );
};

export default AcceleratorCards;
