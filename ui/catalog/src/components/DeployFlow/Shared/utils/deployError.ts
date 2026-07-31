// Extracts error message from deployment error responses
export function extractDeployError(error: unknown): string {
  if (error && typeof error === "object") {
    const err = error as {
      response?: {
        data?: { detail?: string; message?: string; error?: string };
      };
      message?: string;
    };
    if (typeof err.response?.data?.detail === "string")
      return err.response.data.detail;
    if (typeof err.response?.data?.message === "string")
      return err.response.data.message;
    if (typeof err.response?.data?.error === "string")
      return err.response.data.error;
    if (error instanceof Error && !err.response) return error.message;
  }
  return "Failed to deploy application";
}
