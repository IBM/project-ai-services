import { useState, useEffect } from "react";
import { fetchServiceDetails } from "@/api/serviceDetails.api";
import { transformServiceDetails } from "@/utils/serviceDetailsTransform";
import { useServiceDetailsStore } from "@/store/serviceDetails.store";
import { dedupe } from "@/utils/requestManager";
import { CatalogDetailPanel } from "@/components/CatalogDetailPanel";
import type { CatalogAboutSection } from "@/types/api.types";

export interface ServiceDetailData {
  id: string;
  title: string;
  description: string;
  certifiedBy?: string;
  tags?: string[];
  standalone?: boolean;
  about: CatalogAboutSection[];
}

export interface ServiceDetailPanelProps {
  open: boolean;
  onClose: () => void;
  serviceId: string | null;
}

const ServiceDetailPanel = ({
  open,
  onClose,
  serviceId,
}: ServiceDetailPanelProps) => {
  const [failedId, setFailedId] = useState<string | null>(null);

  const setServiceDetails = useServiceDetailsStore((s) => s.setServiceDetails);

  const service = useServiceDetailsStore((s) =>
    serviceId ? (s.serviceDetailsCache[serviceId] ?? null) : null,
  );

  const hasFailed = failedId !== null && failedId === serviceId;
  const isLoading = open && !!serviceId && !service && !hasFailed;

  const error = hasFailed
    ? "Failed to load service details. Please try again."
    : null;

  useEffect(() => {
    if (!open || !serviceId || service) return;

    let cancelled = false;

    dedupe(`serviceDetails:${serviceId}`, () => fetchServiceDetails(serviceId))
      .then((serviceData) => {
        if (cancelled) return;

        setServiceDetails(serviceId, transformServiceDetails(serviceData));
      })
      .catch(() => {
        if (cancelled) return;

        setFailedId(serviceId);
      });

    return () => {
      cancelled = true;
      // Clear a prior failure so reopening or switching service retries cleanly
      setFailedId(null);
    };
  }, [open, serviceId, service, setServiceDetails]);

  return (
    <CatalogDetailPanel
      open={open}
      onClose={onClose}
      title={service?.title || "Service Details"}
      description={service?.description}
      certifiedBy={service?.certifiedBy}
      about={service?.about}
      isLoading={isLoading}
      error={error}
    />
  );
};

export default ServiceDetailPanel;
