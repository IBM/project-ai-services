import React from "react";
import { Tag, Link, OverflowMenu, OverflowMenuItem } from "@carbon/react";
import {
  CheckmarkFilled,
  PauseOutline,
  ErrorFilled,
  InProgress,
  Delete,
} from "@carbon/icons-react";
import sharedStyles from "@/components/Table/table.shared.module.scss";

export const STATUS_CONFIG = {
  Initializing: {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  Downloading: {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  Deploying: {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  Deleting: {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  Running: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: sharedStyles.statusTagSuccess,
  },
  Stopped: {
    tagType: "gray" as const,
    icon: PauseOutline,
    className: sharedStyles.statusTagSecondary,
  },
  Error: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: sharedStyles.statusTagError,
  },
  // ── Data source connector statuses ──────────────────────────────────────────
  connected: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: sharedStyles.statusTagSuccess,
  },
  offline: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: sharedStyles.statusTagError,
  },
  // ── Application datasource statuses ─────────────────────────────────────────
  "up to date": {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: sharedStyles.statusTagSuccess,
  },
  "out of sync": {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: sharedStyles.statusTagError,
  },
  syncing: {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  // Transient state while the connector record is being deleted on the service pod
  "delete pending": {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  // Service pod was unreachable when the catalog last attempted to fetch sync state
  unknown: {
    tagType: "gray" as const,
    icon: ErrorFilled,
    className: sharedStyles.statusTagSecondary,
  },
} as const;

const DEFAULT_STATUS_CONFIG = {
  tagType: "gray" as const,
  icon: PauseOutline,
  className: sharedStyles.statusTagSecondary,
} as const;

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

export const StatusCell = ({ value }: SharedCellRendererProps) => {
  const status = String(value);
  const config =
    STATUS_CONFIG[status as keyof typeof STATUS_CONFIG] ??
    DEFAULT_STATUS_CONFIG;

  return (
    <Tag
      type={config.tagType}
      size="md"
      renderIcon={config.icon}
      className={config.className}
    >
      {status}
    </Tag>
  );
};

export interface MessageCellProps extends SharedCellRendererProps {
  /**
   * Statuses that indicate a clean/healthy state — message is suppressed when
   * the row's status is in this list (or when the message is empty).
   * Defaults to ["Running"] to preserve existing behaviour for the deployments table.
   */
  hideStatuses?: string[];
  /**
   * Statuses that warrant the error icon instead of the in-progress icon.
   * Defaults to ["Error", "Offline"] to preserve existing behaviour for the deployments table.
   */
  errorStatuses?: string[];
}

export const MessageCell = ({
  value,
  rowData,
  hideStatuses = ["Running", "up to date"],
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
}: ActionCellProps) => {
  const deleteEnabled = isDeleteEnabled(rowData?.status);

  return (
    <OverflowMenu size="lg" flipped aria-label="Actions">
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
