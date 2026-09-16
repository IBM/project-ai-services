import { Toggletip, ToggletipButton, ToggletipContent } from "@carbon/react";
import { Information } from "@carbon/icons-react";
import styles from "./AddDataSourceModal.module.scss";

/**
 * Shared field label with an optional inline Toggletip info icon.
 * Used by AddDataSourceModal and ConnectorDetailsPanel for Carbon
 * form inputs' labelText prop.
 */
const ConnectorFieldLabel = ({
  text,
  description,
}: {
  text: string;
  description?: string;
}) => (
  <div className={styles.labelWithInfo}>
    <span>{text}</span>
    {description && (
      <Toggletip align="top">
        <ToggletipButton label="Additional information">
          <Information />
        </ToggletipButton>
        <ToggletipContent>
          <p>{description}</p>
        </ToggletipContent>
      </Toggletip>
    )}
  </div>
);

export default ConnectorFieldLabel;
