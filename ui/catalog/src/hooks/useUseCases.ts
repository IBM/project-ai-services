import { useState, useEffect } from "react";
import type { UseCase } from "@/types/useCase";

interface UseUseCasesReturn {
  useCases: UseCase[];
  isLoading: boolean;
  error: string | null;
}

export const useUseCases = (): UseUseCasesReturn => {
  const [useCases, setUseCases] = useState<UseCase[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchUseCases = async () => {
      try {
        setIsLoading(true);
        const response = await fetch("/data/useCases.json");
        if (!response.ok) {
          throw new Error("Failed to fetch use cases");
        }
        const data = await response.json();
        setUseCases(data.useCases);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
        console.error("Error fetching use cases:", err);
      } finally {
        setIsLoading(false);
      }
    };

    fetchUseCases();
  }, []);

  return { useCases, isLoading, error };
};
