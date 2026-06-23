import { Toggletip, ToggletipButton, ToggletipContent } from "@carbon/react";
import { Information } from "@carbon/icons-react";
import { parseMarkdownLinks } from "@/utils/string";
import styles from "../ServicesDeployFlow.module.scss";

interface ServiceFieldLabelProps {
  label: string;
  description?: string;
  className?: string;
  required?: boolean;
}

/**
 * ServiceFieldLabel component that displays a label with an optional tooltip.
 * When a description is provided, an information icon with a toggletip is shown.
 * This pattern is used throughout the services deployment flow for dynamic field labels.
 */
export const ServiceFieldLabel: React.FC<ServiceFieldLabelProps> = ({
  label,
  description,
  className,
  required = false,
}) => {
  // If no description, return plain label with optional required indicator
  if (!description) {
    return (
      <span className={className}>
        {label}
        {required && <span className={styles.requiredIndicator}> *</span>}
      </span>
    );
  }

  // Return label with info tooltip and optional required indicator
  return (
    <div className={`${styles.serviceLabelWithInfo} ${className || ""}`}>
      <span>
        {label}
        {required && <span className={styles.requiredIndicator}> *</span>}
      </span>
      <Toggletip align="top">
        <ToggletipButton label="Additional information">
          <Information />
        </ToggletipButton>
        <ToggletipContent>
          <p>{parseMarkdownLinks(description)}</p>
        </ToggletipContent>
      </Toggletip>
    </div>
  );
};

// Made with Bob
