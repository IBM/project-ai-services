import type { ConnectorField } from "./schemaUtils";
import type { FormValues, CreateDatasourceRequest } from "./types";

function normalizePrivateKey(value: string): string {
  // Convert literal \n escape sequences to real newlines first.
  const withNewlines = value.replace(/\\n/g, "\n");

  // If the key is still a single line, try to reconstruct it from the PEM parts.
  const match = withNewlines.match(
    /^(-----BEGIN [^-]+-----)\s+([A-Za-z0-9+/=\s]+?)\s+(-----END [^-]+-----)$/,
  );
  if (!match) return withNewlines;

  const [, header, rawBody, footer] = match;
  const body = rawBody
    .replace(/\s+/g, "")
    .match(/.{1,64}/g)!
    .join("\n");
  return `${header}\n${body}\n${footer}`;
}

/**
 * Transforms the AddDataSourceModal form state into the
 * `CreateDatasourceRequest` payload expected by POST /api/v1/datasources.
 */
export function transformToCreateDatasourcePayload(
  name: string,
  providerId: string,
  formValues: FormValues,
  fields: ConnectorField[],
): CreateDatasourceRequest {
  const params: Record<string, string | string[]> = {};

  for (const field of fields) {
    const raw = formValues[field.key];

    if (field.type === "checkboxArray") {
      const arr = (raw as string[]) ?? [];
      if (arr.length > 0) {
        params[field.key] = arr;
      }
    } else {
      let str = ((raw as string) ?? "").trim();
      if (field.key === "private_key") {
        str = normalizePrivateKey(str);
      }
      if (str !== "") {
        params[field.key] = str;
      }
    }
  }

  return {
    name,
    provider_id: providerId,
    params,
  };
}
