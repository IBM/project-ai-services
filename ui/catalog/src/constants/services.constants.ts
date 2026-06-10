// Service ID mapping - maps form service keys to catalog IDs
export const SERVICE_ID_MAP: Record<string, string> = {
  digitizeDocuments: "digitize",
  findSimilarItems: "similarity",
  questionAndAnswer: "chat",
  summarization: "summarize",
};

// Service key type for type safety
export type ServiceKey = keyof typeof SERVICE_ID_MAP;

// Made with Bob
