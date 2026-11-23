import { useState } from "react";
import { generateModelKey } from "@/api/models.api";

// Generates a model API key on demand; the key lives only in this hook's state and is cleared on close
export const useGenerateModelKey = (modelId: string) => {
  const [isOpen, setIsOpen] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const generate = async () => {
    setIsOpen(true);
    setIsGenerating(true);
    setApiKey(null);
    setError(null);
    try {
      const response = await generateModelKey(modelId);
      setApiKey(response.key);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to generate the API key",
      );
    } finally {
      setIsGenerating(false);
    }
  };

  const close = () => {
    setIsOpen(false);
    setApiKey(null);
    setError(null);
  };

  return { isOpen, isGenerating, apiKey, error, generate, close };
};
