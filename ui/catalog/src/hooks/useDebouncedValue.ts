import { useState, useEffect } from "react";

/**
 * A custom React hook that returns a debounced version of the provided value.
 * Useful for debouncing user search inputs, auto-saves, etc.
 *
 * @param value The value to debounce.
 * @param delay The delay in milliseconds (default: 400).
 * @returns The debounced value.
 */
export function useDebouncedValue<T>(value: T, delay = 400): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value);

  useEffect(() => {
    const handler = setTimeout(() => {
      setDebouncedValue(value);
    }, delay);

    return () => {
      clearTimeout(handler);
    };
  }, [value, delay]);

  return debouncedValue;
}
