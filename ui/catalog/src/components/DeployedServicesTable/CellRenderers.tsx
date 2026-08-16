import type { Dispatch } from "react";
import type { AppAction } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import type { DeploymentDetails } from "@/types/api.types";
import {
  ActionCell as SharedActionCell,
  NameCell as SharedNameCell,
  StatusCell,
  MessageCell,
} from "@/components/Table/components/CellRenderers";

export { StatusCell, MessageCell };

interface ActionCellWrapperProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string };
}

export const ActionCell = ({
  rowId,
  dispatch,
  rowData,
}: ActionCellWrapperProps) => (
  <SharedActionCell
    rowId={rowId}
    rowData={rowData}
    onDelete={(id) =>
      dispatch({ type: "SHARED_OPEN_DELETE_DIALOG", payload: id })
    }
    isDeleteEnabled={(status) => status !== "Deleting"}
  />
);

interface NameCellWrapperProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; type?: string };
  onRowClick?: (deployment: DeploymentDetails) => void;
}

export const NameCell = ({
  value,
  rowId,
  rowData,
  onRowClick,
}: NameCellWrapperProps) => (
  <SharedNameCell
    value={value}
    rowId={rowId}
    rowData={rowData}
    onNameClick={
      onRowClick
        ? (id, name, status, type) =>
            onRowClick({
              id,
              name,
              status,
              type: type || "Service",
              resources: [],
            })
        : undefined
    }
  />
);

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; type?: string };
  onRowClick?: (deployment: DeploymentDetails) => void;
}

type CellRendererComponent = (
  props: CellRendererProps,
) => React.ReactElement | null;

export const CELL_RENDERERS: Record<string, CellRendererComponent> = {
  actions: ActionCell,
  name: NameCell,
  status: StatusCell,
  messages: MessageCell,
};
