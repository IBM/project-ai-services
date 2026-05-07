import { useState, useMemo } from "react";
import { AccordionItem, Checkbox } from "@carbon/react";
import CatalogBrowseLayout from "@/layouts/CatalogBrowseLayout";
import ArchitectureCard from "@/components/ArchitectureCard";

// Mock data
const mockArchitectures = [
  {
    id: "1",
    title: "Digital assistant",
    description:
      "Enable digital assistants using Retrieval-Augmented Generation (RAG), including AI services that query a managed knowledge base to answer questions from custom documents and data.",
    tags: ["Digitize documents", "Find similar items", "Question and answer"],
    isCertified: true,
  },
];

const Architectures = () => {
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

  // Calculate dynamic counts based on mock data
  const providerCounts = useMemo(() => {
    return {
      ibmCertified: mockArchitectures.filter((arch) => arch.isCertified).length,
      nonCertified: mockArchitectures.filter((arch) => !arch.isCertified)
        .length,
    };
  }, []);

  const serviceCounts = useMemo(() => {
    const counts: Record<string, number> = {};

    mockArchitectures.forEach((arch) => {
      arch.tags.forEach((tag) => {
        const key = tag.toLowerCase().replace(/\s+/g, "-");
        counts[key] = (counts[key] || 0) + 1;
      });
    });

    return counts;
  }, []);

  // Get unique service tags dynamically
  const uniqueServices = useMemo(() => {
    const services = new Set<string>();
    mockArchitectures.forEach((arch) => {
      arch.tags.forEach((tag) => services.add(tag));
    });
    return Array.from(services).sort();
  }, []);

  // Filter architectures based on selected filters and search
  const filteredArchitectures = useMemo(() => {
    return mockArchitectures.filter((arch) => {
      const matchesProvider =
        selectedProviders.length === 0 ||
        (selectedProviders.includes("ibm-certified") && arch.isCertified) ||
        (selectedProviders.includes("non-certified") && !arch.isCertified);

      const matchesService =
        selectedServices.length === 0 ||
        arch.tags.some((tag) => {
          const normalizedTag = tag.toLowerCase().replace(/\s+/g, "-");
          return selectedServices.includes(normalizedTag);
        });

      // Search in card content (title, description, tags)
      const matchesSearch =
        !searchValue ||
        arch.title.toLowerCase().includes(searchValue.toLowerCase()) ||
        arch.description.toLowerCase().includes(searchValue.toLowerCase()) ||
        arch.tags.some((tag) =>
          tag.toLowerCase().includes(searchValue.toLowerCase()),
        );

      return matchesProvider && matchesService && matchesSearch;
    });
  }, [selectedProviders, selectedServices, searchValue]);

  // Filter options
  const providerOptions = useMemo(() => {
    return [
      {
        label: "IBM certified",
        value: "ibm-certified",
        count: providerCounts.ibmCertified,
      },
      {
        label: "Non-certified",
        value: "non-certified",
        count: providerCounts.nonCertified,
      },
    ];
  }, [providerCounts]);

  const serviceOptions = useMemo(() => {
    return uniqueServices.map((service) => {
      const key = service.toLowerCase().replace(/\s+/g, "-");
      return {
        label: service,
        value: key,
        count: serviceCounts[key] || 0,
      };
    });
  }, [uniqueServices, serviceCounts]);

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
      title="Architectures"
      subtitle="Production-ready AI solutions that combine multiple services into complete, integrated systems for complex use cases."
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

export default Architectures;
