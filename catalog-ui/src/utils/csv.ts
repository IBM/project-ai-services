/**
 * Escapes CSV values according to RFC 4180 specification
 * - Wraps values in double quotes
 * - Escapes internal double quotes by doubling them
 * - Handles null/undefined values as empty strings
 *
 * @param value - The value to escape
 * @returns Escaped CSV string
 */
export const escapeCSV = (value: unknown): string => {
  const stringValue = value == null ? "" : String(value);
  return `"${stringValue.replace(/"/g, '""')}"`;
};

/**
 * Converts data rows to CSV format and triggers browser download
 *
 * @param data - Array of data objects to export
 * @param headers - Array of header definitions with key and header properties
 * @param filename - Name of the file to download (will be sanitized)
 * @throws Error if data is empty or headers are invalid
 *
 * @example
 * ```ts
 * exportToCSV(
 *   [{ id: '1', name: 'John' }],
 *   [{ key: 'id', header: 'ID' }, { key: 'name', header: 'Name' }],
 *   'users.csv'
 * );
 * ```
 */
export const exportToCSV = <T>(
  data: T[],
  headers: Array<{ key: string; header: unknown }>,
  filename: string,
): void => {
  // Validate inputs
  if (!data || data.length === 0) {
    throw new Error("Cannot export empty data");
  }

  if (!headers || headers.length === 0) {
    throw new Error("Headers are required for CSV export");
  }

  if (!filename || filename.trim().length === 0) {
    throw new Error("Filename is required for CSV export");
  }

  // Build CSV content
  const csvHeader = headers.map((h) => escapeCSV(h.header)).join(",");
  const csvBody = data
    .map((row) =>
      headers
        .map((h) => escapeCSV((row as Record<string, unknown>)[h.key]))
        .join(","),
    )
    .join("\n");
  const csv = `${csvHeader}\n${csvBody}`;

  // Create blob with UTF-8 BOM for Excel compatibility
  const BOM = "\uFEFF";
  const blob = new Blob([BOM + csv], { type: "text/csv;charset=utf-8;" });

  // Create and trigger download
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;

  document.body.appendChild(link);
  link.click();

  // Immediate cleanup (browser queues download before removal)
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
};

/**
 * Ensures filename has .csv extension
 * Removes any existing extension and adds .csv
 *
 * @param filename - The filename to process
 * @returns Filename with .csv extension
 *
 * @example
 * ```ts
 * ensureCSVExtension('data.txt') // returns 'data.csv'
 * ensureCSVExtension('data') // returns 'data.csv'
 * ensureCSVExtension('data.csv') // returns 'data.csv'
 * ```
 */
export const ensureCSVExtension = (filename: string): string => {
  if (!filename || filename.trim().length === 0) {
    return "export.csv";
  }

  const trimmed = filename.trim();
  const withoutExtension = trimmed.replace(/\.[^/.]+$/, "");

  // Sanitize filename - remove invalid characters
  // eslint-disable-next-line no-control-regex
  const sanitized = withoutExtension.replace(/[<>:"/\\|?*\x00-\x1F]/g, "_");

  return `${sanitized || "export"}.csv`;
};
