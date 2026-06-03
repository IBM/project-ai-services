/** Escapes CSV values per RFC 4180 by wrapping in quotes and doubling internal quotes */
export const escapeCSV = (value: unknown): string => {
  const stringValue = value == null ? "" : String(value);
  return `"${stringValue.replace(/"/g, '""')}"`;
};

/** Converts data to CSV format with UTF-8 BOM and triggers browser download */
export const exportToCSV = <T>(
  data: T[],
  headers: Array<{ key: string; header: unknown }>,
  filename: string,
): void => {
  if (!data || data.length === 0) {
    throw new Error("Cannot export empty data");
  }

  if (!headers || headers.length === 0) {
    throw new Error("Headers are required for CSV export");
  }

  if (!filename || filename.trim().length === 0) {
    throw new Error("Filename is required for CSV export");
  }

  const csvHeader = headers.map((h) => escapeCSV(h.header)).join(",");
  const csvBody = data
    .map((row) =>
      headers
        .map((h) => escapeCSV((row as Record<string, unknown>)[h.key]))
        .join(","),
    )
    .join("\n");
  const csv = `${csvHeader}\n${csvBody}`;

  const BOM = "\uFEFF";
  const blob = new Blob([BOM + csv], { type: "text/csv;charset=utf-8;" });

  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;

  document.body.appendChild(link);
  link.click();

  document.body.removeChild(link);
  URL.revokeObjectURL(url);
};

/** Sanitizes filename and ensures it has .csv extension */
export const ensureCSVExtension = (filename: string): string => {
  if (!filename || filename.trim().length === 0) {
    return "export.csv";
  }

  const trimmed = filename.trim();
  const withoutExtension = trimmed.replace(/\.[^/.]+$/, "");

  // eslint-disable-next-line no-control-regex
  const sanitized = withoutExtension.replace(/[<>:"/\\|?*\x00-\x1F]/g, "_");

  return `${sanitized || "export"}.csv`;
};

/** Validates, flattens data with children, and exports to CSV with proper error handling */
export const downloadCSVWithChildren = <T extends { children?: T[] }>(
  data: T[],
  headers: Array<{ key: string; header: unknown }>,
  filename: string,
): { success: boolean; message: string; rowCount?: number } => {
  try {
    // Validate filename
    if (!filename || filename.trim().length === 0) {
      return {
        success: false,
        message: "Provide a valid file name",
      };
    }

    // Validate data
    if (!data || data.length === 0) {
      return {
        success: false,
        message: "No data available to export",
      };
    }

    // Flatten data to include child rows
    const flattenedRows: T[] = [];
    data.forEach((row) => {
      flattenedRows.push(row);
      if (row.children && row.children.length > 0) {
        flattenedRows.push(...row.children);
      }
    });

    // Ensure filename has .csv extension
    const sanitizedFilename = ensureCSVExtension(filename);

    // Always exclude actions column from export
    const exportableHeaders = headers.filter((h) => h.key !== "actions");

    // Export to CSV
    exportToCSV(flattenedRows, exportableHeaders, sanitizedFilename);

    return {
      success: true,
      message: `Successfully exported ${flattenedRows.length} rows to ${sanitizedFilename}`,
      rowCount: flattenedRows.length,
    };
  } catch (error) {
    return {
      success: false,
      message:
        error instanceof Error
          ? error.message
          : "An error occurred while exporting the CSV file. Please try again.",
    };
  }
};
