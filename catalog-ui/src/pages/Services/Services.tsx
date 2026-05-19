import { useState } from "react";
import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import ServiceCard from "@/components/ServiceCard";
import ServiceDetailPanel from "@/components/ServiceDetailPanel";
import type { ServiceDetailData } from "@/components/ServiceDetailPanel";
import styles from "./Services.module.scss";

const mockServices: ServiceDetailData[] = [
  {
    id: "1",
    title: "Digitize documents",
    description:
      "Converts physical and scanned documents into searchable, editable digital formats; enables efficient document management, data extraction, and workflow automation at scale.",
    isCertified: true,
    tags: ["by IBM Power"],
    demos: {
      version: "1.0.0",
      inferenceBackend: "RedHat AI Inference (default)",
      embeddingModel: "ibm-granite/granite-embedding-278m-multilingual",
      vectorStore: "OpenSearch (default)",
      llm: "ibm-granite/granite-3.3-8b-instruct (on-prem)",
      defaultInferenceBackend: "OpenSearch (default)",
    },
    inputs: [
      "Document files (e.g., PDFs)",
      "Flag if texts shall be filtered, embedded, and indexed into knowledge management (optional)",
    ],
    outputs: [
      "Texts digitized from input documents",
      "Pointers to digitized input documents",
      "Knowledge management indexing status",
    ],
    dependencies: [
      "Inferencing endpoint compatible with OpenAI or similar AI APIs (required for LLM-based summarization capabilities)",
      "Supported VectorDB (required for knowledge management features such as embeddings, indexing, and similarity search)",
    ],
    contentSupport: {
      languages: ["English", "French", "German", "Italian"],
      formats: ["PDF", "DOCX", "PPTX", "XLSX", "HTML", "TXT"],
      content: ["Text", "Tables", "Images"],
    },
    resourceConsumption: {
      small: ["Compute: 00 CPU cores", "Memory: 00 GB", "Storage: 00 GB"],
      medium: [
        "Compute: 00 CPU cores + Slave cards",
        "Memory: 00 GB",
        "Storage: 00 GB",
      ],
      large: [
        "Compute: 00 CPU cores + 15 Slave cards",
        "Memory: 00 GB",
        "Storage: 00 GB",
      ],
    },
    sla: {
      small: {
        assumptions: ["Document size: TBD", "Features"],
        guarantees: [
          "Embedding throughput: >8 mil. docs./hour",
          "End-to-end throughput: TBD",
        ],
      },
      medium: {
        assumptions: ["Document size: TBD", "Features"],
        guarantees: [
          "Embedding throughput: >8 mil. docs./hour",
          "End-to-end throughput: TBD",
        ],
      },
      large: {
        assumptions: ["Document size: TBD", "Features"],
        guarantees: ["–"],
      },
    },
    assets: {
      architectures: "Digital assistant",
      apiUrl: "http://10.20.188.184:5000/docs",
      sourceCodeUrl: "https://github.com/example/digitize-docs",
    },
  },
  {
    id: "2",
    title: "Find similar items",
    description:
      "Identifies and retrieves items similar to a reference item based on content, attributes, or patterns; enables efficient recommendation, duplicate detection, and content discovery at scale.",
    isCertified: true,
    tags: ["by IBM Power"],
    demos: {
      version: "1.0.0",
      inferenceBackend: "RedHat AI Inference (default)",
      embeddingModel: "BAI/bge-reranker-v2-m3 (on-prem)",
      rerankerModel: "BAAI/bge-reranker-v2-m3 (on-prem)",
      vectorStore: "OpenSearch (default)",
      defaultInferenceBackend: "OpenSearch (default)",
    },
    inputs: [
      "Item to find similar items for (e.g., text or document)",
      "Number of documents to receive, (optional) flag to activate reranking",
    ],
    outputs: [
      "Ranked list of items (e.g., text chunks) and pointers to their associated source (e.g., originating document)",
    ],
    dependencies: [
      "Inferencing endpoint compatible with OpenAI or similar AI APIs (required for embedding and reranking capabilities)",
      "Supported VectorDB (required as source for similar items)",
      "Knowledge management (VectorDB)",
      "Milvus",
    ],
    contentSupport: {
      languages: ["English", "French", "German", "Italian"],
      content: ["Text", "Tables", "Images"],
      reranking: ["Improved ranking through reranking AI model"],
    },
    resourceConsumption: {
      small: ["Compute: 00 CPU cores", "Memory: 00 GB", "Storage: 00 GB"],
      medium: [
        "Compute: 00 CPU cores + 0 Slave cards",
        "Memory: 00 GB",
        "Storage: 00 GB",
      ],
      large: [
        "Compute: 00 CPU cores + 0 Slave cards",
        "Memory: 00 GB",
        "Storage: 00 GB",
      ],
    },
    sla: {
      small: {
        assumptions: ["Time-to-retrieve items: <1 sec./item"],
        guarantees: [],
      },
      medium: {
        assumptions: ["Time-to-retrieve items: <1 sec./item"],
        guarantees: [],
      },
      large: {
        assumptions: [],
        guarantees: [],
      },
    },
    assets: {
      architectures: "Digital assistant",
      apiUrl: "http://10.20.188.184:5000/docs",
      sourceCodeUrl: "https:/example/watsonx-question-and-answer-on-power",
    },
  },
  {
    id: "3",
    title: "Question and answer",
    description:
      "Provides accurate, context-aware responses to user queries; enables automated customer support, knowledge base interactions, and conversational assistance at scale.",
    isCertified: true,
    tags: ["by IBM Power"],
    demos: {
      version: "1.0.0",
      defaultInferenceBackend: "OpenSearch(default)",
      llm: "ibm-granite/granite-3.3-8b-instruct (on-prem)",
    },
    inputs: [
      "Question in natural language",
      "Document context (optional)",
      "Flag if knowledge database should be used, otherwise, only general LLM knowledge is used",
    ],
    outputs: [
      "Answer to the input question",
      "Explanation of where knowledge for the answer was sourced from (general LLM or documents)",
      "If augmented with documents from the knowledge base: list of excerpts from the knowledge base, along with pointers from the knowledge base",
    ],
    dependencies: [
      "Inferencing endpoint compatible with OpenAI or similar AI APIs (required for LLM-based Q&A capabilities)",
      "Find similar items (required for augmenting the prompt to the LLM with knowledge from the knowledge base)",
    ],
    contentSupport: {
      languages: ["English", "French", "German", "Italian"],
      content: ["Text", "Tables"],
    },
    resourceConsumption: {
      small: ["Compute: 4 CPU cores", "Memory: 16 GB", "Storage: 50 GB"],
      medium: ["Compute: 8 CPU cores", "Memory: 32 GB", "Storage: 100 GB"],
      large: ["Compute: 16 CPU cores", "Memory: 64 GB", "Storage: 200 GB"],
    },
    sla: {
      small: {
        assumptions: [
          "Query complexity: Simple questions",
          "Context size: Up to 2K tokens",
        ],
        guarantees: ["Response time: <2 seconds", "Accuracy: >85%"],
      },
      medium: {
        assumptions: [
          "Query complexity: Moderate questions",
          "Context size: Up to 8K tokens",
        ],
        guarantees: ["Response time: <3 seconds", "Accuracy: >90%"],
      },
      large: {
        assumptions: [
          "Query complexity: Complex multi-step questions",
          "Context size: Up to 32K tokens",
        ],
        guarantees: ["Response time: <5 seconds", "Accuracy: >92%"],
      },
    },
    assets: {
      architectures: "Digital assistant",
      apiUrl: "http://10.20.188.184:5000/docs",
      sourceCodeUrl: "https://github.com/example/question-answer",
    },
  },
  {
    id: "4",
    title: "Summarize",
    description:
      "Condenses long-form content into concise, accurate summaries while preserving key information; enables efficient document analysis and information extraction at scale.",
    isCertified: true,
    tags: ["by IBM Power"],
    demos: {
      version: "1.0.0",
      defaultInferenceBackend: "OpenSearch(default)",
      llm: "ibm-granite/granite-3.3-8b-instruct (on-prem)",
    },
    inputs: [
      "Text to be summarized",
      "Maximum length of summary (optional)",
      "Summarization style or structure, like paragraph, bullets, key-points, or headings(optional)",
      "Temperature - controls the randomness (optional)",
    ],
    outputs: ["Summary of the input text"],
    dependencies: [
      "Inferencing endpoint compatible with OpenAI or similar AI APIs (required for LLM-based summarization capabilities)",
    ],
    contentSupport: {
      languages: ["English", "French", "German", "Italian"],
    },
    resourceConsumption: {
      small: ["Compute: 2 CPU cores", "Memory: 8 GB", "Storage: 20 GB"],
      medium: ["Compute: 4 CPU cores", "Memory: 16 GB", "Storage: 50 GB"],
      large: ["Compute: 8 CPU cores", "Memory: 32 GB", "Storage: 100 GB"],
    },
    sla: {
      small: {
        guarantees: ["Processing time: <3 seconds", "Compression ratio: 5:1"],
      },
      medium: {
        guarantees: ["Processing time: <5 seconds", "Compression ratio: 10:1"],
      },
      large: {
        guarantees: ["Processing time: <10 seconds", "Compression ratio: 20:1"],
      },
    },
    assets: {
      architectures: "Digital assistant",
      apiUrl: "http://10.20.188.184:5000/docs",
      sourceCodeUrl: "https://github.com/example/summarize",
    },
  },
];

