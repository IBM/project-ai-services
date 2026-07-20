import type { TableHeaders } from "@/components/table/types";

// Search filtering

export function filterRowsBySearch<T extends Record<string, unknown>>(
  rows: T[],
  search: string,
  searchFields: (keyof T)[],
): T[] {
  if (!search) return rows;
  const lower = search.toLowerCase();
  return rows.filter((row) =>
    searchFields.some((field) =>
      String(row[field] ?? "")
        .toLowerCase()
        .includes(lower),
    ),
  );
}

// Visible column header calculation

export function getVisibleHeaders(
  headers: TableHeaders,
  visibleColumns: Record<string, boolean>,
): TableHeaders {
  return headers.filter(
    (h) => h.key === "actions" || visibleColumns[h.key] === true,
  );
}

// Toggleable column list (for the column management UI)

export function getToggleableHeaders(headers: TableHeaders): TableHeaders {
  return headers.filter((h) => h.key !== "actions");
}
