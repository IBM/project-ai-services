import type { Dispatch, ReactElement } from "react";
import { Button } from "@carbon/react";
import { TrashCan } from "@carbon/icons-react";
import type { AppAction } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import {
  StatusCell,
  MessageCell,
} from "@/components/Table/components/CellRenderers";

export { StatusCell, MessageCell };

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; name?: string };
}

type RendererFn = (props: CellRendererProps) => ReactElement | null;

export const FilesCell = ({ value }: Pick<CellRendererProps, "value">) => (
  <span>{String(value ?? "")}</span>
);

export const LastSyncCell = ({ value }: Pick<CellRendererProps, "value">) => (
  <span>{String(value ?? "")}</span>
);

export const DeleteCell = () => (
  <Button
    hasIconOnly
    kind="ghost"
    size="sm"
    renderIcon={TrashCan}
    iconDescription="Delete"
  />
);

export const CELL_RENDERERS: Record<string, RendererFn> = {
  status: StatusCell as RendererFn,
  files: FilesCell as RendererFn,
  last_sync: LastSyncCell as RendererFn,
  messages: MessageCell as RendererFn,
  actions: DeleteCell as RendererFn,
};
