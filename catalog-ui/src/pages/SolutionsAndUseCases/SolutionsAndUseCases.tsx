import { useState, useMemo } from "react";
import { AccordionItem, Checkbox } from "@carbon/react";
import CatalogBrowseLayout from "@/layouts/CatalogBrowseLayout";
import SolutionCard from "@/components/SolutionCard";

// Mock data
const mockSolutions = [
  {
    id: "1",
    title: "Agriculture assistant",
    description:
      "Provides farmers with accurate, explainable crop-growing advice in their native language.",
    domain: "Agriculture",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "2",
    title: "Banking assistant",
    description:
      "Enables finance teams to investigate fraud, identify patterns, and uncover their core data—without needing specialized AI skills such as LLMs.",
    domain: "Banking and finance",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "3",
    title: "AI assistant",
    description:
      "Enables teams to ask a question to an AI assistant, get an answer, why it matters, and what to do next.",
    domain: "Enterprise resource plan",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "4",
    title: "Claims & policy manager",
    description:
      "Helps customers create accurate, complete, and compliant insurance claims against active policies, and guiding them through the process.",
    domain: "Insurance",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "5",
    title: "Conference slide search",
    description:
      "Discusses conference experiences by parsing text content from slides from current and past events through natural language conversation.",
    domain: "Conferences",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "6",
    title: "Invoice matching assistant",
    description:
      "Ensures accurate, complete, and compliant invoice matching through automated multi-way invoice matching.",
    domain: "Enterprise resource plan",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "7",
    title: "IT service desk assistant",
    description:
      "Enables teams to quickly resolve everyday IT issues, automate common support tasks, and get instant help—without waiting for a technician.",
    domain: "IT operations",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
  {
    id: "8",
    title: "Financial document assistant",
    description:
      "Enables conversational self-service for financial FAQs by helping users quickly find information without digging through documents—reducing support burden.",
    domain: "Banking and finance",
    assets: ["Digital assistant"],
    architectures: ["Digital assistant"],
    isCertified: true,
  },
];

const SolutionsAndUseCases = () => {
  const [searchValue, setSearchValue] = useState("");
  const [selectedProviders, setSelectedProviders] = useState<string[]>([]);
  const [selectedDomains, setSelectedDomains] = useState<string[]>([]);
  const [selectedAssets, setSelectedAssets] = useState<string[]>([]);
  const [selectedArchitectures, setSelectedArchitectures] = useState<string[]>(
    [],
  );

  const handleProviderChange = (checked: boolean, value: string) => {
    setSelectedProviders((prev) =>
      checked ? [...prev, value] : prev.filter((p) => p !== value),
    );
  };

  const handleDomainChange = (checked: boolean, value: string) => {
    setSelectedDomains((prev) =>
      checked ? [...prev, value] : prev.filter((d) => d !== value),
    );
  };

  const handleAssetChange = (checked: boolean, value: string) => {
    setSelectedAssets((prev) =>
      checked ? [...prev, value] : prev.filter((a) => a !== value),
    );
  };

  const handleArchitectureChange = (checked: boolean, value: string) => {
    setSelectedArchitectures((prev) =>
      checked ? [...prev, value] : prev.filter((a) => a !== value),
    );
  };

  const handleClearFilters = () => {
    setSearchValue("");
    setSelectedProviders([]);
    setSelectedDomains([]);
    setSelectedAssets([]);
    setSelectedArchitectures([]);
  };

  const totalSelectedFilters =
    selectedProviders.length +
    selectedDomains.length +
    selectedAssets.length +
    selectedArchitectures.length;

  // Calculate dynamic counts
  const ibmCount = mockSolutions.filter((sol) => sol.isCertified).length;

  const domainCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    mockSolutions.forEach((sol) => {
      const key = sol.domain.toLowerCase().replace(/\s+/g, "-");
      counts[key] = (counts[key] || 0) + 1;
    });
    return counts;
  }, []);

  const assetCounts = useMemo(() => {
    const counts: Record<string, number> = {};

    mockSolutions.forEach((sol) => {
      sol.assets.forEach((asset) => {
        const key = asset.toLowerCase().replace(/\s+/g, "-");
        counts[key] = (counts[key] || 0) + 1;
      });
    });

    return counts;
  }, []);

  const architectureCounts = useMemo(() => {
    const counts: Record<string, number> = {};

    mockSolutions.forEach((sol) => {
      sol.architectures.forEach((arch) => {
        const key = arch.toLowerCase().replace(/\s+/g, "-");
        counts[key] = (counts[key] || 0) + 1;
      });
    });

    return counts;
  }, []);

  // Get unique assets and architectures dynamically
  const uniqueAssets = useMemo(() => {
    const assets = new Set<string>();
    mockSolutions.forEach((sol) => {
      sol.assets.forEach((asset) => assets.add(asset));
    });
    return Array.from(assets).sort();
  }, []);

  const uniqueArchitectures = useMemo(() => {
    const architectures = new Set<string>();
    mockSolutions.forEach((sol) => {
      sol.architectures.forEach((arch) => architectures.add(arch));
    });
    return Array.from(architectures).sort();
  }, []);

  // Filter solutions based on selected filters
  const filteredSolutions = useMemo(() => {
    return mockSolutions.filter((sol) => {
      const matchesProvider =
        selectedProviders.length === 0 ||
        (selectedProviders.includes("ibm") && sol.isCertified);

      const matchesDomain =
        selectedDomains.length === 0 ||
        selectedDomains.some((domain) => {
          const normalizedSolDomain = sol.domain
            .toLowerCase()
            .replace(/\s+/g, "-");
          return normalizedSolDomain === domain;
        });

      const matchesAsset =
        selectedAssets.length === 0 ||
        sol.assets.some((asset) => {
          const normalizedAsset = asset.toLowerCase().replace(/\s+/g, "-");
          return selectedAssets.includes(normalizedAsset);
        });

      const matchesArchitecture =
        selectedArchitectures.length === 0 ||
        sol.architectures.some((arch) => {
          const normalizedArch = arch.toLowerCase().replace(/\s+/g, "-");
          return selectedArchitectures.includes(normalizedArch);
        });

      return (
        matchesProvider && matchesDomain && matchesAsset && matchesArchitecture
      );
    });
  }, [
    selectedProviders,
    selectedDomains,
    selectedAssets,
    selectedArchitectures,
  ]);

  // Filter options based on search
  const providerOptions = useMemo(() => {
    const options = [{ label: "IBM", value: "ibm", count: ibmCount }];

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, ibmCount]);

  const domainOptions = useMemo(() => {
    // Dynamically generate domain options from actual data
    const uniqueDomains = Array.from(
      new Set(mockSolutions.map((sol) => sol.domain)),
    );

    const options = uniqueDomains
      .map((domain) => {
        const key = domain.toLowerCase().replace(/\s+/g, "-");
        return {
          label: domain,
          value: key,
          count: domainCounts[key] || 0,
        };
      })
      .sort((a, b) => a.label.localeCompare(b.label));

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, domainCounts]);

  const assetOptions = useMemo(() => {
    const options = uniqueAssets.map((asset) => {
      const key = asset.toLowerCase().replace(/\s+/g, "-");
      return {
        label: asset,
        value: key,
        count: assetCounts[key] || 0,
      };
    });

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, uniqueAssets, assetCounts]);

  const architectureOptions = useMemo(() => {
    const options = uniqueArchitectures.map((arch) => {
      const key = arch.toLowerCase().replace(/\s+/g, "-");
      return {
        label: arch,
        value: key,
        count: architectureCounts[key] || 0,
      };
    });

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, uniqueArchitectures, architectureCounts]);

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

      {domainOptions.length > 0 && (
        <AccordionItem title="Domains" open>
          {domainOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`domain-${option.value}`}
              checked={selectedDomains.includes(option.value)}
              onChange={(_, { checked }) =>
                handleDomainChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}

      {assetOptions.length > 0 && (
        <AccordionItem title="Assets" open>
          {assetOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`asset-${option.value}`}
              checked={selectedAssets.includes(option.value)}
              onChange={(_, { checked }) =>
                handleAssetChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}

      {architectureOptions.length > 0 && (
        <AccordionItem title="Architectures" open>
          {architectureOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`architecture-${option.value}`}
              checked={selectedArchitectures.includes(option.value)}
              onChange={(_, { checked }) =>
                handleArchitectureChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}
    </>
  );

  const results =
    filteredSolutions.length > 0 ? (
      <>
        {filteredSolutions.map((sol) => (
          <SolutionCard
            key={sol.id}
            id={sol.id}
            title={sol.title}
            description={sol.description}
            tags={sol.assets}
            category={sol.domain}
            onViewDetails={(id) => console.log("View details:", id)}
          />
        ))}
      </>
    ) : null;

  return (
    <CatalogBrowseLayout
      title="Solutions and use cases"
      subtitle="Pre-built AI demos from real-world use cases to help you envision how AI can solve common business problems."
      searchValue={searchValue}
      onSearchChange={setSearchValue}
      totalSelectedFilters={totalSelectedFilters}
      onClearFilters={handleClearFilters}
      filterAccordions={filterAccordions}
      results={results}
      emptyMessage="No solutions match your filters. Try adjusting your search or clearing filters."
    />
  );
};

export default SolutionsAndUseCases;
