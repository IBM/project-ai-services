import type { Dispatch, ReactElement } from "react";
import type { AppAction } from "./types";
import { ACTION_TYPES } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
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
  onMenuOpen?: (rowId: string) => Promise<void>;
  onViewIntegration?: (rowId: string) => void;
  onLaunchEndpoint?: (rowId: string) => void;
}

export const ActionCell = ({
  rowId,
  dispatch,
  rowData,
  onMenuOpen,
  onViewIntegration,
  onLaunchEndpoint,
}: ActionCellWrapperProps) => (
  <SharedActionCell
    rowId={rowId}
    rowData={rowData}
    onDelete={(id) =>
      dispatch({ type: "SHARED_OPEN_DELETE_DIALOG", payload: id })
    }
    isDeleteEnabled={(status) => status !== "Deleting"}
    onMenuOpen={onMenuOpen}
    onViewIntegration={onViewIntegration}
    onLaunchEndpoint={onLaunchEndpoint}
  />
);

interface NameCellWrapperProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; type?: string };
}

export const NameCell = ({
  value,
  rowId,
  dispatch,
  rowData,
}: NameCellWrapperProps) => (
  <SharedNameCell
    value={value}
    rowId={rowId}
    rowData={rowData}
    isLinkEnabled={rowData?.status === "Running"}
    onNameClick={(id, name, status, type) =>
      dispatch({
        type: ACTION_TYPES.SHOW_DEPLOYMENT_DETAILS,
        payload: {
          id,
          name,
          status,
          type: type || "Digital assistant",
        },
        defaultSection: "details",
      })
    }
  />
);

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: { status?: string; type?: string };
  onMenuOpen?: (rowId: string) => Promise<void>;
  onViewIntegration?: (rowId: string) => void;
  onLaunchEndpoint?: (rowId: string) => void;
}

type CellRendererComponent = (props: CellRendererProps) => ReactElement | null;

export const WorkerTypeCell = ({ value }: CellRendererProps): ReactElement => {
  return <span>{String(value ?? "")}</span>;
};

export const CELL_RENDERERS: Record<string, CellRendererComponent> = {
  actions: ActionCell,
  name: NameCell,
  status: StatusCell,
  workerType: WorkerTypeCell,
  messages: MessageCell,
};
