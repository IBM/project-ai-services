import { NoDataEmptyState } from "@carbon/ibm-products";

interface TableEmptyStatesProps {
  /** Non-null means a fetch error occurred — show the error message. */
  fetchError: string | null;
  /** True when the table has no rows and no fetch error. */
  noData: boolean;
  /** True when the current search/filter produces zero visible rows. */
  noSearchResults: boolean;
  /**
   * Singular lower-case entity name, e.g. "digital assistant" or "service".
   * Used to generate human-readable titles and subtitles.
   */
  entityName: string;
  /** Optional CSS class forwarded to the NoDataEmptyState root element. */
  className?: string;
}

const TableEmptyStates = ({
  fetchError,
  noData,
  noSearchResults,
  entityName,
  className,
}: TableEmptyStatesProps) => {
  if (fetchError) {
    return (
      <NoDataEmptyState
        title={`Error loading ${entityName}s`}
        subtitle={fetchError}
        className={className}
      />
    );
  }

  if (noData) {
    // Capitalise first letter for the title
    const capitalized =
      entityName.charAt(0).toUpperCase() + entityName.slice(1);
    return (
      <NoDataEmptyState
        title={`Start by adding a ${entityName}`}
        subtitle={`To deploy a new ${capitalized}, click Deploy.`}
        className={className}
      />
    );
  }

  if (noSearchResults) {
    return (
      <NoDataEmptyState
        title="No data"
        subtitle="Try adjusting your search or filter."
        className={className}
      />
    );
  }

  return null;
};

export default TableEmptyStates;
