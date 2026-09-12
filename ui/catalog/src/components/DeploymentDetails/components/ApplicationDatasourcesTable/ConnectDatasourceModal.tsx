import { useState, useEffect, useId } from "react";
import {
  Modal,
  FilterableMultiSelect,
  InlineNotification,
  ActionableNotification,
  UnorderedList,
  ListItem,
  DropdownSkeleton,
  Tag,
} from "@carbon/react";
import { fetchAllDataSourceConnectors } from "@/api/connectors.api";
import {
  connectApplicationDatasources,
  fetchAllApplicationDatasources,
} from "@/api/applications.api";
import type {
  DataSourceConnectorApiResponse,
  ConnectDatasourceError,
} from "@/types/api.types";
import styles from "./ConnectDatasourceModal.module.scss";

interface ConnectDatasourceModalProps {
  open: boolean;
  applicationId: string;
  onClose: () => void;
  onConnected: () => void;
  /** Called when some (but not all) datasources connected — modal stays open. */
  onPartialConnect?: () => void;
}

interface DropdownItem {
  id: string;
  label: string;
}

const ConnectDatasourceModal = ({
  open,
  applicationId,
  onClose,
  onConnected,
  onPartialConnect,
}: ConnectDatasourceModalProps) => {
  const multiSelectId = useId();

  const [allConnectors, setAllConnectors] = useState<
    DataSourceConnectorApiResponse[]
  >([]);
  const [connectedIds, setConnectedIds] = useState<Set<string>>(new Set());
  const [isLoadingConnectors, setIsLoadingConnectors] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedItems, setSelectedItems] = useState<DropdownItem[]>([]);
  // Snapshot of selectedItems at submit time — kept so the error notification
  // can display labels and the "X of Y" count even after selectedItems is cleared.
  const [submittedItems, setSubmittedItems] = useState<DropdownItem[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [connectErrors, setConnectErrors] = useState<ConnectDatasourceError[]>(
    [],
  );
  const [selectionError, setSelectionError] = useState(false);

  // Fetch all connectors when the modal opens; clear form state when it closes
  useEffect(() => {
    if (!open) {
      // Don't clear allConnectors here — doing so causes a "no data sources"
      // flash during the closing animation when the list becomes empty before
      // the modal has fully closed.
      setSelectedItems([]);
      setSubmittedItems([]);
      setSubmitError(null);
      setConnectErrors([]);
      setLoadError(null);
      setSelectionError(false);
      return;
    }

    let cancelled = false;

    setAllConnectors([]);
    setConnectedIds(new Set());
    setSelectedItems([]);
    setSubmittedItems([]);
    setSubmitError(null);
    setConnectErrors([]);
    setLoadError(null);
    setSelectionError(false);
    setIsLoadingConnectors(true);

    Promise.all([
      fetchAllDataSourceConnectors(),
      fetchAllApplicationDatasources(applicationId),
    ])
      .then(([connectors, alreadyConnected]) => {
        if (!cancelled) {
          setAllConnectors(connectors);
          setConnectedIds(new Set(alreadyConnected.map((r) => r.id)));
        }
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
  }, [open, applicationId]);

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

  // Build a human-readable label for a failed datasource_id using the
  // submittedItems snapshot (preserved even after selectedItems is cleared).
  const labelForId = (id: string): string =>
    submittedItems.find((item) => item.id === id)?.label ?? id;

  const handleSubmit = async () => {
    if (selectedItems.length === 0) {
      setSelectionError(true);
      return;
    }
    // Snapshot before any async work so the error notification can reference
    // labels and the total count even after selectedItems is cleared.
    const snapshot = selectedItems;
    setIsSubmitting(true);
    setSubmitError(null);
    setConnectErrors([]);
    try {
      const errors = await connectApplicationDatasources(
        applicationId,
        snapshot.map((item) => item.id),
      );

      if (errors.length === 0) {
        // All succeeded — close and refresh.
        onConnected();
      } else {
        // At least one failed — stay open and surface the reasons.
        setSubmittedItems(snapshot);
        setConnectErrors(errors);
        // Clear the selection so the user starts fresh.
        setSelectedItems([]);
        // If some succeeded, remove them from the available list and
        // refresh the table in the background.
        if (errors.length < snapshot.length) {
          const failedIds = new Set(errors.map((e) => e.datasource_id));
          setConnectedIds((prev) => {
            const next = new Set(prev);
            snapshot.forEach((item) => {
              if (!failedIds.has(item.id)) next.add(item.id);
            });
            return next;
          });
          onPartialConnect?.();
        }
      }
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

      {connectErrors.length > 0 && (
        <ActionableNotification
          inline
          kind="error"
          title={
            connectErrors.length === submittedItems.length
              ? "Failed to connect all data sources"
              : `Failed to connect ${connectErrors.length.toString()} of ${submittedItems.length.toString()} data source${submittedItems.length !== 1 ? "s" : ""}`
          }
          subtitle={
            <UnorderedList className={styles.errorList}>
              {connectErrors.map((e) => (
                <ListItem key={e.datasource_id}>
                  <strong>{labelForId(e.datasource_id)}:</strong> {e.error}
                </ListItem>
              ))}
            </UnorderedList>
          }
          lowContrast
          onCloseButtonClick={() => setConnectErrors([])}
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

        {/* State: loaded but nothing available — only show when there are no
            connect errors so both notifications don't appear simultaneously */}
        {hasNoConnectors && connectErrors.length === 0 && (
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
