// De-duplicates in-flight requests to prevent race conditions
const inFlightRequests = new Map<string, Promise<unknown>>();

export async function dedupe<T>(
  key: string,
  fetcher: () => Promise<T>,
): Promise<T> {
  if (inFlightRequests.has(key)) {
    return inFlightRequests.get(key) as Promise<T>;
  }

  const promise = fetcher().finally(() => {
    inFlightRequests.delete(key);
  });

  inFlightRequests.set(key, promise);
  return promise;
}
