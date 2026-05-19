import { SidePanel } from "@carbon/ibm-products";
import { Button, Tag } from "@carbon/react";
import { Badge } from "@carbon/icons-react";
import styles from "./ServiceDetailPanel.module.scss";

export interface ServiceDetailPanelProps {
  open: boolean;
  onClose: () => void;
  service: {
    id: string;
    title: string;
    description: string;
    isCertified?: boolean;
  } | null;
  onDeploy?: (id: string) => void;
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
    >
      <div className={styles.content}>
        {/* Description */}
        <p className={styles.description}>{service.description}</p>

        {/* Tags */}
        <div className={styles.tagContainer}>
          <div className={styles.tag}>by IBM Power</div>
          {service.isCertified && (
            <div className={styles.certifiedTag}>
              <Badge size={16} className={styles.checkIcon} />
              IBM certified
            </div>
          )}
        </div>

        <div className={styles.divider} />

        {/* Demos and prototypes */}
        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>Demos and prototypes</h2>
          
          <div className={styles.demoGrid}>
            <div className={styles.demoItem}>
              <div className={styles.fieldLabel}>Version</div>
              <div className={styles.fieldValue}>1.0.0</div>
            </div>
            <div className={styles.demoItem}>
              <div className={styles.fieldLabel}>Inference backend</div>
              <div className={styles.fieldValue}>RedHat AI Inference (default)</div>
            </div>
          </div>

          <div className={styles.demoGrid}>
            <div className={styles.demoItem}>
              <div className={styles.fieldLabel}>Default embedding model</div>
              <div className={styles.fieldmodel}>ibm-granite/granite-embedding-278m-multilingual</div>
            </div>
            <div className={styles.demoItem}>
              <div className={styles.fieldLabel}>Default vector store</div>
              <div className={styles.fieldValue}>OpenSearch (default)</div>
            </div>
          </div>

          <div className={styles.demoGrid}>
            <div className={styles.demoItem}>
              <div className={styles.fieldLabel}>Default Large Language Model (LLM)</div>
              <div className={styles.fieldmodel}>ibm-granite/granite-3.3-8b-instruct (on-prem)</div>
            </div>
            <div className={styles.demoItem}>
              <div className={styles.fieldLabel}>Default inference backend</div>
              <div className={styles.fieldValue}>OpenSearch (default)</div>
            </div>
          </div>
        </section>

        <div className={styles.divider} />

        {/* Inputs and outputs */}
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Inputs and outputs</h3>
          
          <div className={styles.twoColumns}>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Inputs</div>
              <ul className={styles.bulletList}>
                <li>Flag if text that is filtered, embedded, and indexed into vector store</li>
                <li>Hypertext</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Outputs</div>
              <ul className={styles.bulletList}>
                <li>Generated text from input documents</li>
                <li>Pointers to digitized documents</li>
                <li>Knowledge management indexing status</li>
              </ul>
            </div>
          </div>
        </section>


        <div className={styles.divider} />
        {/* Dependencies and integration */}
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Dependencies and integration</h3>
          
          <div className={styles.columnLabel}>External dependencies</div>
          <ul className={styles.bulletList}>
            <li>Inferencing endpoint compatible with OpenAI or similar AI APIs (required for embedding, filtering, and image-to-text capabilities)</li>
            <li>Supports various file formats for document ingestion (such as embeddings, indexing, and similarity search)</li>
          </ul>
        </section>

        <div className={styles.divider} />
        {/* Content and format support */}
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Content and format support</h3>
          
          <div className={styles.threeColumns}>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Languages</div>
              <ul className={styles.dashList}>
                <li>English</li>
                <li>French</li>
                <li>German</li>
                <li>Italian</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Supported formats</div>
              <ul className={styles.dashList}>
                <li>PDF</li>
                <li>DOCX</li>
                <li>PPTX</li>
                <li>XLSX</li>
                <li>HTML</li>
                <li>TXT</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Content</div>
              <ul className={styles.dashList}>
                <li>Text</li>
                <li>Tables</li>
                <li>Images</li>
              </ul>
            </div>
          </div>
        </section>
          
        <div className={styles.divider} />
        {/* Expected resource consumption */}
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Expected resource consumption</h3>
          
          <div className={styles.threeColumns}>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Small</div>
              <ul className={styles.dashList}>
                <li>Compute: 00 CPU cores</li>
                <li>Memory: 00 GB</li>
                <li>Storage: 00 GB</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Medium</div>
              <ul className={styles.dashList}>
                <li>Compute: 00 CPU cores + Slave cards</li>
                <li>Memory: 00 GB</li>
                <li>Storage: 00 GB</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Large</div>
              <ul className={styles.dashList}>
                <li>Compute: 00 CPU cores + 15 Slave cards</li>
                <li>Memory: 00 GB</li>
                <li>Storage: 00 GB</li>
              </ul>
            </div>
          </div>
        </section>
        

        <div className={styles.divider} />
        {/* Service level agreements */}
        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>Service level agreements</h2>
          
          <div className={styles.threeColumns}>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Small:</div>
              <div className={styles.subLabel}>Assumptions</div>
              <ul className={styles.dashList}>
                <li>Document size: TBD</li>
                <li>Features</li>
              </ul>
              <div className={styles.subLabel}>Guarantees</div>
              <ul className={styles.dashList}>
                <li>Embedding throughput: {'>'}8 mil. docs./hour</li>
                <li>End-to-end throughput: TBD</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Medium:</div>
              <div className={styles.subLabel}>Assumptions</div>
              <ul className={styles.dashList}>
                <li>Document size: TBD</li>
                <li>Features</li>
              </ul>
              <div className={styles.subLabel}>Guarantees</div>
              <ul className={styles.dashList}>
                <li>Embedding throughput: {'>'}8 mil. docs./hour</li>
                <li>End-to-end throughput: TBD</li>
              </ul>
            </div>
            <div className={styles.column}>
              <div className={styles.columnLabel}>Large:</div>
              <div className={styles.subLabel}>Assumptions</div>
              <ul className={styles.dashList}>
                <li>Document size: TBD</li>
                <li>Features</li>
              </ul>
              <div className={styles.subLabel}>Guarantees</div>
              <ul className={styles.dashList}>
                <li>–</li>
              </ul>
            </div>
          </div>
        </section>
        
        <div className={styles.divider} />

        {/* Assets */}
        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>Assets</h2>
          
          <div className={styles.assetsGrid}>
            <div className={styles.assetField}>
              <div className={styles.fieldLabel}>Architectures</div>
              <div className={styles.assetTag}>Digital assistant</div>
            </div>

            <div className={styles.assetField}>
              <div className={styles.fieldLabel}>API</div>
              <div className={styles.fieldLabel}>documentation</div>
              <a href="#" className={styles.infoLink}>http://10.20.188.184:5000/docs</a>
            </div>
          </div>

          <div className={styles.assetField}>
            <div className={styles.fieldLabel}>Source code</div>
            <Button kind="tertiary" size="md" className={styles.sourceButton}>
              View source code
            </Button>
          </div>
        </section>
      </div>
    </SidePanel>
  );
};

export default ServiceDetailPanel;

// Made with Bob
