import { useState, useRef, useEffect } from "react";
import { generateModelKey } from "@/api/models.api";

// Generates a model API key on demand; the key lives only in this hook's state and is cleared on close
export const useGenerateModelKey = (modelId: string) => {
  const [isOpen, setIsOpen] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  useEffect(() => {
    return () => {
      abortControllerRef.current?.abort();
    };
  }, []);

  const open = () => {
    setApiKey(null);
    setError(null);
    setIsGenerating(false);
    setIsOpen(true);
  };

  const generate = async () => {
    // Block re-entry: a second request could revoke the key from the first before it is shown
    if (isGenerating) return;
    const controller = new AbortController();
    abortControllerRef.current = controller;

    setIsOpen(true);
    setIsGenerating(true);
    setApiKey(null);
    setError(null);

    try {
      const response = await generateModelKey(modelId, {
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      setApiKey(response.key);
    } catch (err) {
      if (controller.signal.aborted) return;
      setError(
        err instanceof Error ? err.message : "Failed to generate the API key",
      );
    } finally {
      if (!controller.signal.aborted) {
        setIsGenerating(false);
      }
    }
  };

  const close = () => {
    abortControllerRef.current?.abort();
    setIsOpen(false);
  };

  return { isOpen, isGenerating, apiKey, error, open, generate, close };
};
