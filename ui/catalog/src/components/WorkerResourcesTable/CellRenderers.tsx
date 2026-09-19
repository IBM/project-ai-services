import type { Dispatch } from "react";
import { OverflowMenu, OverflowMenuItem } from "@carbon/react";
import { Delete, ErrorFilled } from "@carbon/icons-react";
import type { AppAction } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import {
  StatusCell,
  NameCell as SharedNameCell,
} from "@/components/Table/components/CellRenderers";
import sharedStyles from "@/components/Table/table.shared.module.scss";
import styles from "./WorkerResourcesTable.module.scss";
export { StatusCell };

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; name?: string };
}

export const RuntimeTypeCell = ({
  value,
}: CellRendererProps): React.ReactElement => {
  return <span>{String(value ?? "")}</span>;
};

export const MessageCell = ({
  value,
  rowData,
}: CellRendererProps): React.ReactElement => {
  const isDisconnected = rowData?.status === "disconnected";
  return (
    <span className={isDisconnected ? styles.messageCellError : undefined}>
      {isDisconnected && (
        <ErrorFilled size={16} className={styles.messageErrorIcon} />
      )}
      {String(value ?? "")}
    </span>
  );
};

export const NameCell = ({ value, rowId }: CellRendererProps) => (
  <SharedNameCell value={value} rowId={rowId} isLinkEnabled={false} />
);

export const ActionCell = ({ rowId, dispatch }: CellRendererProps) => (
  <OverflowMenu size="lg" flipped aria-label="Actions">
    <OverflowMenuItem
      itemText={
        <div className={sharedStyles.deleteMenuItem}>
          <span>Deregister</span>
          <Delete size={16} />
        </div>
      }
      isDelete
      onClick={() =>
        dispatch({ type: "SHARED_OPEN_DELETE_DIALOG", payload: rowId })
      }
    />
  </OverflowMenu>
);

type RendererFn = (props: CellRendererProps) => React.ReactElement | null;

export const CELL_RENDERERS: Record<string, RendererFn> = {
  name: NameCell as RendererFn,
  status: StatusCell as RendererFn,
  runtime_label: RuntimeTypeCell as RendererFn,
  message: MessageCell as RendererFn,
  actions: ActionCell,
};
