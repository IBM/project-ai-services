import { useState, useMemo } from "react";
import { AccordionItem, Checkbox, CheckboxGroup } from "@carbon/react";
import CatalogBrowseLayout from "@/layouts/CatalogBrowseLayout";

const Services = () => {
  const [searchValue, setSearchValue] = useState("");
  const [selectedProviders, setSelectedProviders] = useState<string[]>([]);

  const handleProviderChange = (checked: boolean, value: string) => {
    setSelectedProviders((prev) =>
      checked ? [...prev, value] : prev.filter((p) => p !== value),
    );
  };

  const handleClearFilters = () => {
    setSearchValue("");
    setSelectedProviders([]);
  };

  const totalSelectedFilters = selectedProviders.length;

  // Calculate dynamic counts - all zeros since no mock data
  const ibmCount = 0;
  const ibmCertifiedAnyProviderCount = 0;

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

  const filterAccordions = (
    <>
      {providerOptions.length > 0 && (
        <AccordionItem title="Provider" open>
          <CheckboxGroup legendText="">
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
          </CheckboxGroup>
        </AccordionItem>
      )}
    </>
  );

  // TODO: Entire page code needs to be updated
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
