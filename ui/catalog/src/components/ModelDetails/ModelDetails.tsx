import {
  Button,
  Column,
  Grid,
  InlineNotification,
  SkeletonText,
  Tag,
  TextInput,
} from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import { Badge } from "@carbon/icons-react";
import DetailsLayout from "@/components/DetailsLayout";
import StatusTag from "@/components/StatusTag";
import { WORKER_RUNTIME_LABELS } from "@/constants";
import { useModelDetails } from "@/hooks/useModelDetails";
import { useGenerateModelKey } from "@/hooks/useGenerateModelKey";
import GenerateKeyModal from "./GenerateKeyModal";
import styles from "./ModelDetails.module.scss";

// Shown when a value is absent: either not returned by the backend yet (see the TODOs at each use) or genuinely empty (e.g. no worker for remote models)
const PLACEHOLDER = "—";

export interface ModelDetailsProps {
  modelId: string;
  modelName: string;
  onBack: () => void;
}

interface DetailRowProps {
  label: string;
  value: string | number;
}

// A label/value pair in a details card
const DetailRow = ({ label, value }: DetailRowProps) => (
  <div className={styles.detailRow}>
    <span className={styles.detailLabel}>{label}</span>
    <span className={styles.detailValue}>{value}</span>
  </div>
);

// Detail screen for a single model deployment
const ModelDetails = ({ modelId, modelName, onBack }: ModelDetailsProps) => {
  const { detail, certifiedBy, isLoading, error } = useModelDetails(modelId);
  const keyGen = useGenerateModelKey(modelId);

  const runtimeType = detail?.worker?.runtime_type;
  const deploymentType = runtimeType
    ? (WORKER_RUNTIME_LABELS[runtimeType.toLowerCase()]?.label ?? runtimeType)
    : PLACEHOLDER;
  const modelEndpoint = detail?.endpoints?.[0]?.url;
  // TODO: PLACEHOLDER is shown because GET /models/{id} does not return profile yet; remove the fallback once it does
  const hardwareSize = detail?.profile
    ? `${detail.profile.name} (${detail.profile.description})`
    : PLACEHOLDER;

  const renderContent = () => {
    if (error) {
      return (
        <InlineNotification
          kind="error"
          role="alert"
          title="Failed to load model details:"
          subtitle={error}
          lowContrast
          hideCloseButton
        />
      );
    }

    if (isLoading || !detail) {
      return <SkeletonText paragraph lineCount={6} />;
    }

    return (
      <Grid className={styles.cardsGrid}>
        <Column sm={4} md={8} lg={16}>
          <ProductiveCard title="Details" className={styles.card}>
            <div className={styles.deploymentName}>
              <TextInput
                id="model-deployment-name"
                labelText="Deployment name"
                value={detail.name}
                readOnly
              />
            </div>
            <div className={styles.detailRows}>
              <DetailRow
                label="Type"
                value={detail.component_type.name || detail.component_type.id}
              />
              <DetailRow
                label="Worker resource"
                value={detail.worker?.name ?? PLACEHOLDER}
              />
              <DetailRow label="Deployment type" value={deploymentType} />
              <DetailRow label="Hardware size" value={hardwareSize} />
              <DetailRow
                label="Inference backend"
                value={detail.provider.name}
              />
              {/* TODO: PLACEHOLDER is shown because the API does not return the sharing status yet; replace it with the real value once it does */}
              <DetailRow
                label="Available to other services"
                value={PLACEHOLDER}
              />
              <DetailRow
                label="Services using this model-backend"
                value={detail.connected_services}
              />
            </div>
          </ProductiveCard>
        </Column>

        <Column sm={4} md={8} lg={16}>
          <ProductiveCard title="Integration endpoints" className={styles.card}>
            <div className={styles.endpointBlock}>
              <div className={styles.detailRow}>
                <span className={styles.detailLabel}>Model endpoint</span>
                <span className={styles.detailValue}>
                  {modelEndpoint ? (
                    <a
                      href={modelEndpoint}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={styles.link}
                    >
                      {modelEndpoint}
                    </a>
                  ) : (
                    PLACEHOLDER
                  )}
                </span>
              </div>
              <div>
                <Button
                  kind="tertiary"
                  size="md"
                  onClick={keyGen.open}
                  disabled={keyGen.isGenerating}
                >
                  Generate API key
                </Button>
              </div>
            </div>
          </ProductiveCard>
        </Column>

        <Column sm={4} md={8} lg={16}>
          <ProductiveCard title="Services" className={styles.card}>
            <div className={styles.detailRows}>
              {(detail.applications ?? []).length === 0 ? (
                <span className={styles.detailValue}>
                  No services are using this model.
                </span>
              ) : (
                (detail.applications ?? []).map((application) => (
                  <DetailRow
                    key={application.id}
                    label={application.name}
                    value="Application"
                  />
                ))
              )}
            </div>
          </ProductiveCard>
        </Column>
      </Grid>
    );
  };

  const title = detail?.name ?? modelName;

  return (
    <>
      <DetailsLayout
        backLabel="Models"
        onBack={onBack}
        title={title}
        titleAsText={title}
        subtitle={
          detail && (
            <>
              <StatusTag status={detail.status} />
              <Tag type="gray">{detail.model}</Tag>
              {certifiedBy && (
                <span className={styles.certifiedBadge}>
                  <Badge size={16} className={styles.badgeIcon} />
                  <span className={styles.badgeName}>
                    {certifiedBy} certified
                  </span>
                </span>
              )}
            </>
          )
        }
        navItems={[{ key: "details", label: "Details" }]}
        activeKey="details"
      >
        {renderContent()}
      </DetailsLayout>

      <GenerateKeyModal
        isOpen={keyGen.isOpen}
        isGenerating={keyGen.isGenerating}
        apiKey={keyGen.apiKey}
        error={keyGen.error}
        onConfirm={keyGen.generate}
        onRetry={keyGen.generate}
        onClose={keyGen.close}
      />
    </>
  );
};

export default ModelDetails;
