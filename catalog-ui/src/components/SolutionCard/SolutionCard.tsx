import { Tag, IconButton, Tooltip } from "@carbon/react";
import {
  ArrowRight,
  AgricultureAnalytics,
  PiggyBank,
  IbmPlanningAnalytics,
  Umbrella,
  Help,
  Badge,
} from "@carbon/icons-react";
import styles from "./SolutionCard.module.scss";

export interface SolutionCardProps {
  id: string;
  title: string;
  description: string;
  tags: string[];
  category: string;
  onViewDetails?: (id: string) => void;
}

const categoryIcons: Record<
  string,
  React.ComponentType<{ size?: string | number }>
> = {
  Agriculture: AgricultureAnalytics,
  "Banking and Finance": PiggyBank,
  "Dev operations": Help,
  "Enterprise resource planning": IbmPlanningAnalytics,
  Healthcare: Help,
  Insurance: Umbrella,
  "IT operations": Help,
  "Professional services": Help,
  "Public sector": Help,
  "Real estates": Help,
};

const SolutionCard = ({
  id,
  title,
  description,
  tags,
  category,
  onViewDetails,
}: SolutionCardProps) => {
  const IconComponent = categoryIcons[category] || AgricultureAnalytics;
  const primaryTag = tags[0] || "Digital assistant";

  return (
    <div className={styles.card}>
      <div className={styles.cardHeader}>
        <div className={styles.iconContainer}>
          <IconComponent size={32} />
        </div>
        <Tooltip align="top" label="IBM certified">
          <button className={styles.indicatorIcon} type="button">
            <Badge size={16} />
          </button>
        </Tooltip>
      </div>

      <div className={styles.content}>
        <p className={styles.category}>{category}</p>
        <h3 className={styles.title}>{title}</h3>
        <p className={styles.description}>{description}</p>
      </div>

      <div className={styles.footer}>
        <Tag type="gray" size="sm">
          {primaryTag}
        </Tag>
        <IconButton
          kind="ghost"
          size="sm"
          label="View details"
          onClick={() => onViewDetails?.(id)}
        >
          <ArrowRight size={20} />
        </IconButton>
      </div>
    </div>
  );
};

export default SolutionCard;
