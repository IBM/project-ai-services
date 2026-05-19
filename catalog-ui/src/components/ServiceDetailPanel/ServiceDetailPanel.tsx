import { SidePanel } from "@carbon/ibm-products";
import { Button } from "@carbon/react";
import { Badge, Launch } from "@carbon/icons-react";
import styles from "./ServiceDetailPanel.module.scss";

export interface ServiceDetailData {
  id: string;
  title: string;
  description: string;
  isCertified?: boolean;
  tags?: string[];
  demos?: {
    version?: string;
    inferenceBackend?: string;
    embeddingModel?: string;
    vectorStore?: string;
    rerankerModel?: string;
    llm?: string;
    defaultInferenceBackend?: string;
  };
  inputs?: string[];
  outputs?: string[];
  dependencies?: string[];
  contentSupport?: {
    languages?: string[];
    formats?: string[];
    content?: string[];
    reranking?: string[];
  };
  resourceConsumption?: {
    small?: string[];
    medium?: string[];
    large?: string[];
  };
  sla?: {
    small?: {
      assumptions?: string[];
      guarantees?: string[];
    };
    medium?: {
      assumptions?: string[];
      guarantees?: string[];
    };
    large?: {
      assumptions?: string[];
      guarantees?: string[];
    };
  };
  assets?: {
    architectures?: string;
    apiUrl?: string;
    sourceCodeUrl?: string;
  };
}

export interface ServiceDetailPanelProps {
  open: boolean;
  onClose: () => void;
  service: ServiceDetailData | null;
}

