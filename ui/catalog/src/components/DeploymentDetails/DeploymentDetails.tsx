import React, { useState, useEffect } from "react";
import { useDeploymentDatasourceSupport } from "./hooks/useDeploymentDatasourceSupport";
import {
  Grid,
  Column,
  Tag,
  ProgressBar,
  SkeletonText,
  SkeletonPlaceholder,
  ToastNotification,
  CopyButton,
} from "@carbon/react";
import ApplicationDatasourcesTable from "./components/ApplicationDatasourcesTable";
import { InlineEditableTitle, DetailsLayout, StatusTag } from "@/components";
import { ProductiveCard } from "@carbon/ibm-products";
import { Badge, DiamondFill } from "@carbon/icons-react";
import type {
  DeploymentDetails as DeploymentDetailsType,
  DeploymentServiceData,
  DeployIntegrationEndpoints,
  UsedResourcesResponse,
  ApplicationDetailsApiResponse,
  AcceleratorCards as AcceleratorCardType,
  ApplicationWorker,
  ResourceAllocation,
} from "@/types/api.types";
import { formatVersion } from "@/utils/string";
import styles from "./DeploymentDetails.module.scss";
import { api } from "@/api/axios";
import {
  APPLICATION_ENDPOINTS,
  SERVICE_ENDPOINTS,
  COMPONENT_TYPES,
  WORKER_RUNTIME_LABELS,
  PAGE_TITLES,
} from "@/constants";

interface DeploymentDetailsProps {
  deployment: DeploymentDetailsType;
  onBack: () => void;
  deploymentSource: string;
  onNameUpdate?: (newName: string) => void;
  /** Pre-select a side-nav section when the panel opens */
  defaultSection?: "details" | "services" | "integration" | "datasources";
}

