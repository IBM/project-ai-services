import React from "react";
import type { Dispatch } from "react";
import { OverflowMenu, OverflowMenuItem } from "@carbon/react";
import { Delete, Edit } from "@carbon/icons-react";
import type { AppAction } from "./types";
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
  rowData?: { status?: string; name?: string };
}

export const NameCell = ({ value, rowId }: CellRendererProps) => (
  <SharedNameCell value={value} rowId={rowId} isLinkEnabled={true} />
);

export const ServicesCell = ({ value }: Pick<CellRendererProps, "value">) => {
  const count = value as number | null;
  return <span>{count === null || count === 0 ? "-" : String(count)}</span>;
};

export const ActionCell = () => (
  <OverflowMenu size="lg" flipped aria-label="Actions">
    <OverflowMenuItem
      itemText={
        <div className={styles.actionMenuItem}>
          <span>Update key</span>
          <Edit size={16} />
        </div>
      }
    />
    <OverflowMenuItem
      itemText={
        <div className={sharedStyles.deleteMenuItem}>
          <span>Delete</span>
          <Delete size={16} />
        </div>
      }
      isDelete
    />
  </OverflowMenu>
);

type RendererFn = (props: CellRendererProps) => React.ReactElement | null;

export const CELL_RENDERERS: Record<string, RendererFn> = {
  name: NameCell as RendererFn,
  status: StatusCell as RendererFn,
  services: ServicesCell as RendererFn,
  messages: MessageCell as RendererFn,
  actions: ActionCell,
};