const ServiceDetailPanel = ({
  open,
  onClose,
  service,
}: ServiceDetailPanelProps) => {
  if (!service) return null;

  return (
    <SidePanel
      open={open}
      onRequestClose={onClose}
      title={service.title}
      includeOverlay
      placement="right"
      size="lg"
      className={styles.sidePanel}
      actionToolbarButtons={
        service.assets?.sourceCodeUrl
          ? [
              <Button
                key="view-source"
                kind="ghost"
                renderIcon={Launch}
                onClick={() =>
                  window.open(service.assets?.sourceCodeUrl, "_blank")
                }
              >
                View source code
              </Button>,
            ]
          : undefined
      }
    >
      <div className={styles.content}>
        {/* Description */}
        <p className={styles.description}>{service.description}</p>

        {/* Tags */}
        <div className={styles.tagContainer}>
          {service.tags?.map((tag, index) => (
            <div key={index} className={styles.tag}>
              {tag}
            </div>
          ))}
          {service.isCertified && (
            <div className={styles.certifiedTag}>
              <Badge size={16} className={styles.checkIcon} />
              IBM certified
            </div>
          )}
        </div>

        <div className={styles.divider} />

        {/* Demos and prototypes */}
        {service.demos && (
          <>
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>Demos and prototypes</h2>

              <div className={styles.demoGrid}>
                {service.demos.version && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>Version</div>
                    <div className={styles.fieldValue}>
                      {service.demos.version}
                    </div>
                  </div>
                )}
                {service.demos.inferenceBackend && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>Inference backend</div>
                    <div className={styles.fieldValue}>
                      {service.demos.inferenceBackend}
                    </div>
                  </div>
                )}
              </div>

              <div className={styles.demoGrid}>
                {service.demos.embeddingModel && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>
                      Default embedding model
                    </div>
                    <div className={styles.fieldValue}>
                      {service.demos.embeddingModel}
                    </div>
                  </div>
                )}
                {service.demos.vectorStore && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>
                      Default vector store
                    </div>
                    <div className={styles.fieldValue}>
                      {service.demos.vectorStore}
                    </div>
                  </div>
                )}
              </div>

              <div className={styles.demoGrid}>
                {service.demos.llm && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>
                      Default Large Language Model (LLM)
                    </div>
                    <div className={styles.fieldValue}>{service.demos.llm}</div>
                  </div>
                )}
                {service.demos.rerankerModel && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>Reranker Model</div>
                    <div className={styles.fieldValue}>
                      {service.demos.rerankerModel}
                    </div>
                  </div>
                )}
                {service.demos.defaultInferenceBackend && (
                  <div className={styles.demoItem}>
                    <div className={styles.fieldLabel}>
                      Default inference backend
                    </div>
                    <div className={styles.fieldValue}>
                      {service.demos.defaultInferenceBackend}
                    </div>
                  </div>
                )}
              </div>
            </section>

            <div className={styles.divider} />
          </>
        )}

        {/* Inputs and outputs */}
        {(service.inputs || service.outputs) && (
          <>
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>Inputs and outputs</h2>

              <div className={styles.twoColumns}>
                {service.inputs && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Inputs</div>
                    <ul className={styles.bulletList}>
                      {service.inputs.map((input, index) => (
                        <li key={index}>{input}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {service.outputs && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Outputs</div>
                    <ul className={styles.bulletList}>
                      {service.outputs.map((output, index) => (
                        <li key={index}>{output}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </section>

            <div className={styles.divider} />
          </>
        )}

        {/* Dependencies and integration */}
        {service.dependencies && service.dependencies.length > 0 && (
          <>
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>
                Dependencies and integration
              </h2>

              <div className={styles.columnLabel}>External dependencies</div>
              <ul className={styles.bulletList}>
                {service.dependencies.map((dep, index) => (
                  <li key={index}>{dep}</li>
                ))}
              </ul>
            </section>

            <div className={styles.divider} />
          </>
        )}

        {/* Content and format support */}
        {service.contentSupport && (
          <>
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>
                Content and format support
              </h2>

              <div className={styles.threeColumns}>
                {service.contentSupport.languages && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Languages</div>
                    <ul className={styles.dashList}>
                      {service.contentSupport.languages.map((lang, index) => (
                        <li key={index}>{lang}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {service.contentSupport.formats && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Document formats</div>
                    <ul className={styles.dashList}>
                      {service.contentSupport.formats.map((format, index) => (
                        <li key={index}>{format}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {service.contentSupport.content && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Content</div>
                    <ul className={styles.dashList}>
                      {service.contentSupport.content.map((content, index) => (
                        <li key={index}>{content}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {service.contentSupport.reranking && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Reranking</div>
                    <ul className={styles.dashList}>
                      {service.contentSupport.reranking.map((rerank, index) => (
                        <li key={index}>{rerank}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </section>

            <div className={styles.divider} />
          </>
        )}

        {/* Expected resource consumption */}
        {service.resourceConsumption && (
          <>
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>
                Expected resource consumption
              </h2>

              <div className={styles.threeColumns}>
                {service.resourceConsumption.small && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Small</div>
                    <ul className={styles.dashList}>
                      {service.resourceConsumption.small.map((item, index) => (
                        <li key={index}>{item}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {service.resourceConsumption.medium && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Medium</div>
                    <ul className={styles.dashList}>
                      {service.resourceConsumption.medium.map((item, index) => (
                        <li key={index}>{item}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {service.resourceConsumption.large && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Large</div>
                    <ul className={styles.dashList}>
                      {service.resourceConsumption.large.map((item, index) => (
                        <li key={index}>{item}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </section>

            <div className={styles.divider} />
          </>
        )}

        {/* Service level agreements */}
        {service.sla && (
          <>
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>Service level agreements</h2>

              <div className={styles.threeColumns}>
                {service.sla.small && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Small:</div>
                    {service.sla.small.assumptions &&
                      service.sla.small.assumptions.length > 0 && (
                        <>
                          <div className={styles.subLabel}>Assumptions</div>
                          <ul className={styles.dashList}>
                            {service.sla.small.assumptions.map(
                              (item, index) => (
                                <li key={index}>{item}</li>
                              ),
                            )}
                          </ul>
                        </>
                      )}
                    {service.sla.small.guarantees &&
                      service.sla.small.guarantees.length > 0 && (
                        <>
                          <div className={styles.subLabel}>Guarantees</div>
                          <ul className={styles.dashList}>
                            {service.sla.small.guarantees.map((item, index) => (
                              <li key={index}>{item}</li>
                            ))}
                          </ul>
                        </>
                      )}
                  </div>
                )}
                {service.sla.medium && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Medium:</div>
                    {service.sla.medium.assumptions &&
                      service.sla.medium.assumptions.length > 0 && (
                        <>
                          <div className={styles.subLabel}>Assumptions</div>
                          <ul className={styles.dashList}>
                            {service.sla.medium.assumptions.map(
                              (item, index) => (
                                <li key={index}>{item}</li>
                              ),
                            )}
                          </ul>
                        </>
                      )}
                    {service.sla.medium.guarantees &&
                      service.sla.medium.guarantees.length > 0 && (
                        <>
                          <div className={styles.subLabel}>Guarantees</div>
                          <ul className={styles.dashList}>
                            {service.sla.medium.guarantees.map(
                              (item, index) => (
                                <li key={index}>{item}</li>
                              ),
                            )}
                          </ul>
                        </>
                      )}
                  </div>
                )}
                {service.sla.large && (
                  <div className={styles.column}>
                    <div className={styles.columnLabel}>Large:</div>
                    {service.sla.large.assumptions &&
                      service.sla.large.assumptions.length > 0 && (
                        <>
                          <div className={styles.subLabel}>Assumptions</div>
                          <ul className={styles.dashList}>
                            {service.sla.large.assumptions.map(
                              (item, index) => (
                                <li key={index}>{item}</li>
                              ),
                            )}
                          </ul>
                        </>
                      )}
                    {service.sla.large.guarantees &&
                      service.sla.large.guarantees.length > 0 && (
                        <>
                          <div className={styles.subLabel}>Guarantees</div>
                          <ul className={styles.dashList}>
                            {service.sla.large.guarantees.map((item, index) => (
                              <li key={index}>{item}</li>
                            ))}
                          </ul>
                        </>
                      )}
                  </div>
                )}
              </div>
            </section>

            <div className={styles.divider} />
          </>
        )}

        {service.assets && (
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Assets</h2>

            <div className={styles.assetsGrid}>
              {service.assets.architectures && (
                <div className={styles.assetField}>
                  <div className={styles.fieldLabel}>Architectures</div>
                  <div className={styles.assetTag}>
                    {service.assets.architectures}
                  </div>
                </div>
              )}

              {service.assets.apiUrl && (
                <div className={styles.assetField}>
                  <div className={styles.fieldLabel}>API</div>
                  <div className={styles.fieldLabel}>documentation</div>
                  <a
                    href={service.assets.apiUrl}
                    className={styles.infoLink}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {service.assets.apiUrl}
                  </a>
                </div>
              )}
            </div>

            {service.assets.sourceCodeUrl && (
              <div className={styles.assetField}>
                <div className={styles.fieldLabel}>Source code</div>
                <Button
                  kind="tertiary"
                  size="md"
                  className={styles.sourceButton}
                  onClick={() =>
                    window.open(service.assets?.sourceCodeUrl, "_blank")
                  }
                >
                  View source code
                </Button>
              </div>
            )}
          </section>
        )}
      </div>
    </SidePanel>
  );
};

export default ServiceDetailPanel;
