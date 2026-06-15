import { useReducer, useMemo } from "react";
import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import { ServiceCard, ServiceDetailPanel } from "@/components";
import ServiceCardSkeleton from "@/components/ServiceCard/ServiceCardSkeleton";
import type { ServiceDetailData } from "@/components";
import styles from "./Services.module.scss";
import { DeployedServicesTable } from "@/components";
import { ServicesDeployFlow } from "@/components/ServicesDeployFlow";
import DeploymentDetails from "@/components/DeploymentDetails";
import { useServices } from "@/hooks/useServices";
import { servicesReducer, initialState } from "./types";
import type { Service } from "@/services/deployment.api";
import type { DeploymentDetails as DeploymentDetailsType } from "@/types/digitalAssistants";

const sortServicesByDeployabilityAndName = (
  services: ServiceDetailData[],
): ServiceDetailData[] => {
  return [...services].sort((serviceA, serviceB) => {
    const serviceAIsDeployable = serviceA.standalone === true;
    const serviceBIsDeployable = serviceB.standalone === true;

    if (serviceAIsDeployable !== serviceBIsDeployable) {
      return serviceAIsDeployable ? -1 : 1;
    }

    return serviceA.title.localeCompare(serviceB.title);
  });
};

// Transform Service to ServiceDetailData format
const transformServiceData = (service: Service): ServiceDetailData => {
  return {
    id: service.id,
    title: service.name,
    description: service.description,
    certifiedBy: service.certified_by,
    tags: service.architectures || [],
    standalone: service.standalone,
    about: [], // About sections are loaded separately when viewing details
  };
};

const Services = () => {
  // Use unified services cache
  const { services, isLoading, error, refetch } = useServices();

  // Local UI state using useReducer
  const [state, dispatch] = useReducer(servicesReducer, initialState);

  // Transform and sort services for catalog display
  const catalogServices = useMemo(() => {
    const transformed = services.map(transformServiceData);
    return sortServicesByDeployabilityAndName(transformed);
  }, [services]);

  const handleCardClick = (id: string) => {
    dispatch({ type: "OPEN_PANEL", payload: id });
  };

  const handleTabChange = (evt: { selectedIndex: number }) => {
    dispatch({ type: "SET_SELECTED_TAB", payload: evt.selectedIndex });
    // Catalog tab is at index 1
    // Services are static data, only fetch if not already cached
    if (evt.selectedIndex === 1 && services.length === 0) {
      refetch();
    }
  };

  const handleDeploy = (serviceId: string) => {
    // Open DeployFlow with pre-selected service (from service card)
    dispatch({ type: "OPEN_DEPLOY_FLOW", payload: serviceId });
  };

  const handleDeployFromTable = () => {
    // Open DeployFlow without pre-selected service (from table)
    dispatch({ type: "OPEN_DEPLOY_FLOW", payload: null });
  };

  const handleCloseDeployFlow = () => {
    dispatch({ type: "CLOSE_DEPLOY_FLOW" });
    // Clear selected service after animation
    setTimeout(() => {
      dispatch({ type: "CLEAR_DEPLOY_SERVICE_ID" });
    }, 300);
  };

  const handleDeploySubmit = () => {
    // Refresh deployed services table after successful deployment
    // Switch to Deployments tab (index 0)
    dispatch({ type: "DEPLOY_SUBMIT" });
  };

  const handleClosePanel = () => {
    dispatch({ type: "CLOSE_PANEL" });
    setTimeout(() => {
      dispatch({ type: "CLEAR_SELECTED_SERVICE_ID" });
    }, 300);
  };

  const handleShowDeploymentDetails = (deployment: DeploymentDetailsType) => {
    dispatch({ type: "SHOW_DEPLOYMENT_DETAILS", payload: deployment });
  };

  const handleBackFromDetails = () => {
    dispatch({ type: "HIDE_DEPLOYMENT_DETAILS" });
  };

  // If showing deployment details, render DeploymentDetails component
  if (state.showDeploymentDetails && state.selectedDeployment) {
    return (
      <DeploymentDetails
        deployment={state.selectedDeployment}
        onBack={handleBackFromDetails}
        deploymentSource="Services"
      />
    );
  }

  return (
    <div className={styles.servicesContainer}>
      <PageHeader
        title="Services"
        subtitle="Single-purpose AI capabilities designed to perform specific tasks independently or as part of larger solutions."
        className={styles.pageHeader}
      />
      <Tabs selectedIndex={state.selectedTabIndex} onChange={handleTabChange}>
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
            <DeployedServicesTable
              onDeploy={handleDeployFromTable}
              refreshTrigger={state.tableRefreshTrigger}
              onRowClick={handleShowDeploymentDetails}
            />
          </TabPanel>
          <TabPanel>
            {isLoading ? (
              <div className={styles.catalogGrid}>
                {Array.from({ length: 5 }).map((_, index) => (
                  <ServiceCardSkeleton key={`skeleton-${index}`} />
                ))}
              </div>
            ) : error ? (
              <div className={styles.errorMessage}>{error}</div>
            ) : (
              <div className={styles.catalogGrid}>
                {catalogServices.map((service) => (
                  <ServiceCard
                    key={service.id}
                    id={service.id}
                    title={service.title}
                    description={service.description}
                    certifiedBy={service.certifiedBy}
                    standalone={service.standalone}
                    onDeploy={handleDeploy}
                    onLearnMore={handleCardClick}
                  />
                ))}
              </div>
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>

      <ServiceDetailPanel
        open={state.isPanelOpen}
        onClose={handleClosePanel}
        serviceId={state.selectedServiceId}
      />

      <ServicesDeployFlow
        open={state.isDeployFlowOpen}
        onClose={handleCloseDeployFlow}
        onSubmit={handleDeploySubmit}
        preSelectedServiceId={state.deployServiceId || undefined}
      />
    </div>
  );
};

export default Services;
