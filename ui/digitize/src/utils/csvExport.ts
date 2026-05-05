/**
 * Utility functions for CSV export functionality
 */

export interface CSVExportOptions {
  filename: string;
  headers: Array<{ key: string; header: string }>;
  rows: Array<Record<string, any>>;
  excludeColumns?: string[];
}

/**
 * Escapes CSV values by wrapping them in quotes and escaping internal quotes
 */
const escapeCSV = (value: unknown): string => {
  return `"${String(value ?? '').replace(/"/g, '""')}"`;
};

/**
 * Extracts plain text from React elements or returns the value as-is
 */
const extractTextContent = (value: any): string => {
  if (value === null || value === undefined) {
    return '';
  }
  
  // If it's a React element, try to extract text content
  if (typeof value === 'object' && value.props) {
    // Handle common patterns like status cells with icons and text
    if (value.props.children) {
      const children = Array.isArray(value.props.children) 
        ? value.props.children 
        : [value.props.children];
      
      return children
        .map((child: any) => {
          if (typeof child === 'string') return child;
          if (typeof child === 'number') return String(child);
          if (child?.props?.children) return extractTextContent(child);
          return '';
        })
        .filter(Boolean)
        .join(' ')
        .trim();
    }
  }
  
  return String(value);
};

/**
 * Exports data to CSV and triggers download
 * @param options - CSV export configuration
 * @throws Error if export fails
 */
export const exportToCSV = (options: CSVExportOptions): void => {
  const { filename, headers, rows, excludeColumns = [] } = options;

  if (rows.length === 0) {
    throw new Error('No data available to export');
  }

  // Filter out excluded columns
  const exportableHeaders = headers.filter(
    (h) => !excludeColumns.includes(h.key)
  );

  // Create CSV header row
  const csvHeaders = exportableHeaders.map((h) => h.header);

  // Create CSV data rows
  const csvRows = rows.map((row) =>
    exportableHeaders.map((h) => {
      const value = row[h.key];
      const textContent = extractTextContent(value);
      return escapeCSV(textContent);
    })
  );

  // Combine headers and rows
  const csv = [csvHeaders, ...csvRows].map((r) => r.join(',')).join('\n');

  // Create blob and download
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `${filename.replace(/\.[^/.]+$/, '')}.csv`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
};

/**
 * Validates filename for CSV export
 * @param filename - The filename to validate
 * @returns Error message if invalid, empty string if valid
 */
export const validateFilename = (filename: string): string => {
  const trimmed = filename.trim();
  
  if (!trimmed) {
    return 'Provide a valid file name';
  }
  
  // Check for invalid characters
  const invalidChars = /[<>:"/\\|?*]/;
  if (invalidChars.test(trimmed)) {
    return 'Filename contains invalid characters';
  }
  
  return '';
};

// Made with Bob