const Services = () => {
  const [selectedService, setSelectedService] =
    useState<ServiceDetailData | null>(null);
  const [isPanelOpen, setIsPanelOpen] = useState(false);

  const handleCardClick = (id: string) => {
    const service = mockServices.find((s) => s.id === id);
    if (service) {
      setSelectedService(service);
      setIsPanelOpen(true);
    }
  };

  const handleDeploy = (id: string) => {
    console.log("Deploy service:", id);
  };

  const handleClosePanel = () => {
    setIsPanelOpen(false);
    setTimeout(() => setSelectedService(null), 300);
  };

  return (
    <div className={styles.servicesContainer}>
      <PageHeader
        title="Services"
        subtitle="Single-purpose AI capabilities designed to perform specific tasks independently or as part of larger solutions."
        className={styles.pageHeader}
      />
      <Tabs>
        <TabList
          aria-label="Services tabs"
          contained={false}
          className={styles.tabList}
        >
          <Tab>Deployments</Tab>
          <Tab>Catalog</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <div className={styles.tabContent}>Deployments content</div>
          </TabPanel>
          <TabPanel>
            <div className={styles.catalogGrid}>
              {mockServices.map((service) => (
                <ServiceCard
                  key={service.id}
                  id={service.id}
                  title={service.title}
                  description={service.description}
                  isCertified={service.isCertified}
                  onDeploy={handleDeploy}
                  onLearnMore={handleCardClick}
                />
              ))}
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>

      <ServiceDetailPanel
        open={isPanelOpen}
        onClose={handleClosePanel}
        service={selectedService}
      />
    </div>
  );
};

export default Services;
