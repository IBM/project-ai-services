import { useEffect } from "react";

const SUCCESS_TOAST_DISMISS_MS = 5_000; // 5 seconds

interface UseExportToastAutoDismissOptions {
  exportToastOpen: boolean;
  exportToastKind: "success" | "error";
  /** Called after the timeout to dismiss the toast. */
  onDismiss: () => void;
  /** Override the dismiss timeout in ms. Defaults to 5000. */
  dismissAfterMs?: number;
}

export function useExportToastAutoDismiss({
  exportToastOpen,
  exportToastKind,
  onDismiss,
  dismissAfterMs = SUCCESS_TOAST_DISMISS_MS,
}: UseExportToastAutoDismissOptions): void {
  useEffect(() => {
    if (exportToastOpen && exportToastKind === "success") {
      const timer = setTimeout(onDismiss, dismissAfterMs);
      return () => clearTimeout(timer);
    }
  }, [exportToastOpen, exportToastKind, dismissAfterMs, onDismiss]);
}
