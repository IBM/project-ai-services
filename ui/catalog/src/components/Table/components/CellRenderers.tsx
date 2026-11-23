import React from "react";
import { Link, OverflowMenu, OverflowMenuItem } from "@carbon/react";
import { ErrorFilled, InProgress, Delete } from "@carbon/icons-react";
import StatusTag from "@/components/StatusTag";
import sharedStyles from "@/components/Table/table.shared.module.scss";

export interface SharedCellRendererProps {
  value: unknown;
  rowId: string;
  rowData?: { status?: string };
}

export interface ActionCellProps {
  rowId: string;
  rowData?: { status?: string };
  onDelete: (rowId: string) => void;
  // Each table has its own delete eligibility rule and passes it explicitly.
  isDeleteEnabled: (status: string | undefined) => boolean;
  /** Called when the overflow menu opens — use to prefetch data keyed by rowId */
  onMenuOpen?: (rowId: string) => Promise<void>;
  /** Called when the user clicks "View integration endpoints" — only enabled when Running */
  onViewIntegration?: (rowId: string) => void;
  /** Called when the user clicks "Launch service endpoint" — only enabled when Running */
  onLaunchEndpoint?: (rowId: string) => void;
}

export interface NameCellProps {
  value: unknown;
  rowId: string;
  rowData?: { status?: string; type?: string };
  onNameClick?: (
    id: string,
    name: string,
    status: string,
    type: string,
  ) => void;
  /** When true, the name renders as a clickable link; otherwise plain text. */
  isLinkEnabled?: boolean;
}

export const StatusCell = ({ value }: SharedCellRendererProps) => (
  <StatusTag status={String(value)} />
);

export interface MessageCellProps extends SharedCellRendererProps {
  /**
   * Statuses that indicate a clean/healthy state — message is suppressed when
   * the row's status is in this list (or when the message is empty).
   * Defaults to ["Running", "connected", "up to date"] — covers the deployments table
   * (Running) and the connectors table (connected) and the application datasources table (up to date).
   */
  hideStatuses?: string[];
  /**
   * Statuses that warrant the error icon instead of the in-progress icon.
   * Defaults to ["Error", "offline"] to preserve existing behaviour for the deployments table.
   */
  errorStatuses?: string[];
}

export const MessageCell = ({
  value,
  rowData,
  hideStatuses = ["Running", "connected", "up to date"],
  errorStatuses = ["Error", "offline", "out of sync"],
}: MessageCellProps) => {
  const message = String(value || "");
  const status = rowData?.status || "";

  // Hide message when the row is in a clean/healthy state or there is no message
  if (hideStatuses.includes(status) || !message) {
    return <span></span>;
  }

  let MessageIcon;
  let iconClassName: string;

  // Hard-failure statuses get the error icon; everything else gets the in-progress icon
  if (errorStatuses.includes(status)) {
    MessageIcon = ErrorFilled;
    iconClassName = sharedStyles.messageIconError;
  } else {
    MessageIcon = InProgress;
    iconClassName = sharedStyles.messageIconInfo;
  }

  return (
    <div className={sharedStyles.messageWithIcon}>
      <MessageIcon size={16} className={iconClassName} />
      <span className={sharedStyles.messageText}>{message}</span>
    </div>
  );
};

export const ActionCell = ({
  rowId,
  rowData,
  onDelete,
  isDeleteEnabled,
  onMenuOpen,
  onViewIntegration,
  onLaunchEndpoint,
}: ActionCellProps) => {
  const [isPrefetching, setIsPrefetching] = React.useState(false);
  const deleteEnabled = isDeleteEnabled(rowData?.status);
  const isRunning = rowData?.status === "Running";

  const handleOpen = React.useCallback(async () => {
    if (!onMenuOpen) return;
    setIsPrefetching(true);
    await onMenuOpen(rowId);
    setIsPrefetching(false);
  }, [onMenuOpen, rowId]);

  return (
    <OverflowMenu
      size="lg"
      flipped
      aria-label="Actions"
      onOpen={onMenuOpen ? handleOpen : undefined}
    >
      {onViewIntegration ? (
        <OverflowMenuItem
          itemText="View integration endpoints"
          disabled={!isRunning}
          onClick={() => onViewIntegration(rowId)}
        />
      ) : null}
      {onLaunchEndpoint ? (
        <OverflowMenuItem
          itemText="Launch service endpoint"
          disabled={!isRunning || isPrefetching}
          onClick={() => onLaunchEndpoint(rowId)}
        />
      ) : null}
      <OverflowMenuItem
        itemText={
          <div className={sharedStyles.deleteMenuItem}>
            <span>Delete</span>
            <Delete size={16} />
          </div>
        }
        isDelete
        disabled={!deleteEnabled}
        onClick={() => onDelete(rowId)}
      />
    </OverflowMenu>
  );
};

export const NameCell = ({
  value,
  rowId,
  rowData,
  onNameClick,
  isLinkEnabled,
}: NameCellProps) => {
  if (!isLinkEnabled || !onNameClick) {
    return <span>{String(value)}</span>;
  }

  return (
    <Link
      href="#"
      onClick={(e: React.MouseEvent<HTMLAnchorElement>) => {
        e.preventDefault();
        e.stopPropagation();
        onNameClick(
          rowId,
          String(value),
          rowData?.status || "Unknown",
          rowData?.type || "",
        );
      }}
    >
      {String(value)}
    </Link>
  );
};
