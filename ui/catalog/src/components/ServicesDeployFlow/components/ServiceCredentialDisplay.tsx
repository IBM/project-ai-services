import React, { useState } from "react";
import { Button } from "@carbon/react";
import { View, ViewOff } from "@carbon/icons-react";
import type { ProviderSchema } from "../../../services/deployment.api";
import styles from "../ServicesDeployFlow.module.scss";

interface ServiceCredentialDisplayProps {
  providerId: string;
  providerSchema: ProviderSchema | null;
  values: Record<string, unknown>;
  serviceId: string;
  className?: string;
}

export const ServiceCredentialDisplay: React.FC<
  ServiceCredentialDisplayProps
> = ({ providerSchema, values, serviceId, className }) => {
  const [showApiKey, setShowApiKey] = useState<Record<string, boolean>>({});

  if (!providerSchema || !providerSchema.properties) {
    return null;
  }

  const requiredFields = providerSchema.required || [];

  // Get credential fields (exclude 'model' field)
  const credentialFields = Object.entries(providerSchema.properties)
    .filter(([key]) => key !== "model")
    .map(([key, property]) => ({
      key,
      title: property?.title || key,
      description: property?.description || "",
      type: property?.type || "string",
      format: property?.format || "text",
      required: requiredFields.includes(key),
    }));

  return (
    <>
      {credentialFields.map((field) => {
        const fieldValue = values?.[field.key];

        // Show required fields even if empty, with a warning indicator
        if (!fieldValue && !field.required) return null;

        const isPasswordField = field.format === "password";
        const fieldKey = `${serviceId}-${field.key}`;

        return (
          <div
            key={field.key}
            className={className || styles.serviceConfigItem}
          >
            <span className={styles.serviceConfigItemLabel}>
              {field.title}
              {field.required && !fieldValue && (
                <span style={{ color: "#da1e28", marginLeft: "4px" }}>*</span>
              )}
            </span>
            <span className={styles.serviceConfigItemValue}>
              {!fieldValue ? (
                <span style={{ color: "#da1e28", fontStyle: "italic" }}>
                  Required - Click Edit to add
                </span>
              ) : isPasswordField ? (
                <>
                  <span className={styles.apiKeyValue}>
                    {showApiKey[fieldKey] ? String(fieldValue) : "•".repeat(20)}
                  </span>
                  <Button
                    kind="ghost"
                    size="sm"
                    hasIconOnly
                    renderIcon={showApiKey[fieldKey] ? ViewOff : View}
                    iconDescription={
                      showApiKey[fieldKey]
                        ? `Hide ${field.title}`
                        : `Show ${field.title}`
                    }
                    onClick={() =>
                      setShowApiKey((prev) => ({
                        ...prev,
                        [fieldKey]: !prev[fieldKey],
                      }))
                    }
                    className={styles.apiKeyToggle}
                  />
                </>
              ) : (
                String(fieldValue)
              )}
            </span>
          </div>
        );
      })}
    </>
  );
};

// Made with Bob
