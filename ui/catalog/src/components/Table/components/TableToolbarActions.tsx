import { forwardRef } from "react";
import type { ReactNode } from "react";
import {
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Button,
  Checkbox,
  CheckboxGroup,
  OverflowMenu,
} from "@carbon/react";
import {
  Export,
  Column as ColumnIcon,
  Renew,
  Filter,
} from "@carbon/icons-react";
import type { TableHeaders } from "@/components/Table/types";
import { getToggleableHeaders } from "@/components/Table/utils/tableUtils";
import sharedStyles from "@/components/Table/table.shared.module.scss";

interface TableToolbarActionsProps {
  /** Current search string. */
  search: string;
  /** Full HEADERS constant for this table (used to build the Edit columns menu). */
  headers: TableHeaders;
  /** Current column visibility map. */
  visibleColumns: Record<string, boolean>;
  /** Called with the new search value on every keystroke. */
  onSearchChange: (value: string) => void;
  /** Called when the refresh icon button is clicked. */
  onRefresh: () => void;
  /** Called when the export icon button is clicked (opens the export modal). */
  onExport: () => void;
  /** Called with the column key when a checkbox in Edit columns is toggled. */
  onToggleColumn: (key: string) => void;
  /** Called when the Reset button inside Edit columns is clicked. */
  onResetColumns: () => void;
  /**
   * Optional slot for a filter control unique to a specific table.
   */
  filterSlot?: ReactNode;
  /**
   * Accessible label for the filter overflow menu button.
   */
  filterLabel?: string;
  /**
   * Additional toolbar buttons rendered after the column-visibility menu.
   */
  children?: ReactNode;
}

/**
 * Carbon's OverflowMenu clones internal props (closeMenu, handleOverflowMenuItemFocus)
 * onto each direct child. A plain <li> forwards them to the DOM, causing React warnings.
 * This wrapper absorbs those props so they never reach the DOM.
 */

const OverflowMenuPanel = forwardRef<
  HTMLLIElement,
  {
    children?: ReactNode;
    className?: string;
    role?: string;
    closeMenu?: unknown;
    handleOverflowMenuItemFocus?: unknown;
  }
>(
  (
    { children, closeMenu: _c, handleOverflowMenuItemFocus: _h, ...rest },
    ref,
  ) => (
    <li ref={ref} {...rest}>
      {children}
    </li>
  ),
);
OverflowMenuPanel.displayName = "OverflowMenuPanel";

const TableToolbarActions = ({
  search,
  headers,
  visibleColumns,
  onSearchChange,
  onRefresh,
  onExport,
  onToggleColumn,
  onResetColumns,
  filterSlot,
  filterLabel = "Filter",
  children,
}: TableToolbarActionsProps) => {
  const toggleableHeaders = getToggleableHeaders(headers);

  return (
    <TableToolbar>
      <TableToolbarSearch
        placeholder="Search"
        persistent
        value={search}
        onChange={(e) => {
          if (typeof e !== "string") {
            onSearchChange(e.target.value);
          }
        }}
      />

      <TableToolbarContent>
        <Button
          hasIconOnly
          kind="ghost"
          renderIcon={Renew}
          iconDescription="Refresh"
          size="lg"
          onClick={onRefresh}
        />

        {filterSlot && (
          <OverflowMenu
            renderIcon={Filter}
            iconDescription={filterLabel}
            aria-label={filterLabel}
            size="lg"
            flipped
          >
            <OverflowMenuPanel
              className={sharedStyles.overflowMenuContent}
              role="none"
            >
              {filterSlot}
            </OverflowMenuPanel>
          </OverflowMenu>
        )}

        <Button
          hasIconOnly
          kind="ghost"
          renderIcon={Export}
          iconDescription="Export"
          size="lg"
          onClick={onExport}
        />

        <OverflowMenu
          renderIcon={ColumnIcon}
          iconDescription="Edit columns"
          aria-label="Edit columns"
          size="lg"
          flipped
        >
          <OverflowMenuPanel
            className={sharedStyles.overflowMenuContent}
            role="none"
          >
            <h6 className={sharedStyles.overflowMenuHeading}>Edit columns</h6>
            <CheckboxGroup legendText="">
              {toggleableHeaders.map((header) => (
                <Checkbox
                  key={`column-${header.key}`}
                  labelText={String(header.header)}
                  id={`column-${header.key}`}
                  checked={visibleColumns[header.key] === true}
                  disabled={header.key === "name"}
                  onChange={() => onToggleColumn(header.key)}
                />
              ))}
            </CheckboxGroup>
            <div className={sharedStyles.overflowMenuActions}>
              <Button kind="secondary" size="sm" onClick={onResetColumns}>
                Reset
              </Button>
            </div>
          </OverflowMenuPanel>
        </OverflowMenu>

        {children}
      </TableToolbarContent>
    </TableToolbar>
  );
};

export default TableToolbarActions;
