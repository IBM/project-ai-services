import { Link, Tag, OverflowMenu, OverflowMenuItem } from "@carbon/react";
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
import styles from "./DigitalAssistants.module.scss";

// Status configuration
const STATUS_CONFIG = {
  Initializing: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Downloading: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Deploying: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Running: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: styles.statusTagSuccess,
  },
  Deleting: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Error: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: styles.statusTagError,
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
  rowData?: { status?: string; type?: string };
}

export const ActionCell = ({ rowId, dispatch, rowData }: CellRendererProps) => {
  const canDelete =
    rowData?.status === "Running" || rowData?.status === "Error";

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
        disabled={!canDelete}
        onClick={() => {
          dispatch({
            type: ACTION_TYPES.OPEN_DELETE_DIALOG,
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
  dispatch,
  rowData,
}: CellRendererProps) => (
  <Link
    href="#"
    onClick={(e: React.MouseEvent<HTMLAnchorElement>) => {
      e.preventDefault();
      dispatch({
        type: ACTION_TYPES.SHOW_DEPLOYMENT_DETAILS,
        payload: {
          id: rowId,
          name: String(value),
          status: rowData?.status || "Unknown",
          type: rowData?.type || "Digital assistant",
          resources: [],
        },
      });
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

export const MessageCell = ({ value }: CellRendererProps) => {
  const message = String(value || "");

  if (!message) {
    return <span>{message}</span>;
  }

  // Check message content to determine appropriate icon
  const messageLower = message.toLowerCase();
  const isError =
    messageLower.includes("error") || messageLower.includes("failed");
  const isSuccess =
    messageLower.includes("success") || messageLower.includes("completed");

  let MessageIcon;
  let iconClass;

  if (isError) {
    MessageIcon = ErrorFilled;
    iconClass = styles.messageIconError;
  } else if (isSuccess) {
    MessageIcon = CheckmarkFilled;
    iconClass = styles.messageIconSuccess;
  } else {
    MessageIcon = InProgress;
    iconClass = styles.messageIconInfo;
  }

  return (
    <div className={styles.messageWithIcon}>
      <MessageIcon size={16} className={iconClass} />
      <span>{message}</span>
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
