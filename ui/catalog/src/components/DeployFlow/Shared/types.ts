export interface ResourceItem {
  label: string;
  required: string;
  available: string;
  unit: string;
  type: "cpu" | "memory" | "accelerator" | "storage";
  acceleratorType?: string;
}
