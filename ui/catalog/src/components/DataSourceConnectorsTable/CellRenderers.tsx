import React from "react";
import type { Dispatch } from "react";
import { OverflowMenu, OverflowMenuItem } from "@carbon/react";
import { Delete, Edit } from "@carbon/icons-react";
import type { AppAction } from "./types";
import { ACTION_TYPES } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import {
  StatusCell,
  MessageCell,
  NameCell as SharedNameCell,
} from "@/components/Table/components/CellRenderers";
import sharedStyles from "@/components/Table/table.shared.module.scss";
import styles from "./DataSourceConnectorsTable.module.scss";

export { StatusCell, MessageCell };

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; name?: string; services?: number | null };
}

const isOffline = (rowData?: { status?: string }) =>
  rowData?.status === "offline";

export const NameCell = ({ value, rowId, dispatch }: CellRendererProps) => (
  <SharedNameCell
    value={value}
    rowId={rowId}
    isLinkEnabled={true}
    onNameClick={(id) =>
      dispatch({
        type: ACTION_TYPES.OPEN_DETAILS_PANEL,
        payload: { id, mode: "view" },
      })
    }
  />
);

export const ServicesCell = ({ value }: Pick<CellRendererProps, "value">) => {
  const count = value as number | null;
  return <span>{count === null || count === 0 ? "-" : String(count)}</span>;
};

export const ActionCell = ({ rowId, rowData, dispatch }: CellRendererProps) => {
  // Disable Remove when the connector still has connected services
  const hasConnectedServices =
    typeof rowData?.services === "number" && rowData.services > 0;
  // Disable Update key when the connector is offline (design spec)
  const offline = isOffline(rowData);

  return (
    <OverflowMenu size="lg" flipped aria-label="Actions">
      <OverflowMenuItem
        itemText={
          <div className={styles.actionMenuItem}>
            <span>Update key</span>
            <Edit size={16} />
          </div>
        }
        disabled={offline}
        onClick={() =>
          dispatch({
            type: ACTION_TYPES.OPEN_DETAILS_PANEL,
            payload: { id: rowId, mode: "update-key" },
          })
        }
      />
      <OverflowMenuItem
        itemText={
          <div className={sharedStyles.deleteMenuItem}>
            <span>Remove</span>
            <Delete size={16} />
          </div>
        }
        isDelete
        disabled={hasConnectedServices}
        title={
          hasConnectedServices
            ? "Disconnect all services before removing"
            : undefined
        }
        onClick={() =>
          dispatch({ type: "SHARED_OPEN_DELETE_DIALOG", payload: rowId })
        }
      />
    </OverflowMenu>
  );
};

type RendererFn = (props: CellRendererProps) => React.ReactElement | null;

export const CELL_RENDERERS: Record<string, RendererFn> = {
  name: NameCell as RendererFn,
  status: StatusCell as RendererFn,
  services: ServicesCell as RendererFn,
  messages: MessageCell as RendererFn,
  actions: ActionCell as RendererFn,
};
