import {
  Accordion,
  AccordionItem,
  Checkbox,
  CheckboxGroup,
  Button,
} from "@carbon/react";
import styles from "./FilterPanel.module.scss";

export interface FilterOption {
  id: string;
  /** Display text; defaults to `id` when omitted. */
  label?: string;
}

export interface FilterGroupConfig {
  /** Unique key for the group; also used as the checkbox id prefix. */
  id: string;
  title: string;
  options: FilterOption[];
  selected: string[];
  onToggle: (id: string) => void;
}

export interface FilterPanelProps {
  title: string;
  ariaLabel?: string;
  groups: FilterGroupConfig[];
  onReset: () => void;
}

const FilterGroup = ({
  id,
  title,
  options,
  selected,
  onToggle,
}: FilterGroupConfig) => (
  <AccordionItem title={title} open className={styles.group}>
    {options.length === 0 ? (
      <p className={styles.emptyMessage}>No options available</p>
    ) : (
      <CheckboxGroup
        legendText={<span className="cds--visually-hidden">{title}</span>}
      >
        {options.map((option) => (
          <Checkbox
            key={option.id}
            id={`filter-${id}-${option.id}`}
            labelText={option.label ?? option.id}
            checked={selected.includes(option.id)}
            onChange={() => onToggle(option.id)}
            className={styles.checkbox}
          />
        ))}
      </CheckboxGroup>
    )}
  </AccordionItem>
);

// Created Custom panel on purpose as @carbon/ibm-products FilterPanel is deprecated .
const FilterPanel = ({
  title,
  ariaLabel = title,
  groups,
  onReset,
}: FilterPanelProps) => {
  const hasActiveFilters = groups.some((group) => group.selected.length > 0);

  return (
    <aside className={styles.panel} aria-label={ariaLabel}>
      <div className={styles.heading}>
        <h2 className={styles.title}>{title}</h2>
      </div>
      <div className={styles.content}>
        <Accordion size="lg" isFlush align="start" className={styles.accordion}>
          {groups.map((group) => (
            <FilterGroup key={group.id} {...group} />
          ))}
        </Accordion>
      </div>
      <Button
        className={styles.resetButton}
        kind="secondary"
        size="lg"
        disabled={!hasActiveFilters}
        onClick={onReset}
      >
        Reset filters
      </Button>
    </aside>
  );
};

export default FilterPanel;
