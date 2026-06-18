import { Tag, Tooltip, ClickableTile } from "@carbon/react";
import {
  ArrowRight,
  AgricultureAnalytics,
  PiggyBank,
  IbmPlanningAnalytics,
  Umbrella,
  Badge,
  IbmZOpenEditor,
  Stethoscope,
  SettingsServices,
  UserService,
  UserMultiple,
  Hotel,
  Industry,
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
  "Banking & Finance": PiggyBank,
  "Dev operations": IbmZOpenEditor,
  "Enterprise resource planning": IbmPlanningAnalytics,
  Healthcare: Stethoscope,
  Insurance: Umbrella,
  "IT operations": SettingsServices,
  "Professional services": UserService,
  "Public sector": UserMultiple,
  "Real estate": Hotel,
  Manufacturing: Industry,
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

  return (
    <ClickableTile className={styles.card} onClick={() => onViewDetails?.(id)}>
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
        <div className={styles.tags}>
          {tags.map((tag, index) => (
            <Tag type="gray" size="md" key={index}>
              {tag}
            </Tag>
          ))}
        </div>
        <ArrowRight size={20} />
      </div>
    </ClickableTile>
  );
};

export default SolutionCard;
