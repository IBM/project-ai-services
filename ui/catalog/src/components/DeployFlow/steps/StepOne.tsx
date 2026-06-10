import {
  TextInput,
  Dropdown,
  Grid,
  Column,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
} from "@carbon/react";
import { Information } from "@carbon/icons-react";
import styles from "../DeployFlow.module.scss";
import type { StepProps } from "../types";
export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  showNameError = false,
}) => {
  const isNameValid = !!formData.name.trim();
  // Extract version options from API response
  const versionOptions = [
    { id: deployOptions.version, text: deployOptions.version },
  ];

  // Extract embedding model options from global_components
  const embeddingComponent = deployOptions.global_components.find(
    (c) => c.type === "embedding",
  );
  const embeddingModelOptions =
    embeddingComponent?.providers.map((provider) => ({
      id: provider.id,
      text: provider.name,
    })) || [];

  // Extract vector store options from global_components
  const vectorStoreComponent = deployOptions.global_components.find(
    (c) => c.type === "vector_store",
  );
  const vectorStoreOptions =
    vectorStoreComponent?.providers.map((provider) => ({
      id: provider.id,
      text: provider.name,
    })) || [];

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      <div className={styles.formSection}>
        <Grid narrow className={styles.formGrid}>
          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <TextInput
                id="assistant-name"
                labelText="Name"
                placeholder="Digital assistant (copy)"
                value={formData.name}
                invalid={showNameError && !isNameValid}
                invalidText="Name is required"
                onChange={(e) => {
                  onChange({ name: e.target.value });
                }}
              />
            </div>
          </Column>

          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <Dropdown
                id="assistant-version"
                titleText="Digital assistant version"
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

          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <Dropdown
                id="embedding-model"
                titleText={
                  <div className={styles.labelWithInfo}>
                    <span>Embedding model</span>
                    <Toggletip align="top">
                      <ToggletipButton label="Additional information">
                        <Information />
                      </ToggletipButton>
                      <ToggletipContent>
                        <p>
                          For data recognition and categorization during
                          document digitization
                        </p>
                      </ToggletipContent>
                    </Toggletip>
                  </div>
                }
                label="Select embedding model"
                items={embeddingModelOptions}
                itemToString={(item) => (item ? item.text : "")}
                selectedItem={
                  embeddingModelOptions.find(
                    (m) => m.id === formData.embeddingModel,
                  ) || null
                }
                onChange={({ selectedItem }) =>
                  onChange({ embeddingModel: selectedItem?.id || "" })
                }
              />
            </div>
          </Column>

          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <Dropdown
                id="vector-store"
                titleText="Vector store"
                label="Select vector store"
                items={vectorStoreOptions}
                itemToString={(item) => (item ? item.text : "")}
                selectedItem={
                  vectorStoreOptions.find(
                    (v) => v.id === formData.vectorStore,
                  ) || null
                }
                onChange={({ selectedItem }) =>
                  onChange({ vectorStore: selectedItem?.id || "" })
                }
              />
            </div>
          </Column>
        </Grid>
      </div>
    </>
  );
};
