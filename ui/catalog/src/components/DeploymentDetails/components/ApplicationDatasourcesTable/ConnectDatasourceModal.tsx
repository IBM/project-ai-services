import { useState, useEffect, useId } from "react";
import {
  Modal,
  FilterableMultiSelect,
  InlineNotification,
  DropdownSkeleton,
  Tag,
} from "@carbon/react";
import { fetchAllDataSourceConnectors } from "@/api/connectors.api";
import { connectApplicationDatasources } from "@/api/applications.api";
import type { DataSourceConnectorApiResponse } from "@/types/api.types";
import styles from "./ConnectDatasourceModal.module.scss";

interface ConnectDatasourceModalProps {
  open: boolean;
  applicationId: string;
  /** IDs of connectors already connected — these are excluded from the dropdown */
  connectedIds: Set<string>;
  onClose: () => void;
  onConnected: () => void;
}

interface DropdownItem {
  id: string;
  label: string;
}

const ConnectDatasourceModal = ({
  open,
  applicationId,
  connectedIds,
  onClose,
  onConnected,
}: ConnectDatasourceModalProps) => {
  const multiSelectId = useId();

  const [allConnectors, setAllConnectors] = useState<
    DataSourceConnectorApiResponse[]
  >([]);
  const [isLoadingConnectors, setIsLoadingConnectors] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<DropdownItem[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [selectionError, setSelectionError] = useState(false);

  // Fetch all connectors when the modal opens; clear form state when it closes
  useEffect(() => {
    if (!open) {
      // Don't clear allConnectors here — doing so causes a "no data sources"
      // flash during the closing animation when the list becomes empty before
      // the modal has fully closed.
      setSelectedItems([]);
      setSubmitError(null);
      setLoadError(null);
      setSelectionError(false);
      return;
    }

    let cancelled = false;

    setAllConnectors([]);
    setSelectedItems([]);
    setSubmitError(null);
    setLoadError(null);
    setSelectionError(false);
    setIsLoadingConnectors(true);

    fetchAllDataSourceConnectors()
      .then((data) => {
        if (!cancelled) setAllConnectors(data);
      })
      .catch((err: unknown) => {
        if (!cancelled)
          setLoadError(
            err instanceof Error ? err.message : "Failed to load data sources",
          );
      })
      .finally(() => {
        if (!cancelled) setIsLoadingConnectors(false);
      });

    return () => {
      cancelled = true;
    };
  }, [open]);

  // Available connectors = all connectors minus already-connected ones
  const availableConnectors = allConnectors.filter(
    (c) => !connectedIds.has(c.id),
  );

  const dropdownItems: DropdownItem[] = availableConnectors.map((c) => ({
    id: c.id,
    label: `${c.name} (${c.provider.name})`,
  }));

  const hasNoConnectors =
    !isLoadingConnectors && !loadError && availableConnectors.length === 0;

  // True when connectors exist but every one is already connected
  const allAlreadyConnected = hasNoConnectors && allConnectors.length > 0;

  const handleSubmit = async () => {
    if (selectedItems.length === 0) {
      setSelectionError(true);
      return;
    }
    setIsSubmitting(true);
    setSubmitError(null);
    try {
      await connectApplicationDatasources(
        applicationId,
        selectedItems.map((item) => item.id),
      );
      onConnected();
    } catch (err: unknown) {
      setSubmitError(
        err instanceof Error ? err.message : "Failed to connect data sources",
      );
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      open={open}
      size="sm"
      modalHeading="Connect data source"
      primaryButtonText={isSubmitting ? "Adding..." : "Add"}
      secondaryButtonText="Cancel"
      primaryButtonDisabled={isSubmitting || hasNoConnectors || !!loadError}
      onRequestClose={() => {
        if (!isSubmitting) onClose();
      }}
      onRequestSubmit={() => void handleSubmit()}
    >
      <p className={styles.description}>
        Files will begin ingesting once the connection is established. You can
        continue working while this completes.
      </p>

      {submitError && (
        <InlineNotification
          kind="error"
          title="Error"
          subtitle={submitError}
          lowContrast
          hideCloseButton
          className={styles.notification}
        />
      )}

      <div className={styles.fieldWrapper}>
        {/* State: loading */}
        {isLoadingConnectors && <DropdownSkeleton hideLabel />}

        {/* State: API fetch failed */}
        {!isLoadingConnectors && loadError && (
          <InlineNotification
            kind="error"
            title="Failed to load data sources"
            subtitle={loadError}
            lowContrast
            hideCloseButton
            className={styles.notification}
          />
        )}

        {/* State: loaded but nothing available */}
        {hasNoConnectors && (
          <InlineNotification
            kind="info"
            title={
              allAlreadyConnected
                ? "All data sources are already connected"
                : "No data sources available"
            }
            subtitle={
              allAlreadyConnected
                ? "All configured data sources have been connected to this application."
                : "To get started, add a data source in the Connectors panel."
            }
            lowContrast
            hideCloseButton
            className={styles.notification}
          />
        )}

        {/* State: connectors available */}
        {!isLoadingConnectors && !loadError && !hasNoConnectors && (
          <>
            <FilterableMultiSelect
              id={multiSelectId}
              titleText="Data source"
              placeholder="Select data sources"
              items={dropdownItems}
              itemToString={(item) => (item ? item.label : "")}
              selectedItems={selectedItems}
              onChange={({ selectedItems: next }) => {
                setSelectedItems(next ?? []);
                if ((next ?? []).length > 0) setSelectionError(false);
              }}
              disabled={isSubmitting}
              invalid={selectionError}
              invalidText="Select one or more data sources"
              selectionFeedback="top-after-reopen"
              autoAlign
            />
            {selectedItems.length > 0 && (
              <div className={styles.selectedTags}>
                {selectedItems.map((item) => (
                  <Tag
                    key={item.id}
                    type="cool-gray"
                    size="md"
                    className={styles.tag}
                  >
                    {item.label}
                  </Tag>
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </Modal>
  );
};

export default ConnectDatasourceModal;
