import { Toggletip, ToggletipButton, ToggletipContent } from "@carbon/react";
import { Information } from "@carbon/icons-react";
import styles from "../ServicesDeployFlow.module.scss";

interface ServiceFieldLabelProps {
  label: string;
  description?: string;
  className?: string;
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
}) => {
  // If no description, return plain label
  if (!description) {
    return <span className={className}>{label}</span>;
  }

  // Return label with info tooltip
  return (
    <div className={`${styles.serviceLabelWithInfo} ${className || ""}`}>
      <span>{label}</span>
      <Toggletip align="top">
        <ToggletipButton label="Additional information">
          <Information />
        </ToggletipButton>
        <ToggletipContent>
          <p>{description}</p>
        </ToggletipContent>
      </Toggletip>
    </div>
  );
};

// Made with Bob
