import React from "react";
import { Tag } from "@carbon/react";
import type { Dispatch } from "react";
import type { AppAction, ModelDeploymentRow } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import {
  ActionCell as SharedActionCell,
  NameCell as SharedNameCell,
  StatusCell,
} from "@/components/Table/components/CellRenderers";

export { StatusCell };

interface CellRendererProps {
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  rowData?: Partial<ModelDeploymentRow>;
  onRowClick?: (row: ModelDeploymentRow) => void;
}

type CellRendererComponent = (
  props: CellRendererProps,
) => React.ReactElement | null;

// Renders the instance name as a link that opens the model details panel
export const NameCell = ({
  value,
  rowId,
  rowData,
  onRowClick,
}: CellRendererProps) => (
  <SharedNameCell
    value={value}
    rowId={rowId}
    rowData={rowData}
    isLinkEnabled={!!onRowClick}
    onNameClick={
      onRowClick && rowData
        ? () => onRowClick(rowData as ModelDeploymentRow)
        : undefined
    }
  />
);

export const ActionCell = ({ rowId, rowData }: CellRendererProps) => (
  <SharedActionCell
    rowId={rowId}
    rowData={rowData}
    onDelete={() => {
      /* delete not yet wired */
    }}
    isDeleteEnabled={() => false}
  />
);

// Renders component_type as an outlined pill tag (e.g. "LLM", "Embedding")
export const TypeCell = ({ value }: CellRendererProps): React.ReactElement => {
  const label = String(value ?? "").toUpperCase();
  return (
    <Tag type="outline" size="sm">
      {label}
    </Tag>
  );
};

// Plain-text cell for columns with no special rendering
export const TextCell = ({ value }: CellRendererProps): React.ReactElement => (
  <span>{String(value ?? "")}</span>
);

export const CELL_RENDERERS: Record<string, CellRendererComponent> = {
  name: NameCell,
  status: StatusCell,
  model: TextCell,
  componentType: TypeCell,
  backend: TextCell,
  workerResource: TextCell,
  servicesCount: TextCell,
  actions: ActionCell,
};
