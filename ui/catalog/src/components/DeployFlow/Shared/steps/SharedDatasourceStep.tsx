import { useEffect, type FC } from "react";
import {
  Toggle,
  Tile,
  Grid,
  Column,
  FilterableMultiSelect,
  InlineNotification,
  SkeletonPlaceholder,
  Tag,
} from "@carbon/react";
import { CheckmarkFilled } from "@carbon/icons-react";
import styles from "../DeployFlow.shared.module.scss";
import stepStyles from "../SharedDatasourceStep.module.scss";
import type { BaseStepProps } from "../types";
import { useConnectorDropdownItems } from "../hooks/useConnectorDropdownItems";

type SharedDatasourceStepProps = Pick<
  BaseStepProps,
  "title" | "formData" | "onChange" | "onComponentError"
> & {
  // Set to true by the parent when Deploy is clicked and nothing is selected.
  // Drives the invalid state on the dropdown without disabling the button upfront.
  showSelectionError?: boolean;
};

export const SharedDatasourceStep: FC<SharedDatasourceStepProps> = ({
  title,
  formData,
  onChange,
  onComponentError,
  showSelectionError = false,
}) => {
  const uploadEnabled = formData.uploadFromSourceEnabled ?? false;
  const { items, loading, fetchError } =
    useConnectorDropdownItems(uploadEnabled);
  const selectedIds = formData.dataSources ?? [];
  const selectedCount = selectedIds.length;

  // Only block deploy for a fetch error — the user can't recover from that
  // without leaving the step. Empty selection is caught at submit time via
  // showSelectionError so the button stays enabled until they actually try.
  const blocksDeploy = uploadEnabled && !!fetchError;

  // Propagate block state to parent so the Deploy button can be disabled.
  useEffect(() => {
    onComponentError?.(blocksDeploy);
  }, [blocksDeploy, onComponentError]);

  // Show the invalid state on the dropdown only after a deploy attempt.
  const showInvalid =
    showSelectionError && uploadEnabled && selectedCount === 0;

  const handleToggle = (checked: boolean) => {
    onChange({ uploadFromSourceEnabled: checked });
  };

  const handleSourceChange = (ids: string[]) => {
    onChange({ dataSources: ids });
  };

  const selectedItems = items.filter((item) => selectedIds.includes(item.id));

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      <div className={styles.formSection}>
        {/* Summary tile — "N data sources selected" */}
        <Grid narrow className={stepStyles.summaryGrid}>
          <Column sm={4} md={4} lg={4}>
            <Tile className={stepStyles.summaryTile}>
              <p className={stepStyles.summaryLabel}>Source data location</p>
              <div className={stepStyles.summaryValue}>
                {selectedCount > 0 && (
                  <CheckmarkFilled
                    className={stepStyles.checkIcon}
                    size={20}
                    aria-hidden
                  />
                )}
                <span className={stepStyles.summaryCount}>{selectedCount}</span>
                <span className={stepStyles.summaryText}>
                  {selectedCount === 1
                    ? "data source selected"
                    : "data sources selected"}
                </span>
              </div>
            </Tile>
          </Column>
        </Grid>

        {/* Upload-from-source toggle card */}
        <div className={stepStyles.uploadCard}>
          <div className={stepStyles.uploadCardHeader}>
            <div className={stepStyles.uploadCardText}>
              <p className={stepStyles.uploadCardTitle}>
                Upload data from source locations
              </p>
              <p className={stepStyles.uploadCardDescription}>
                Connect a source location to access documents and data where
                they reside, with automatic synchronization that adds new
                content and eliminates duplicates.
              </p>
            </div>
            <Toggle
              id="upload-from-source-toggle"
              labelText=""
              labelA=""
              labelB=""
              toggled={uploadEnabled}
              onToggle={handleToggle}
              className={stepStyles.toggle}
              aria-label="Upload data from source locations"
            />
          </div>

          {uploadEnabled && (
            <div className={stepStyles.dropdownSection}>
              {loading && (
                <SkeletonPlaceholder className={stepStyles.dropdownSkeleton} />
              )}
              {/* State 1: API call failed */}
              {!loading && fetchError && (
                <InlineNotification
                  kind="error"
                  title="Failed to load data sources"
                  subtitle={fetchError}
                  lowContrast
                  hideCloseButton
                />
              )}
              {/* State 2: API succeeded but no connectors are configured */}
              {!loading && !fetchError && items.length === 0 && (
                <InlineNotification
                  kind="info"
                  title="No data sources available"
                  subtitle="To get started, configure a data source connection in the Connectors panel."
                  lowContrast
                  hideCloseButton
                />
              )}
              {/* State 3: API succeeded and connectors are available to select */}
              {!loading && !fetchError && items.length > 0 && (
                <>
                  <FilterableMultiSelect
                    id="deploy-data-source-dropdown"
                    titleText="Data source"
                    placeholder="Select data sources"
                    items={items}
                    itemToString={(item) => item?.label ?? ""}
                    selectedItems={selectedItems}
                    onChange={({ selectedItems: next }) =>
                      handleSourceChange((next ?? []).map((item) => item.id))
                    }
                    invalid={showInvalid}
                    invalidText="Select one or more data sources"
                    selectionFeedback="top-after-reopen"
                    className={stepStyles.dropdown}
                  />
                  {selectedItems.length > 0 && (
                    <div className={stepStyles.selectedTags}>
                      {selectedItems.map((item) => (
                        <Tag key={item.id} type="cool-gray" size="md">
                          {item.label}
                        </Tag>
                      ))}
                    </div>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
};
