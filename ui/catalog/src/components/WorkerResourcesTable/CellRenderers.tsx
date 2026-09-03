import type { Dispatch } from "react";
import { OverflowMenu, OverflowMenuItem } from "@carbon/react";
import { Delete } from "@carbon/icons-react";
import type { AppAction } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import {
  StatusCell,
  NameCell as SharedNameCell,
} from "@/components/Table/components/CellRenderers";
import sharedStyles from "@/components/Table/table.shared.module.scss";

export { StatusCell };

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; name?: string };
}

export const NameCell = ({ value, rowId }: CellRendererProps) => (
  <SharedNameCell value={value} rowId={rowId} isLinkEnabled={false} />
);

export const ActionCell = () => (
  <OverflowMenu size="lg" flipped aria-label="Actions">
    <OverflowMenuItem
      itemText={
        <div className={sharedStyles.deleteMenuItem}>
          <span>Deregister</span>
          <Delete size={16} />
        </div>
      }
      isDelete
    />
  </OverflowMenu>
);

type RendererFn = (props: CellRendererProps) => React.ReactElement | null;

export const MessageCell = ({ value }: CellRendererProps) => (
  <span>{String(value ?? "")}</span>
);

export const CELL_RENDERERS: Record<string, RendererFn> = {
  name: NameCell as RendererFn,
  status: StatusCell as RendererFn,
  messages: MessageCell as RendererFn,
  actions: ActionCell,
};
