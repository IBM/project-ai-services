import { useEffect } from "react";
import {
  TextInput,
  Dropdown,
  Grid,
  Column,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
  InlineNotification,
} from "@carbon/react";
import { Information } from "@carbon/icons-react";
import { formatVersion } from "@/utils/string";
import styles from "../DeployFlow.shared.module.scss";
import type { DeployFormData } from "../types";

export interface StepOneComponentRow {
  type: string;
  name: string;
  hasModels: boolean; // true → model dropdown; false → provider dropdown
  modelOptions: Array<{ id: string; text: string }>;
  selectedModel: string;
  providerOptions: Array<{ id: string; text: string }>;
  selectedProviderId: string;
  description?: string;
}

export interface SharedStepOneProps {
  title: string;
  formData: DeployFormData;
  onChange: (updates: Partial<DeployFormData>) => void;
  version: string;
  versionLabel: string;
  components: StepOneComponentRow[];
  onComponentChange: (componentType: string, providerId: string) => void;
  onModelChange?: (componentType: string, model: string) => void;
  showNameError?: boolean;
  failedComponentNames?: string[]; // Non-empty → renders an error banner listing the failed component names.
  onComponentError?: (hasError: boolean) => void;
}

export const SharedStepOne = ({
  title,
  formData,
  onChange,
  version,
  versionLabel,
  components,
  onComponentChange,
  onModelChange,
  showNameError = false,
  failedComponentNames = [],
  onComponentError,
}: SharedStepOneProps) => {
  const isNameValid = !!formData.name.trim();
  const versionOptions = [{ id: version, text: formatVersion(version) }];

  useEffect(() => {
    onComponentError?.(failedComponentNames.length > 0);
  }, [failedComponentNames, onComponentError]);

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      {failedComponentNames.length > 0 && (
        <InlineNotification
          kind="error"
          title={`Failed to load configurations of ${failedComponentNames.join(", ")}.`}
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      <div className={styles.formSection}>
        <Grid narrow className={styles.formGrid}>
          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <TextInput
                id="deploy-name"
                labelText="Name"
                value={formData.name}
                invalid={showNameError && !isNameValid}
                invalidText="Name is required"
                onChange={(e) => onChange({ name: e.target.value })}
              />
            </div>
          </Column>

          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <Dropdown
                id="deploy-version"
                titleText={versionLabel}
                label="Select version"
                items={versionOptions}
                itemToString={(item) => (item ? item.text : "")}
                selectedItem={
                  versionOptions.find((v) => v.id === formData.version) || null
                }
                onChange={({ selectedItem }) =>
                  onChange({ version: selectedItem?.id || "" })
                }
              />
            </div>
          </Column>

          {components.map((component) => {
            const labelNode = component.description ? (
              <div className={styles.labelWithInfo}>
                <span>{component.name}</span>
                <Toggletip align="top">
                  <ToggletipButton label="Additional information">
                    <Information />
                  </ToggletipButton>
                  <ToggletipContent>
                    <p>{component.description}</p>
                  </ToggletipContent>
                </Toggletip>
              </div>
            ) : (
              component.name
            );

            const dropdownProps = component.hasModels
              ? {
                  id: `${component.type}-model`,
                  items: component.modelOptions,
                  selectedItem:
                    component.modelOptions.find(
                      (m) => m.id === component.selectedModel,
                    ) || null,
                  onChange: ({
                    selectedItem,
                  }: {
                    selectedItem: { id: string; text: string } | null;
                  }) => onModelChange?.(component.type, selectedItem?.id || ""),
                }
              : {
                  id: `${component.type}-provider`,
                  items: component.providerOptions,
                  selectedItem:
                    component.providerOptions.find(
                      (p) => p.id === component.selectedProviderId,
                    ) || null,
                  onChange: ({
                    selectedItem,
                  }: {
                    selectedItem: { id: string; text: string } | null;
                  }) =>
                    onComponentChange(component.type, selectedItem?.id || ""),
                };

            return (
              <Column key={component.type} sm={4} md={8} lg={16}>
                <div className={styles.formField}>
                  <Dropdown
                    {...dropdownProps}
                    titleText={labelNode}
                    label={`Select ${component.name.toLowerCase()}`}
                    itemToString={(item) => (item ? item.text : "")}
                  />
                </div>
              </Column>
            );
          })}
        </Grid>
      </div>
    </>
  );
};
