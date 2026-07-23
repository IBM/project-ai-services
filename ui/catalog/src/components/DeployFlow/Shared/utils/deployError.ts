export function extractDeployError(error: unknown): string {
  if (error && typeof error === "object") {
    const err = error as {
      response?: {
        data?: { detail?: string; message?: string; error?: string };
      };
      message?: string;
    };
    if (err.response?.data?.detail) return err.response.data.detail;
    if (err.response?.data?.message) return err.response.data.message;
    if (err.response?.data?.error) return err.response.data.error;
    if (err.message) return err.message;
  }
  return "Failed to deploy application";
}
