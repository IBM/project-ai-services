import { useEffect, useReducer } from "react";
import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import { ServiceCard, ServiceDetailPanel } from "@/components";
import type { ServiceDetailData } from "@/components";
import styles from "./Services.module.scss";
import { DeployedServicesTable } from "@/components";
import { api } from "@/api/axios";
import { SERVICE_ENDPOINTS } from "@/constants/api-endpoints.constants";
import { servicesReducer, INITIAL_STATE, ACTION_TYPES } from "./types";

export interface ServiceApiResponse {
  id: string;
  name: string;
  description: string;
  certified_by: string;
  architectures: string[];
}

export const getServices = async (): Promise<ServiceApiResponse[]> => {
  const response = await api.get<ServiceApiResponse[]>(
    SERVICE_ENDPOINTS.GET_SERVICES,
  );
  return response.data;
};

// Transform API response to ServiceDetailData format
const transformServiceData = (
  apiService: ServiceApiResponse,
): ServiceDetailData => {
  return {
    id: apiService.id,
    title: apiService.name,
    description: apiService.description,
    isCertified: apiService.certified_by === "IBM",
    tags: apiService.architectures,
  };
};

const Services = () => {
  const [state, dispatch] = useReducer(servicesReducer, INITIAL_STATE);

  const handleCardClick = (id: string) => {
    const service = state.mockServices.find((s) => s.id === id);
    console.log(service);
    if (service) {
      dispatch({
        type: ACTION_TYPES.SERVICES_SET_SELECTED_SERVICE,
        payload: service,
      });
      dispatch({ type: ACTION_TYPES.SERVICES_SET_PANEL_OPEN, payload: true });
    }
  };

  useEffect(() => {
    fetch("/data/services.json")
      .then((res) => res.json())
      .then((data) => {
        dispatch({
          type: ACTION_TYPES.SERVICES_SET_MOCK_SERVICES,
          payload: data,
        });
      });
  }, []);

  const fetchServices = async () => {
    try {
      dispatch({ type: ACTION_TYPES.SERVICES_SET_LOADING, payload: true });
      dispatch({ type: ACTION_TYPES.SERVICES_SET_ERROR, payload: null });
      const data = await getServices();
      const transformedData = data.map(transformServiceData);
      dispatch({
        type: ACTION_TYPES.SERVICES_SET_SERVICES,
        payload: transformedData,
      });
      dispatch({
        type: ACTION_TYPES.SERVICES_SET_HAS_FETCHED_SERVICES,
        payload: true,
      });
    } catch (err) {
      console.error("Failed to fetch services:", err);
      dispatch({
        type: ACTION_TYPES.SERVICES_SET_ERROR,
        payload: "Failed to load services. Please try again later.",
      });
    } finally {
      dispatch({ type: ACTION_TYPES.SERVICES_SET_LOADING, payload: false });
    }
  };

  const handleTabChange = (evt: { selectedIndex: number }) => {
    // Catalog tab is at index 1
    if (evt.selectedIndex === 1 && !state.hasFetchedServices) {
      fetchServices();
    }
  };

  const handleDeploy = () => {};

  const handleClosePanel = () => {
    dispatch({ type: ACTION_TYPES.SERVICES_CLOSE_PANEL });
    setTimeout(() => {
      dispatch({
        type: ACTION_TYPES.SERVICES_SET_SELECTED_SERVICE,
        payload: null,
      });
    }, 300);
  };

  return (
    <div className={styles.servicesContainer}>
      <PageHeader
        title="Services"
        subtitle="Single-purpose AI capabilities designed to perform specific tasks independently or as part of larger solutions."
        className={styles.pageHeader}
      />
      <Tabs onChange={handleTabChange}>
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
            <DeployedServicesTable />
          </TabPanel>
          <TabPanel>
            {state.loading ? (
              <div className={styles.loadingMessage}>Loading services...</div>
            ) : state.error ? (
              <div className={styles.errorMessage}>{state.error}</div>
            ) : (
              <div className={styles.catalogGrid}>
                {state.services.map((service) => (
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
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>

      <ServiceDetailPanel
        open={state.isPanelOpen}
        onClose={handleClosePanel}
        service={state.selectedService}
      />
    </div>
  );
};

export default Services;
