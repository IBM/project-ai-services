import { Tag } from "@carbon/react";
import {
  CheckmarkFilled,
  PauseOutline,
  ErrorFilled,
  InProgress,
} from "@carbon/icons-react";
import styles from "./StatusTag.module.scss";

const STATUS_CONFIG = {
  Initializing: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Downloading: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Deploying: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  Deleting: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
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
  // ── Data source connector statuses ──────────────────────────────────────────
  connected: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: styles.statusTagSuccess,
  },
  offline: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: styles.statusTagError,
  },
  // ── Worker statuses ──────────────────────────────────────────────────────────
  ready: {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: styles.statusTagSuccess,
  },
  pending: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  disconnected: {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: styles.statusTagError,
  },
  // ── Application datasource statuses ─────────────────────────────────────────
  "up to date": {
    tagType: "green" as const,
    icon: CheckmarkFilled,
    className: styles.statusTagSuccess,
  },
  "out of sync": {
    tagType: "red" as const,
    icon: ErrorFilled,
    className: styles.statusTagError,
  },
  syncing: {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  // The backend API filters out "delete pending" rows before they reach the UI.
  // Kept as a fallback.
  "delete pending": {
    tagType: "blue" as const,
    icon: InProgress,
    className: styles.statusTagInfo,
  },
  // Service pod was unreachable when the catalog last attempted to fetch sync state
  unknown: {
    tagType: "gray" as const,
    icon: ErrorFilled,
    className: styles.statusTagSecondary,
  },
} as const;

const DEFAULT_STATUS_CONFIG = {
  tagType: "gray" as const,
  icon: PauseOutline,
  className: styles.statusTagSecondary,
} as const;

interface StatusTagProps {
  status: string;
  className?: string;
}

// Renders a status as a colour-coded Carbon tag with an icon
const StatusTag = ({ status, className }: StatusTagProps) => {
  const config =
    STATUS_CONFIG[status as keyof typeof STATUS_CONFIG] ??
    DEFAULT_STATUS_CONFIG;

  return (
    <Tag
      type={config.tagType}
      size="md"
      renderIcon={config.icon}
      className={
        className ? `${config.className} ${className}` : config.className
      }
    >
      {status}
    </Tag>
  );
};

export default StatusTag;
