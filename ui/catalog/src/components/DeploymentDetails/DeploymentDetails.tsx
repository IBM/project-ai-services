import { useState, useEffect } from "react";
import {
  Grid,
  Column,
  SideNav,
  SideNavItems,
  SideNavLink,
  Tag,
  ProgressBar,
  Button,
  TextInput,
  TextInputSkeleton,
  SkeletonText,
  SkeletonPlaceholder,
} from "@carbon/react";
import { PageHeader, ProductiveCard } from "@carbon/ibm-products";
import {
  ArrowLeft,
  Badge,
  CheckmarkFilled,
  PauseOutline,
  ErrorFilled,
  InProgress,
} from "@carbon/icons-react";
import type {
  DeploymentDetails as DeploymentDetailsType,
  DeploymentServiceData,
  DeployIntegrationEndpoints,
  ResourcesApiResponse,
  ApplicationDetailsApiResponse,
  AcceleratorCards as AcceleratorCardType,
} from "@/types/digitalAssistants";
import styles from "./DeploymentDetails.module.scss";
import { api } from "@/api/axios";
import { APPLICATION_ENDPOINTS, SERVICE_ENDPOINTS } from "@/constants";

interface DeploymentDetailsProps {
  deployment: DeploymentDetailsType;
  onBack: () => void;
  deploymentSource: string;
  onNameUpdate?: (newName: string) => void;
}

