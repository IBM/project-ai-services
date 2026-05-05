import { useState, useMemo } from "react";
import { AccordionItem, Checkbox } from "@carbon/react";
import CatalogBrowseLayout from "@/layouts/CatalogBrowseLayout";

const Services = () => {
  const [searchValue, setSearchValue] = useState("");
  const [selectedProviders, setSelectedProviders] = useState<string[]>([]);
  const [selectedArchitectures, setSelectedArchitectures] = useState<string[]>(
    [],
  );

  const handleProviderChange = (checked: boolean, value: string) => {
    setSelectedProviders((prev) =>
      checked ? [...prev, value] : prev.filter((p) => p !== value),
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
    setSelectedArchitectures([]);
  };

  const totalSelectedFilters =
    selectedProviders.length + selectedArchitectures.length;

  // Calculate dynamic counts - all zeros since no mock data
  const ibmCount = 0;
  const ibmCertifiedAnyProviderCount = 0;

  const architectureCounts = useMemo(() => {
    return {
      "data-content": 0,
      "deep-process": 0,
      "digital-assistant": 0,
      forecasting: 0,
      "fraud-detection": 0,
      "image-video": 0,
      recommender: 0,
    };
  }, []);

  // Filter options based on search
  const providerOptions = useMemo(() => {
    const options = [
      { label: "IBM", value: "ibm", count: ibmCount },
      {
        label: "IBM certified (any provider)",
        value: "ibm-certified",
        count: ibmCertifiedAnyProviderCount,
      },
    ];

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, ibmCount, ibmCertifiedAnyProviderCount]);

  const architectureOptions = useMemo(() => {
    const options = [
      {
        label: "Data and content mgmt",
        value: "data-content",
        count: architectureCounts["data-content"],
      },
      {
        label: "Deep process integration",
        value: "deep-process",
        count: architectureCounts["deep-process"],
      },
      {
        label: "Digital assistant",
        value: "digital-assistant",
        count: architectureCounts["digital-assistant"],
      },
      {
        label: "Forecasting",
        value: "forecasting",
        count: architectureCounts.forecasting,
      },
      {
        label: "Fraud detection",
        value: "fraud-detection",
        count: architectureCounts["fraud-detection"],
      },
      {
        label: "Image and video analysis",
        value: "image-video",
        count: architectureCounts["image-video"],
      },
      {
        label: "Recommender system",
        value: "recommender",
        count: architectureCounts.recommender,
      },
    ];

    if (!searchValue) return options;

    return options.filter((opt) =>
      opt.label.toLowerCase().includes(searchValue.toLowerCase()),
    );
  }, [searchValue, architectureCounts]);

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

  // No results - service cards are being implemented by another developer
  const results = null;

  return (
    <CatalogBrowseLayout
      title="Services"
      subtitle="Pre-built AI demos from real-world use cases to help you envision how AI can solve common business problems."
      searchValue={searchValue}
      onSearchChange={setSearchValue}
      totalSelectedFilters={totalSelectedFilters}
      onClearFilters={handleClearFilters}
      filterAccordions={filterAccordions}
      results={results}
      emptyMessage="No services match your filters. Try adjusting your search or clearing filters."
    />
  );
};

export default Services;
