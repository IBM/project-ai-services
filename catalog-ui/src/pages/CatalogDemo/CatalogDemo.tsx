import { useState, useMemo } from "react";
import { AccordionItem, Checkbox } from "@carbon/react";
import CatalogBrowseLayout from "@/layouts/CatalogBrowseLayout";
import ArchitectureCard from "@/components/ArchitectureCard";

// Mock data for demonstration
const mockArchitectures = [
  {
    id: "1",
    title: "Digital assistant",
    description:
      "Enable digital assistants using Retrieval-Augmented Generation (RAG) to query a message, including custom documents and data.",
    tags: ["Digitize documents", "Find similar items", "Question and answer"],
    isCertified: true,
  },
  {
    id: "2",
    title: "Summarization",
    description:
      "Consolidates (a longer) input text into a brief statement or account of the main points.",
    tags: ["Digitize documents", "Extract and tag info"],
    isCertified: true,
  },
  {
    id: "3",
    title: "Sample Architecture",
    description: "Description of the architecture goes here.",
    tags: ["Question and answer", "Find similar items"],
    isCertified: false,
  },
  {
    id: "4",
    title: "Document Processing",
    description: "Advanced document processing with AI capabilities.",
    tags: ["Extract and tag info", "Digitize documents"],
    isCertified: true,
  },
  {
    id: "5",
    title: "Third-party Certified Solution",
    description: "A certified solution from a third-party provider.",
    tags: ["Question and answer", "Digitize documents"],
    isCertified: true,
  },
];

const CatalogDemo = () => {
  const [searchValue, setSearchValue] = useState("");
  const [selectedProviders, setSelectedProviders] = useState<string[]>([]);
  const [selectedServices, setSelectedServices] = useState<string[]>([]);

  const handleProviderChange = (checked: boolean, value: string) => {
    setSelectedProviders((prev) =>
      checked ? [...prev, value] : prev.filter((p) => p !== value),
    );
  };

  const handleServiceChange = (checked: boolean, value: string) => {
    setSelectedServices((prev) =>
      checked ? [...prev, value] : prev.filter((s) => s !== value),
    );
  };

  const handleClearFilters = () => {
    setSearchValue("");
    setSelectedProviders([]);
    setSelectedServices([]);
  };

  const totalSelectedFilters =
    selectedProviders.length + selectedServices.length;

  // Calculate dynamic counts
  const ibmCertifiedCount = mockArchitectures.filter(
    (arch) => arch.isCertified,
  ).length;
  const nonCertifiedCount = mockArchitectures.filter(
    (arch) => !arch.isCertified,
  ).length;

  const serviceCounts = useMemo(() => {
    const counts: Record<string, number> = {
      digitize: 0,
      extract: 0,
      similar: 0,
      qa: 0,
    };

    mockArchitectures.forEach((arch) => {
      arch.tags.forEach((tag) => {
        const lowerTag = tag.toLowerCase();
        if (lowerTag.includes("digitize")) counts.digitize++;
        if (lowerTag.includes("extract")) counts.extract++;
        if (lowerTag.includes("similar")) counts.similar++;
        if (lowerTag.includes("question")) counts.qa++;
      });
    });

    return counts;
  }, []);

  // Filter architectures based on selected filters only (not search)
  const filteredArchitectures = useMemo(() => {
    return mockArchitectures.filter((arch) => {
      const matchesProvider =
        selectedProviders.length === 0 ||
        (selectedProviders.includes("ibm-certified") && arch.isCertified) ||
        (selectedProviders.includes("non-certified") && !arch.isCertified);

      const matchesService =
        selectedServices.length === 0 ||
        arch.tags.some((tag) =>
          selectedServices.some((service) =>
            tag.toLowerCase().includes(service.toLowerCase()),
          ),
        );

      return matchesProvider && matchesService;
    });
  }, [selectedProviders, selectedServices]);

  // Filter options based on search
  const providerOptions = useMemo(() => {
    const options = [
      {
        label: "IBM certified (any provider)",
        value: "ibm-certified",
        count: ibmCertifiedCount,
      },
      {
        label: "Non-certified",
        value: "non-certified",
        count: nonCertifiedCount,
      },
    ];

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, ibmCertifiedCount, nonCertifiedCount]);

  const serviceOptions = useMemo(() => {
    const options = [
      {
        label: "Digitize documents",
        value: "digitize",
        count: serviceCounts.digitize,
      },
      {
        label: "Extract and tag info",
        value: "extract",
        count: serviceCounts.extract,
      },
      {
        label: "Find similar items",
        value: "similar",
        count: serviceCounts.similar,
      },
      { label: "Question and answer", value: "qa", count: serviceCounts.qa },
    ];

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, serviceCounts]);

  const filterAccordions = (
    <>
      {providerOptions.length > 0 && (
        <AccordionItem title="Provider" open>
          {providerOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`provider-${option.value}`}
              checked={selectedProviders.includes(option.value)}
              onChange={(_, { checked }) =>
                handleProviderChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}

      {serviceOptions.length > 0 && (
        <AccordionItem title="Services" open>
          {serviceOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`service-${option.value}`}
              checked={selectedServices.includes(option.value)}
              onChange={(_, { checked }) =>
                handleServiceChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}
    </>
  );

  const results =
    filteredArchitectures.length > 0 ? (
      <>
        {filteredArchitectures.map((arch) => (
          <ArchitectureCard
            key={arch.id}
            id={arch.id}
            title={arch.title}
            description={arch.description}
            tags={arch.tags}
            isCertified={arch.isCertified}
            onDeploy={(id) => console.log("Deploy:", id)}
            onLearnMore={(id) => console.log("Learn more:", id)}
          />
        ))}
      </>
    ) : null;

  return (
    <CatalogBrowseLayout
      title="Catalog Demo"
      subtitle="Production-ready AI solutions - Demo page showing CatalogBrowseLayout in action"
      searchValue={searchValue}
      onSearchChange={setSearchValue}
      totalSelectedFilters={totalSelectedFilters}
      onClearFilters={handleClearFilters}
      filterAccordions={filterAccordions}
      results={results}
      emptyMessage="No architectures match your filters. Try adjusting your search or clearing filters."
    />
  );
};

export default CatalogDemo;
