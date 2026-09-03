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
  Connected: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: sharedStyles.statusTagSuccess,
  },
  Offline: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: sharedStyles.statusTagError,
  },
  // ── Worker statuses ──────────────────────────────────────────────────────────
  ready: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: sharedStyles.statusTagSuccess,
  },
  pending: {
    tagType: "blue" as const,
    icon: InProgress,
    className: sharedStyles.statusTagInfo,
  },
  disconnected: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: sharedStyles.statusTagError,
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

export const MessageCell = ({ value, rowData }: SharedCellRendererProps) => {
  const message = String(value || "");
  const status = rowData?.status || "";

  // Hide message if status is Running or if message is empty
  if (status === "Running" || !message) {
    return <span></span>;
  }

  let MessageIcon;
  let iconClassName: string;

  // Error and Offline both indicate a hard failure — use the error icon
  if (status === "Error" || status === "Offline") {
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
