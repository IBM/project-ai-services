import {
  TableCell,
  Link,
  Tag,
  OverflowMenu,
  OverflowMenuItem,
} from "@carbon/react";
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
import styles from "./AiDeployments.module.scss";

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
  "Deploying...": {
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
  selectedRowId?: string | null;
}

export const ActionCell = ({ rowId, dispatch }: CellRendererProps) => (
  <OverflowMenu size="lg" flipped aria-label="Actions">
    <OverflowMenuItem
      itemText={
        <div className={styles.deleteMenuItem}>
          <span>Delete</span>
          <Delete size={16} />
        </div>
      }
      isDelete
      onClick={() => {
        dispatch({
          type: ACTION_TYPES.OPEN_DELETE_DIALOG,
          payload: rowId,
        });
      }}
    />
  </OverflowMenu>
);

export const NameCell = ({ value }: CellRendererProps) => (
  <Link href="#">{String(value)}</Link>
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

  const isError = message.toLowerCase().includes("error");
  const MessageIcon = isError ? ErrorFilled : InProgress;

  return (
    <div className={styles.messageWithIcon}>
      <MessageIcon
        size={16}
        className={isError ? styles.messageIconError : styles.messageIconInfo}
      />
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

// Generic cell renderer wrapper
interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction>;
  selectedRowId?: string | null;
  cellKey: string;
  cellProps: Record<string, unknown>;
}

export const renderCell = ({
  header,
  value,
  rowId,
  dispatch,
  selectedRowId,
  cellKey,
  cellProps,
}: RenderCellProps) => {
  const CellRenderer = CELL_RENDERERS[header as keyof typeof CELL_RENDERERS];

  return (
    <TableCell key={cellKey} {...cellProps}>
      {CellRenderer ? (
        <CellRenderer
          value={value}
          rowId={rowId}
          dispatch={dispatch}
          selectedRowId={selectedRowId}
        />
      ) : (
        String(value || "")
      )}
    </TableCell>
  );
};
