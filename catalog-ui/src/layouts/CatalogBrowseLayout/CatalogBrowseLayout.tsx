import type { ReactNode } from "react";
import { PageHeader } from "@carbon/ibm-products";
import { Grid, Column, Search, Accordion, Button, Tag } from "@carbon/react";
import { ArrowRight } from "@carbon/icons-react";
import styles from "./CatalogBrowseLayout.module.scss";

export interface CatalogBrowseLayoutProps {
  title: string;
  subtitle: string;
  searchValue: string;
  onSearchChange: (value: string) => void;
  totalSelectedFilters: number;
  onClearFilters: () => void;
  filterAccordions: ReactNode;
  results: ReactNode;
  emptyMessage: string;
}

const CatalogBrowseLayout = ({
  title,
  subtitle,
  searchValue,
  onSearchChange,
  totalSelectedFilters,
  onClearFilters,
  filterAccordions,
  results,
  emptyMessage,
}: CatalogBrowseLayoutProps) => {
  const hasResults = Boolean(results);

  return (
    <>
      <PageHeader
        title={{ text: title }}
        subtitle={subtitle}
        pageActions={[
          {
            key: "learn-more",
            kind: "tertiary",
            label: "Learn more",
            renderIcon: ArrowRight,
            onClick: () => {
              window.open(
                "https://www.ibm.com/docs/en/aiservices?topic=services-introduction",
                "_blank",
              );
            },
          },
        ]}
        pageActionsOverflowLabel="More actions"
        fullWidthGrid="xl"
      />

      <div className={styles.pageContent}>
        <Grid fullWidth>
          <Column lg={4} md={2} sm={4}>
            <div className={styles.sidebar}>
              <Search
                placeholder="Search"
                labelText="Search"
                value={searchValue}
                onChange={(event) => onSearchChange(event.target.value)}
                size="lg"
              />

              {totalSelectedFilters > 0 && (
                <div className={styles.filterHeader}>
                  <span className={styles.filterTitle}>Filters</span>
                  <Tag
                    type="high-contrast"
                    size="md"
                    filter
                    onClose={onClearFilters}
                  >
                    {totalSelectedFilters}
                  </Tag>
                </div>
              )}

              <Accordion>{filterAccordions}</Accordion>
            </div>
          </Column>

          <Column lg={12} md={6} sm={4}>
            {hasResults ? (
              <div className={styles.cardsGrid}>{results}</div>
            ) : (
              <div className={styles.emptyState}>
                <p>{emptyMessage}</p>
                <Button kind="tertiary" onClick={onClearFilters}>
                  Clear filters
                </Button>
              </div>
            )}
          </Column>
        </Grid>
      </div>
    </>
  );
};

export default CatalogBrowseLayout;