const DeploymentDetails = ({
  deployment,
  onBack,
  deploymentSource,
  onNameUpdate,
  defaultSection = "details",
}: DeploymentDetailsProps) => {
  const [activeSection, setActiveSection] = useState(defaultSection);
  const [datasourceCount, setDatasourceCount] = useState<number | null>(null);
  const [resources, setResources] = useState<ResourceAllocation[]>([]);
  const [deployedCatalogIds, setDeployedCatalogIds] = useState<
    string[] | undefined
  >(undefined);
  const {
    acceptsDatasource,
    error: datasourceSupportError,
    clearError: clearDatasourceSupportError,
  } = useDeploymentDatasourceSupport(deploymentSource, deployedCatalogIds);
  const [isLoadingResources, setIsLoadingResources] = useState(false);
  const [serviceData, setServiceData] = useState<DeploymentServiceData[]>([]);
  const [integrationEndpoints, setIntegrationEndpoints] = useState<
    DeployIntegrationEndpoints[]
  >([]);
  const [integrationFetchError, setIntegrationFetchError] = useState<
    string | null
  >(null);
  const [copyError, setCopyError] = useState<string | null>(null);
  const [acceleratorCards, setAcceleratorCards] = useState<
    AcceleratorCardType[]
  >([]);
  const [certifiedBy, setCertifiedBy] = useState<string | null>(null);
  const [workerInfo, setWorkerInfo] = useState<ApplicationWorker | null>(null);
  const [testAssistantUrl, setTestAssistantUrl] = useState<string | null>(null);
  const isDigitalAssistant =
    deploymentSource === PAGE_TITLES.DIGITAL_ASSISTANTS;

  useEffect(() => {
    const fetchResources = async () => {
      setIsLoadingResources(true);
      setResources([]);

      try {
        const response = await api.get<UsedResourcesResponse>(
          APPLICATION_ENDPOINTS.GET_APPLICATION_RESOURCES(deployment.id),
        );

        const transformedResources = [
          {
            name: "Processors",
            used: response.data.cpu.used_cpu,
            allocated: response.data.cpu.total_cpu,
            unit: "vCPUs",
          },
          {
            name: "Memory",
            used:
              Math.round(
                (response.data.memory.used_bytes / (1024 * 1024 * 1024)) * 10,
              ) / 10,
            allocated:
              Math.round(
                (response.data.memory.total_bytes / (1024 * 1024 * 1024)) * 10,
              ) / 10,
            unit: "GB",
          },
        ];

        setResources(transformedResources);

        // Extract accelerator card IDs from the response
        if (
          response.data.accelerators &&
          Object.keys(response.data.accelerators).length > 0
        ) {
          // Get the array of PCI addresses from ibm.com/spyre_pf
          const spyreCards = response.data.accelerators["ibm.com/spyre_pf"];

          if (Array.isArray(spyreCards) && spyreCards.length > 0) {
            const cards: AcceleratorCardType[] = spyreCards.map((cardId) => ({
              id: cardId,
              label: cardId,
            }));
            setAcceleratorCards(cards);
          } else {
            setAcceleratorCards([]);
          }
        } else {
          setAcceleratorCards([]);
        }
      } catch (error) {
        console.error("Error fetching application resources:", error);
        setResources([]);
        setAcceleratorCards([]);
      } finally {
        setIsLoadingResources(false);
      }
    };

    fetchResources();
  }, [deployment.id]);

  useEffect(() => {
    setIntegrationFetchError(null);
    const fetchServiceDetails = async () => {
      try {
        const [applicationDetailsResponse, servicesResponse] =
          await Promise.all([
            api.get<ApplicationDetailsApiResponse>(
              APPLICATION_ENDPOINTS.GET_APPLICATION_DETAILS(deployment.id),
            ),
            api.get<
              | Array<{
                  id: string;
                  name: string;
                  description: string;
                  certified_by: string;
                  architectures: string[];
                }>
              | {
                  data: Array<{
                    id: string;
                    name: string;
                    description: string;
                    certified_by: string;
                    architectures: string[];
                  }>;
                }
            >(SERVICE_ENDPOINTS.GET_SERVICES),
          ]);

        const servicesCatalog = Array.isArray(servicesResponse.data)
          ? servicesResponse.data
          : servicesResponse.data.data;

        const serviceMetadataById = servicesCatalog.reduce<
          Record<string, { description: string; certifiedBy: string }>
        >((accumulator, service) => {
          accumulator[service.id] = {
            description: service.description,
            certifiedBy: service.certified_by,
          };
          return accumulator;
        }, {});

        const deploymentServices = applicationDetailsResponse.data.services;

        setDeployedCatalogIds(
          deploymentServices.map((s) => s.catalog_id).filter(Boolean),
        );

        const isDeploymentCertified =
          deployment.type === "Digital Assistant"
            ? deploymentServices.length > 0 &&
              deploymentServices.every(
                (service) =>
                  serviceMetadataById[service.catalog_id]?.certifiedBy ===
                  "IBM",
              )
            : deploymentServices.length > 0 &&
              serviceMetadataById[deploymentServices[0].catalog_id]
                ?.certifiedBy === "IBM";
        const knownComponentTypes = new Set<string>(
          Object.values(COMPONENT_TYPES),
        );

        const transformedServices: DeploymentServiceData[] =
          deploymentServices.map((service) => {
            const components = service.components ?? [];
            const llmComponent = components.find(
              (c) => c.type === COMPONENT_TYPES.LLM,
            );
            const embeddingComponent = components.find(
              (c) => c.type === COMPONENT_TYPES.EMBEDDING,
            );
            const vectorStoreComponent = components.find(
              (c) => c.type === COMPONENT_TYPES.VECTOR_STORE,
            );
            const rerankerComponent = components.find(
              (c) => c.type === COMPONENT_TYPES.RERANKER,
            );

            // Collect custom/unknown component types not handled by the known set.
            const customComponents = components
              .filter((c) => !knownComponentTypes.has(c.type))
              .map((c) => ({
                label: c.type
                  .split("_")
                  .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
                  .join(" "),
                model: c.metadata?.model,
                providerName: c.provider?.name,
              }));

            const serviceDescription =
              serviceMetadataById[service.catalog_id]?.description ??
              `${service.type} service`;

            // Inference backend is derived only from known component types.
            // Custom components already render their own row (with providerName),
            // so including them here would duplicate the value.
            const inferenceBackend =
              llmComponent?.provider?.name ||
              embeddingComponent?.provider?.name ||
              rerankerComponent?.provider?.name ||
              undefined;

            return {
              id: service.id,
              title:
                service.type.charAt(0).toUpperCase() + service.type.slice(1),
              description: serviceDescription,
              serviceVersion: service.version,
              largeLanguageModel: llmComponent?.metadata?.model,
              inferenceBackend,
              embeddingModel: embeddingComponent?.metadata?.model,
              vectorStore: vectorStoreComponent?.provider?.name,
              rankerModel: rerankerComponent?.metadata?.model,
              customComponents:
                customComponents.length > 0 ? customComponents : undefined,
            };
          });

        const transformedEndpoints: DeployIntegrationEndpoints[] =
          deploymentServices.map((service) => {
            const uiEndpoint = service.endpoints.find((e) => e.type === "ui");
            const apiEndpoint = service.endpoints.find((e) => e.type === "api");
            const mcpEndpoint = service.endpoints.find((e) => e.type === "mcp");
            const serviceDescription =
              serviceMetadataById[service.catalog_id]?.description ??
              `${service.type} service`;

            return {
              id: service.id,
              title:
                service.type.charAt(0).toUpperCase() + service.type.slice(1),
              description: serviceDescription,
              baseURL: uiEndpoint?.url || apiEndpoint?.url || "",
              apiDocumentation: apiEndpoint?.url
                ? `${apiEndpoint.url}/docs`
                : "",
              mcpEndpoint: mcpEndpoint?.url || "",
              interactiveAPIs: service.endpoints
                .filter((endpoint) => endpoint.type === "ui")
                .map((endpoint) => endpoint.url),
            };
          });

        const chatUiEndpoint = deploymentServices
          .find((s) => s.catalog_id === "chat")
          ?.endpoints?.find((e) => e.type === "ui");
        setTestAssistantUrl(chatUiEndpoint?.url || null);

        setServiceData(transformedServices);
        setIntegrationEndpoints(transformedEndpoints);
        setCertifiedBy(isDeploymentCertified ? "IBM" : null);
        setWorkerInfo(applicationDetailsResponse.data.worker ?? null);
      } catch (error) {
        console.error("Error fetching service details:", error);
        setServiceData([]);
        setIntegrationEndpoints([]);
        setTestAssistantUrl(null);
        setIntegrationFetchError(
          "Could not retrieve integration endpoints. Please try again.",
        );
        setCertifiedBy(null);
        setWorkerInfo(null);
      }
    };

    fetchServiceDetails();
  }, [deployment.id, deployment.type]);

  const calculatePercentage = (used: number, allocated: number) => {
    if (allocated === 0) return 0;
    return Math.round((used / allocated) * 100);
  };

  // Persists the edited deployment name and notifies the parent
  const handleNameSave = async (newName: string) => {
    await api.put(APPLICATION_ENDPOINTS.UPDATE_APPLICATION(deployment.id), {
      name: newName,
    });
    onNameUpdate?.(newName);
  };

  // navigator.clipboard is undefined on non-secure (plain HTTP) pages. Hide the copy button there, since CopyButton would still report "Copied to clipboard".
  const canCopyToClipboard = Boolean(navigator.clipboard);
  const handleCopyMcpEndpoint = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url);
      setCopyError(null);
    } catch {
      setCopyError("Could not copy the MCP endpoint. Please copy it manually.");
    }
  };

  return (
    <>
      {copyError && (
        <ToastNotification
          kind="error"
          title="Failed to copy MCP endpoint"
          subtitle={copyError}
          onClose={() => setCopyError(null)}
          className={styles.toastNotification}
        />
      )}
      {datasourceSupportError && (
        <ToastNotification
          kind="error"
          title="Failed to determine data sources support"
          subtitle={datasourceSupportError}
          timeout={5000}
          onClose={clearDatasourceSupportError}
          className={styles.toastNotification}
        />
      )}
      <DetailsLayout
        backLabel={deploymentSource}
        onBack={onBack}
        title={
          <InlineEditableTitle
            id="deployment-name-inline"
            label="Deployment name"
            value={deployment.name}
            onSave={handleNameSave}
          />
        }
        titleAsText={deployment.name}
        subtitle={
          <>
            {activeSection === "datasources" && datasourceCount === 0 ? (
              <Tag type="purple" size="md" renderIcon={DiamondFill}>
                No data
              </Tag>
            ) : (
              <StatusTag status={deployment.status} />
            )}
            <Tag type="gray">{deployment.type}</Tag>
            {certifiedBy && (
              <span className={styles.headerCertifiedBadge}>
                <Badge size={16} className={styles.badgeIcon} />
                <span className={styles.badgeName}>
                  {certifiedBy} certified
                </span>
              </span>
            )}
          </>
        }
        pageActions={
          isDigitalAssistant && testAssistantUrl
            ? [
                {
                  key: "test-assistant",
                  kind: "tertiary",
                  label: "Test assistant",
                  onClick: () => {
                    window.open(
                      testAssistantUrl,
                      "_blank",
                      "noopener,noreferrer",
                    );
                  },
                },
              ]
            : []
        }
        pageActionsOverflowLabel="More actions"
        fullWidthGrid="xl"
        navItems={[
          { key: "details", label: "Details" },
          { key: "services", label: "Services" },
          { key: "integration", label: "Integration endpoints" },
          ...(acceptsDatasource
            ? [{ key: "datasources", label: "Data sources" }]
            : []),
        ]}
        activeKey={activeSection}
        onNavChange={(key) => setActiveSection(key as typeof activeSection)}
      >
        {activeSection === "details" && (
          <>
            <Grid className={styles.detailsGrid}>
              <Column sm={4} md={8} lg={16}>
                <ProductiveCard title="Details" className={styles.detailsCard}>
                  {(workerInfo?.name || workerInfo?.runtime_type) && (
                    <div className={styles.workerInfoRow}>
                      {workerInfo.name && (
                        <div className={styles.workerInfoField}>
                          <span className={styles.workerInfoLabel}>
                            Worker resource
                          </span>
                          <span className={styles.workerInfoValue}>
                            {workerInfo.name}
                          </span>
                        </div>
                      )}
                      {workerInfo.runtime_type && (
                        <div className={styles.workerInfoField}>
                          <span className={styles.workerInfoLabel}>
                            Deployment type
                          </span>
                          <span className={styles.workerInfoValue}>
                            {WORKER_RUNTIME_LABELS[
                              workerInfo.runtime_type.toLowerCase()
                            ]?.label ?? workerInfo.runtime_type}
                          </span>
                        </div>
                      )}
                    </div>
                  )}
                </ProductiveCard>
              </Column>
            </Grid>

            <Grid className={styles.resourcesGrid}>
              <Column sm={4} md={8} lg={16}>
                <ProductiveCard
                  title="Allocated resources"
                  className={styles.resourceCard}
                >
                  <div className={styles.resourcesRow}>
                    {isLoadingResources ? (
                      <>
                        {[0, 1].map((index) => (
                          <div key={index} className={styles.resourceItem}>
                            <SkeletonText lineCount={1} width="30%" />
                            <SkeletonPlaceholder
                              className={styles.resourceSkeletonBar}
                            />
                            <div className={styles.resourceStats}>
                              <SkeletonText lineCount={1} width="35%" />
                              <SkeletonText lineCount={1} width="40%" />
                            </div>
                          </div>
                        ))}
                      </>
                    ) : (
                      resources.map((resource, index) => {
                        const percentage = calculatePercentage(
                          resource.used,
                          resource.allocated,
                        );

                        return (
                          <div key={index} className={styles.resourceItem}>
                            <h4 className={styles.resourceName}>
                              {resource.name}
                            </h4>
                            <ProgressBar
                              value={percentage}
                              max={100}
                              label="Progress"
                              helperText=""
                              hideLabel
                              className={
                                percentage > 90
                                  ? styles.progressDanger
                                  : percentage > 80
                                    ? styles.progressWarning
                                    : styles.progressSuccess
                              }
                            />
                            <div className={styles.resourceStats}>
                              <span className={styles.usedValue}>
                                {resource.used} {`(${percentage}%)`} used
                              </span>
                              <span className={styles.allocatedValue}>
                                {resource.used} / {resource.allocated}{" "}
                                {resource.unit} allocated
                              </span>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>

                  {/* Accelerator Cards Section - Integrated */}
                  {acceleratorCards.length > 0 && (
                    <Column
                      sm={4}
                      md={8}
                      lg={16}
                      className={styles.acceleratorCardsColumn}
                    >
                      <div className={styles.acceleratorCardsItem}>
                        <h4 className={styles.acceleratorCardsTitle}>
                          Accelerator cards
                        </h4>
                        <Grid className={styles.acceleratorCardsGrid}>
                          <Column sm={2} md={3} lg={3}>
                            <ol className={styles.acceleratorCardsList}>
                              {acceleratorCards
                                .slice(
                                  0,
                                  Math.ceil(acceleratorCards.length / 2),
                                )
                                .map((card) => (
                                  <li
                                    key={card.id}
                                    className={styles.acceleratorCardItem}
                                  >
                                    {card.label}
                                  </li>
                                ))}
                            </ol>
                          </Column>
                          <Column sm={2} md={3} lg={3}>
                            <ol
                              className={styles.acceleratorCardsList}
                              start={Math.ceil(acceleratorCards.length / 2) + 1}
                            >
                              {acceleratorCards
                                .slice(Math.ceil(acceleratorCards.length / 2))
                                .map((card) => (
                                  <li
                                    key={card.id}
                                    className={styles.acceleratorCardItem}
                                  >
                                    {card.label}
                                  </li>
                                ))}
                            </ol>
                          </Column>
                        </Grid>
                      </div>
                    </Column>
                  )}
                </ProductiveCard>
              </Column>
            </Grid>
          </>
        )}

        {activeSection === "services" && (
          <Grid className={styles.servicesGrid}>
            <Column sm={4} md={8} lg={16}>
              <ProductiveCard
                title={"Services"}
                className={styles.detailsCard}
              ></ProductiveCard>
            </Column>
            {serviceData.map((deploymentServiceData) => (
              <Column key={deploymentServiceData.id} sm={4} md={8} lg={16}>
                <ProductiveCard
                  title={deploymentServiceData.title}
                  description={deploymentServiceData.description}
                  className={styles.serviceCard}
                >
                  <div className={styles.serviceDetails}>
                    <div className={styles.serviceDetailRow}>
                      <span className={styles.serviceDetailLabel}>
                        Service version
                      </span>
                      <span className={styles.serviceDetailValue}>
                        {formatVersion(deploymentServiceData.serviceVersion)}
                      </span>
                    </div>

                    {deploymentServiceData.embeddingModel && (
                      <div className={styles.serviceDetailRow}>
                        <span className={styles.serviceDetailLabel}>
                          Embedding model
                        </span>
                        <span className={styles.serviceDetailValue}>
                          {deploymentServiceData.embeddingModel}
                        </span>
                      </div>
                    )}

                    {deploymentServiceData.vectorStore && (
                      <div className={styles.serviceDetailRow}>
                        <span className={styles.serviceDetailLabel}>
                          Vector store
                        </span>
                        <span className={styles.serviceDetailValue}>
                          {deploymentServiceData.vectorStore}
                        </span>
                      </div>
                    )}

                    {deploymentServiceData.largeLanguageModel && (
                      <div className={styles.serviceDetailRow}>
                        <span className={styles.serviceDetailLabel}>
                          Large Language Model (LLM)
                        </span>
                        <span className={styles.serviceDetailValue}>
                          {deploymentServiceData.largeLanguageModel}
                        </span>
                      </div>
                    )}

                    {deploymentServiceData.rankerModel && (
                      <div className={styles.serviceDetailRow}>
                        <span className={styles.serviceDetailLabel}>
                          Reranker model
                        </span>
                        <span className={styles.serviceDetailValue}>
                          {deploymentServiceData.rankerModel}
                        </span>
                      </div>
                    )}

                    {deploymentServiceData.customComponents?.map(
                      (customComponent) => (
                        <React.Fragment key={customComponent.label}>
                          <div className={styles.serviceDetailRow}>
                            <span className={styles.serviceDetailLabel}>
                              {customComponent.label}
                            </span>
                            <span className={styles.serviceDetailValue}>
                              {customComponent.model ??
                                customComponent.providerName ??
                                "Unknown"}
                            </span>
                          </div>
                          {customComponent.model &&
                            customComponent.providerName && (
                              <div className={styles.serviceDetailRow}>
                                <span className={styles.serviceDetailLabel}>
                                  {customComponent.label} backend
                                </span>
                                <span className={styles.serviceDetailValue}>
                                  {customComponent.providerName}
                                </span>
                              </div>
                            )}
                        </React.Fragment>
                      ),
                    )}

                    {deploymentServiceData.inferenceBackend && (
                      <div className={styles.serviceDetailRow}>
                        <span className={styles.serviceDetailLabel}>
                          Inference backend
                        </span>
                        <span className={styles.serviceDetailValue}>
                          {deploymentServiceData.inferenceBackend}
                        </span>
                      </div>
                    )}
                  </div>
                </ProductiveCard>
              </Column>
            ))}
          </Grid>
        )}

        {activeSection === "datasources" && acceptsDatasource && (
          <ApplicationDatasourcesTable
            applicationId={deployment.id}
            onDatasourceCountChange={(count) => setDatasourceCount(count)}
          />
        )}

        {activeSection === "integration" && (
          <Grid className={styles.servicesGrid}>
            <Column sm={4} md={8} lg={16}>
              <ProductiveCard
                title={"Integration endpoints"}
                className={styles.detailsCard}
              ></ProductiveCard>
            </Column>
            {integrationFetchError && (
              <Column sm={4} md={8} lg={16}>
                <ToastNotification
                  aria-label="close notification"
                  kind="error"
                  title="Failed to load integration endpoints"
                  subtitle={integrationFetchError}
                  hideCloseButton
                  className={styles.toastNotification}
                />
              </Column>
            )}
            {integrationEndpoints.map((integrationEndpointsData) => (
              <Column key={integrationEndpointsData.id} sm={4} md={8} lg={16}>
                <ProductiveCard
                  title={integrationEndpointsData.title}
                  description={integrationEndpointsData.description}
                  className={styles.serviceCard}
                >
                  <Grid className={styles.integrationEndpointGrid}>
                    <Column sm={4} md={8} lg={16}>
                      <div className={styles.integrationEndpointLeftColumn}>
                        <div className={styles.integrationEndpointField}>
                          <span
                            className={styles.integrationEndpointDetailLabel}
                          >
                            Base hostname or url
                          </span>
                          <span
                            className={styles.integrationEndpointDetailValue}
                          >
                            <a
                              href={integrationEndpointsData.baseURL}
                              target="_blank"
                              rel="noopener noreferrer"
                            >
                              {integrationEndpointsData.baseURL}
                            </a>
                          </span>
                        </div>

                        {integrationEndpointsData.apiDocumentation && (
                          <div className={styles.integrationEndpointField}>
                            <span
                              className={styles.integrationEndpointDetailLabel}
                            >
                              API documentation
                            </span>
                            <span
                              className={styles.integrationEndpointDetailValue}
                            >
                              <a
                                href={integrationEndpointsData.apiDocumentation}
                                target="_blank"
                                rel="noopener noreferrer"
                              >
                                {integrationEndpointsData.apiDocumentation}
                              </a>
                            </span>
                          </div>
                        )}

                        {integrationEndpointsData.mcpEndpoint && (
                          <div className={styles.integrationEndpointField}>
                            <span
                              className={styles.integrationEndpointDetailLabel}
                            >
                              MCP endpoint
                            </span>
                            <span
                              className={`${styles.integrationEndpointDetailValue} ${styles.integrationEndpointCopyValue}`}
                            >
                              {integrationEndpointsData.mcpEndpoint}
                              {canCopyToClipboard && (
                                <CopyButton
                                  size="xs"
                                  feedback="Copied to clipboard"
                                  iconDescription="Copy MCP endpoint"
                                  onClick={() =>
                                    handleCopyMcpEndpoint(
                                      integrationEndpointsData.mcpEndpoint,
                                    )
                                  }
                                />
                              )}
                            </span>
                          </div>
                        )}
                      </div>
                    </Column>
                  </Grid>
                </ProductiveCard>
              </Column>
            ))}
          </Grid>
        )}
      </DetailsLayout>
    </>
  );
};

export default DeploymentDetails;
