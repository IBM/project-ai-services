import { Tag, OverflowMenu, OverflowMenuItem, Link } from "@carbon/react";
import {
  Delete,
  CheckmarkFilled,
  PauseOutline,
  ErrorFilled,
  InProgress,
} from "@carbon/icons-react";
import type { Dispatch } from "react";
import type { AppAction } from "./types";
import { ACTION_TYPES } from "./types";
import styles from "./DeployedServices.module.scss";
import type { DeploymentDetails } from "@/types/digitalAssistants";

// Status configuration
const STATUS_CONFIG = {
  Running: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: styles.statusTagSuccess,
  },
  Error: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: styles.statusTagError,
  },
  Stopped: {
    tagType: "gray" as const,
    icon: PauseOutline,
    className: styles.statusTagSecondary,
  },

  Deploying: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Downloading: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  "Deleting...": {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
} as const;

const DEFAULT_STATUS_CONFIG = {
  tagType: "gray" as const,
  icon: PauseOutline,
  className: styles.statusTagSecondary,
} as const;

// Cell Renderer Components
interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction>;
  rowData?: { status: string; name?: string; type?: string };
  onRowClick?: (deployment: DeploymentDetails) => void;
}

export const ActionCell = ({ rowId, dispatch, rowData }: CellRendererProps) => {
  // Enable delete button only for "error" or "running" status
  const status = rowData?.status?.toLowerCase() || "";
  const isDeleteEnabled = status === "error" || status === "running";

  return (
    <OverflowMenu size="lg" flipped aria-label="Actions">
      <OverflowMenuItem
        itemText={
          <div className={styles.deleteMenuItem}>
            <span>Delete</span>
            <Delete size={16} />
          </div>
        }
        isDelete
        disabled={!isDeleteEnabled}
        onClick={() => {
          dispatch({
            type: ACTION_TYPES.DEPLOYED_SERVICES_OPEN_DELETE_DIALOG,
            payload: rowId,
          });
        }}
      />
    </OverflowMenu>
  );
};

export const NameCell = ({
  value,
  rowId,
  rowData,
  onRowClick,
}: CellRendererProps) => (
  <Link
    onClick={(e: React.MouseEvent<HTMLAnchorElement>) => {
      e.preventDefault();
      e.stopPropagation();
      if (onRowClick) {
        onRowClick({
          id: rowId,
          name: String(value),
          status: rowData?.status || "Unknown",
          type: rowData?.type || "Service",
          resources: [],
        });
      }
    }}
  >
    {String(value)}
  </Link>
);
export const StatusCell = ({ value }: CellRendererProps) => {
  const status = String(value);
  const config =
    STATUS_CONFIG[status as keyof typeof STATUS_CONFIG] ||
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

export const MessageCell = ({ value, rowData }: CellRendererProps) => {
  const message = String(value || "");
  const status = rowData?.status || "";

  // Don't show message if status is Running
  if (status === "Running" || !message) {
    return <span></span>;
  }

  const isError =
    message.toLowerCase().includes("error") ||
    message.toLowerCase().includes("failed");
  const isSuccess = message.toLowerCase().includes("completed successfully");

  let MessageIcon = InProgress;
  let iconClassName = styles.messageIconInfo;

  if (isError) {
    MessageIcon = ErrorFilled;
    iconClassName = styles.messageIconError;
  } else if (isSuccess) {
    MessageIcon = CheckmarkFilled;
    iconClassName = styles.messageIconSuccess;
  }

  return (
    <div className={styles.messageWithIcon}>
      <MessageIcon size={16} className={iconClassName} />
      <span className={styles.messageText}>{message}</span>
    </div>
  );
};

// Cell renderer mapping
export const CELL_RENDERERS = {
  actions: ActionCell,
  name: NameCell,
  status: StatusCell,
  messages: MessageCell,
} as const;