const DeploymentDetails = ({
  deployment,
  onBack,
  deploymentSource,
  onNameUpdate,
}: DeploymentDetailsProps) => {
  const [activeSection, setActiveSection] = useState("details");
  const [resources, setResources] = useState<
    DeploymentDetailsType["resources"]
  >([]);
  const [isLoadingResources, setIsLoadingResources] = useState(false);
  const [serviceData, setServiceData] = useState<DeploymentServiceData[]>([]);
  const [integrationEndpoints, setIntegrationEndpoints] = useState<
    DeployIntegrationEndpoints[]
  >([]);
  const [acceleratorCards, setAcceleratorCards] = useState<
    AcceleratorCardType[]
  >([]);
  const [editedName, setEditedName] = useState(deployment.name);
  const [isSaving, setIsSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [certifiedBy, setCertifiedBy] = useState<string | null>(null);

  useEffect(() => {
    setEditedName(deployment.name);
    setSaveError("");
  }, [deployment.name]);

  useEffect(() => {
    const fetchResources = async () => {
      setIsLoadingResources(true);
      setResources([]);

      try {
        const response = await api.get<ResourcesApiResponse>(
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
        const transformedServices: DeploymentServiceData[] =
          deploymentServices.map((service) => {
            const llmComponent = service.components.find(
              (c) => c.type === "llm",
            );
            const embeddingComponent = service.components.find(
              (c) => c.type === "embedding",
            );
            const vectorStoreComponent = service.components.find(
              (c) => c.type === "vector_store",
            );
            const rerankerComponent = service.components.find(
              (c) => c.type === "reranker",
            );
            const serviceDescription =
              serviceMetadataById[service.catalog_id]?.description ??
              `${service.type} service`;

            return {
              id: service.id,
              title:
                service.type.charAt(0).toUpperCase() + service.type.slice(1),
              description: serviceDescription,
              serviceVersion: service.version,
              largeLanguageModel: llmComponent?.metadata?.model,
              inferenceBackend:
                llmComponent?.provider?.name ||
                embeddingComponent?.provider?.name ||
                rerankerComponent?.provider?.name ||
                "Unknown",
              embeddingModel: embeddingComponent?.metadata?.model,
              vectorStore: vectorStoreComponent?.provider?.name,
              rankerModel: rerankerComponent?.metadata?.model,
            };
          });

        const transformedEndpoints: DeployIntegrationEndpoints[] =
          deploymentServices.map((service) => {
            const uiEndpoint = service.endpoints.find((e) => e.type === "ui");
            const apiEndpoint = service.endpoints.find((e) => e.type === "api");
            const serviceDescription =
              serviceMetadataById[service.catalog_id]?.description ??
              `${service.type} service`;

            return {
              id: service.id,
              title:
                service.type.charAt(0).toUpperCase() + service.type.slice(1),
              description: serviceDescription,
              baseURL: uiEndpoint?.url || apiEndpoint?.url || "",
              apiDocumentaion: apiEndpoint?.url
                ? `${apiEndpoint.url}/docs`
                : "",
              interactiveAPIs: service.endpoints
                .filter((endpoint) => endpoint.type === "ui")
                .map((endpoint) => endpoint.url),
            };
          });

        setServiceData(transformedServices);
        setIntegrationEndpoints(transformedEndpoints);
        setCertifiedBy(isDeploymentCertified ? "IBM" : null);
      } catch (error) {
        console.error("Error fetching service details:", error);
        setServiceData([]);
        setIntegrationEndpoints([]);
        setCertifiedBy(null);
      }
    };

    fetchServiceDetails();
  }, [deployment.id, deployment.type]);

  const STATUS_CONFIG = {
    Initializing: {
      tagType: "blue" as const,
      icon: InProgress,
      className: styles.statusTagInfo,
    },
    Downloading: {
      tagType: "blue" as const,
      icon: InProgress,
      className: styles.statusTagInfo,
    },
    Deploying: {
      tagType: "blue" as const,
      icon: InProgress,
      className: styles.statusTagInfo,
    },
    Running: {
      tagType: "green" as const,
      icon: CheckmarkFilled,
      className: styles.statusTagSuccess,
    },
    Deleting: {
      tagType: "blue" as const,
      icon: InProgress,
      className: styles.statusTagInfo,
    },
    Error: {
      tagType: "red" as const,
      icon: ErrorFilled,
      className: styles.statusTagError,
    },
    Stopped: {
      tagType: "gray" as const,
      icon: PauseOutline,
      className: styles.statusTagSecondary,
    },
    "Deploying...": {
      tagType: "blue" as const,
      icon: InProgress,
      className: styles.statusTagInfo,
    },
    "Deleting...": {
      tagType: "blue" as const,
      icon: InProgress,
      className: styles.statusTagInfo,
    },
  } as const;

  const DEFAULT_STATUS_CONFIG = {
    tagType: "gray" as const,
    icon: PauseOutline,
    className: styles.statusTagSecondary,
  } as const;

  const getStatusTag = (status: string) => {
    const config =
      STATUS_CONFIG[status as keyof typeof STATUS_CONFIG] ||
      DEFAULT_STATUS_CONFIG;

    return (
      <Tag
        type={config.tagType}
        size="md"
        renderIcon={config.icon}
        className={config.className}
      >
        {status}
      </Tag>
    );
  };

  const calculatePercentage = (used: number, allocated: number) => {
    if (allocated === 0) return 0;
    return Math.round((used / allocated) * 100);
  };

  const handleSave = async () => {
    // Validate name
    if (editedName.length < 3 || editedName.length > 100) {
      setSaveError("Name must be between 3 and 100 characters");
      return;
    }

    // Check if name actually changed
    if (editedName === deployment.name) {
      return;
    }

    setIsSaving(true);
    setSaveError("");

    try {
      await api.put(APPLICATION_ENDPOINTS.UPDATE_APPLICATION(deployment.id), {
        name: editedName,
      });
      onNameUpdate?.(editedName);
    } catch (error) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : "Failed to update deployment name";
      setSaveError(errorMessage);
    } finally {
      setIsSaving(false);
    }
  };

  const handleCancel = () => {
    setEditedName(deployment.name);
    setSaveError("");
  };

  return (
    <>
      <div>
        <PageHeader
          breadcrumbs={[
            {
              key: "back",
              href: "#",
              title: deploymentSource,
              label: (
                <span
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "0.5rem",
                    cursor: "pointer",
                  }}
                  onClick={(e) => {
                    e.preventDefault();
                    onBack();
                  }}
                >
                  <ArrowLeft size={16} />
                  {deploymentSource}
                </span>
              ),
            },
          ]}
          breadcrumbOverflowAriaLabel="Show more breadcrumbs"
          title={deployment.name}
          subtitle={
            <div style={{ display: "flex", gap: "0.5rem" }}>
              {getStatusTag(deployment.status)}
              <Tag type="gray">{deployment.type}</Tag>
            </div>
          }
        />
        <hr className={styles.breadcrumbDivider} />
      </div>

      <Grid className={styles.mainGrid}>
        <Column sm={4} md={2} lg={3} className={styles.sidebarColumn}>
          <SideNav
            isFixedNav
            expanded
            aria-label="Side navigation"
            className={styles.sideNav}
          >
            <SideNavItems>
              <SideNavLink
                isActive={activeSection === "details"}
                onClick={() => setActiveSection("details")}
              >
                Details
              </SideNavLink>
              <SideNavLink
                isActive={activeSection === "services"}
                onClick={() => setActiveSection("services")}
              >
                Services
              </SideNavLink>
              <SideNavLink
                isActive={activeSection === "integration"}
                onClick={() => setActiveSection("integration")}
              >
                Integration endpoints
              </SideNavLink>
            </SideNavItems>
          </SideNav>
        </Column>

        <Column sm={4} md={6} lg={13} className={styles.contentColumn}>
          {activeSection === "details" && (
            <>
              <Grid className={styles.detailsGrid}>
                <Column sm={4} md={8} lg={16}>
                  <ProductiveCard
                    title="Details"
                    className={styles.detailsCard}
                  >
                    {certifiedBy && (
                      <span className={styles.certifiedBadge}>
                        <Badge size={16} className={styles.badgeIcon} />
                        <span className={styles.badgeName}>
                          {certifiedBy} certified
                        </span>
                      </span>
                    )}

                    <Grid className={styles.resourcesGrid}>
                      <Column lg={8} md={4} sm={2}>
                        {isLoadingResources ? (
                          <TextInputSkeleton />
                        ) : (
                          <TextInput
                            className={styles.labelColor}
                            id="deployment-name"
                            labelText="Name"
                            value={editedName}
                            onChange={(e) => {
                              setEditedName(e.target.value);
                              setSaveError("");
                            }}
                            invalid={
                              editedName.length < 3 || editedName.length > 100
                            }
                            invalidText={
                              editedName.length < 3
                                ? "Name must be at least 3 characters"
                                : "Name must be at most 100 characters"
                            }
                            disabled={isSaving}
                          />
                        )}
                      </Column>
                    </Grid>
                  </ProductiveCard>
                </Column>
              </Grid>

              <Grid className={styles.resourcesGrid}>
                <Column sm={4} md={8} lg={16}>
                  <ProductiveCard
                    title="Allocated resources"
                    className={styles.resourceCard}
                  >
                    <Grid className={styles.resourcesInnerGrid}>
                      {isLoadingResources ? (
                        <>
                          {[0, 1].map((index) => (
                            <Column
                              key={index}
                              sm={4}
                              md={4}
                              lg={8}
                              className={styles.resourceColumn}
                            >
                              <div className={styles.resourceItem}>
                                <SkeletonText lineCount={1} width="30%" />
                                <SkeletonPlaceholder
                                  style={{
                                    width: "100%",
                                    height: "0.5rem",
                                    marginTop: "1rem",
                                  }}
                                />
                                <div className={styles.resourceStats}>
                                  <SkeletonText lineCount={1} width="35%" />
                                  <SkeletonText lineCount={1} width="40%" />
                                </div>
                              </div>
                            </Column>
                          ))}
                        </>
                      ) : (
                        resources.map((resource, index) => {
                          const percentage = calculatePercentage(
                            resource.used,
                            resource.allocated,
                          );

                          return (
                            <Column
                              key={index}
                              sm={4}
                              md={4}
                              lg={8}
                              className={styles.resourceColumn}
                            >
                              <div className={styles.resourceItem}>
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
                            </Column>
                          );
                        })
                      )}
                    </Grid>

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
                                start={
                                  Math.ceil(acceleratorCards.length / 2) + 1
                                }
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

              <Grid className={styles.actionsGrid}>
                <Column sm={4} md={8} lg={16}>
                  {saveError && (
                    <div className={styles.saveError}>{saveError}</div>
                  )}
                  <div className={styles.actionButtons}>
                    <Button
                      kind="secondary"
                      onClick={handleCancel}
                      disabled={isSaving}
                    >
                      Cancel
                    </Button>
                    <Button
                      kind="primary"
                      onClick={handleSave}
                      disabled={
                        isSaving ||
                        editedName === deployment.name ||
                        editedName.length < 3 ||
                        editedName.length > 100
                      }
                    >
                      {isSaving ? "Saving..." : "Save"}
                    </Button>
                  </div>
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
                          {deploymentServiceData.serviceVersion}
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

                      <div className={styles.serviceDetailRow}>
                        <span className={styles.serviceDetailLabel}>
                          Inference backend
                        </span>
                        <span className={styles.serviceDetailValue}>
                          {deploymentServiceData.inferenceBackend}
                        </span>
                      </div>
                    </div>
                  </ProductiveCard>
                </Column>
              ))}
            </Grid>
          )}

          {activeSection === "integration" && (
            <Grid className={styles.servicesGrid}>
              <Column sm={4} md={8} lg={16}>
                <ProductiveCard
                  title={"Integration endpoints"}
                  className={styles.detailsCard}
                ></ProductiveCard>
              </Column>
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

                          {integrationEndpointsData.apiDocumentaion && (
                            <div className={styles.integrationEndpointField}>
                              <span
                                className={
                                  styles.integrationEndpointDetailLabel
                                }
                              >
                                API documentation
                              </span>
                              <span
                                className={
                                  styles.integrationEndpointDetailValue
                                }
                              >
                                <a
                                  href={
                                    integrationEndpointsData.apiDocumentaion
                                  }
                                  target="_blank"
                                  rel="noopener noreferrer"
                                >
                                  {integrationEndpointsData.apiDocumentaion}
                                </a>
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
        </Column>
      </Grid>
    </>
  );
};

export default DeploymentDetails;
