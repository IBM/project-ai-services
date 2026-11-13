import React from "react";
import { Tag } from "@carbon/react";
import type { Dispatch } from "react";
import type { AppAction, ModelDeploymentRow } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import {
  ActionCell as SharedActionCell,
  NameCell as SharedNameCell,
  StatusCell,
  MessageCell,
} from "@/components/Table/components/CellRenderers";
import { DEPLOYMENT_STATUS } from "@/constants/app.constants";
import styles from "./ModelsTable.module.scss";

export { StatusCell, MessageCell };

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
    isDeleteEnabled={(status) =>
      status === DEPLOYMENT_STATUS.RUNNING || status === DEPLOYMENT_STATUS.ERROR
    }
  />
);

// Renders component_type as a gray square tag (e.g. "LLM", "Embedding")
export const TypeCell = ({ value }: CellRendererProps): React.ReactElement => {
  const label = String(value ?? "");
  return (
    <Tag type="gray" size="sm" className={styles.squareTag}>
      {label}
    </Tag>
  );
};

// Renders services count as a gray square tag (e.g. 2, 3)
export const ServicesCountCell = ({
  value,
}: CellRendererProps): React.ReactElement => {
  const count = typeof value === "number" ? value : Number(value ?? 0);
  return (
    <Tag type="gray" size="sm" className={styles.squareTag}>
      {count}
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
  createdAt: TextCell,
  model: TextCell,
  componentType: TypeCell,
  backend: TextCell,
  workerResource: TextCell,
  servicesCount: ServicesCountCell,
  messages: MessageCell as CellRendererComponent,
  actions: ActionCell,
};
